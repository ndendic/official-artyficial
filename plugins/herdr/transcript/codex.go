package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Codex's rollout file (~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<id>.jsonl),
// as Codex 0.160 writes it (checked): one JSON object a line,
// {"timestamp", "type", "payload"}. The reader uses the parts that older
// versions write too:
//
//   - session_meta: the first line; payload.id and payload.cwd.
//   - event_msg task_started / task_complete / turn_aborted: a turn's
//     start and end (payload.turn_id).
//   - response_item message: role "user" (a prompt, or context Codex
//     injects: AGENTS.md, the environment), role "assistant" (output_text,
//     phase commentary or final_answer), role "developer" (instructions).
//   - response_item *_call: a tool call (function_call, custom_tool_call,
//     local_shell_call, web_search_call, …); *_call_output is its result.
//
// Codex writes the file at the first prompt, and each message whole when
// it is complete, so a reply grows message by message.

// NewCodex reads a Codex rollout from its start on the first Turns call.
func NewCodex(path string) *File {
	return newFile(path, func() folder { return &codexFold{} })
}

type codexFold struct {
	turnID string // the turn task_started named last
	n      int    // prompts seen, for ids when Codex names none
}

type codexLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		TurnID  string `json:"turn_id"`
		ID      string `json:"id"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Meta struct {
			TurnID string   `json:"turn_id"`
			Kinds  []string `json:"content_item_kinds"`
		} `json:"internal_chat_message_metadata_passthrough"`
	} `json:"payload"`
}

// FoldCodex folds Codex rollout lines into turns, from the file's start.
func FoldCodex(turns []Turn, data []byte) []Turn {
	return keepLast((&codexFold{}).fold(turns, data))
}

func (c *codexFold) fold(turns []Turn, data []byte) []Turn {
	scan := newScanner(data)
	for scan.Scan() {
		var e codexLine
		if json.Unmarshal(scan.Bytes(), &e) != nil {
			continue
		}
		p := &e.Payload
		switch e.Type {
		case "event_msg":
			switch p.Type {
			case "task_started":
				c.turnID = p.TurnID
			case "task_complete", "turn_aborted":
				if n := len(turns); n > 0 {
					turns[n-1].Done = true
				}
			}
		case "response_item":
			switch {
			case p.Type == "message" && p.Role == "user":
				prompt := codexPrompt(e)
				if prompt == "" {
					continue // context Codex injected, not a prompt
				}
				c.n++
				id := p.Meta.TurnID
				if id == "" {
					id = c.turnID
				}
				if id == "" {
					id = p.ID
				}
				if id == "" {
					id = fmt.Sprintf("codex-%d", c.n)
				}
				// A second prompt in one turn (steering) is a turn of its
				// own here, with an id of its own.
				if k := lastSuffix(turns, id); k > 0 {
					id = fmt.Sprintf("%s#%d", id, k+1)
				}
				if n := len(turns); n > 0 {
					turns[n-1].Done = true
				}
				turns = append(turns, Turn{ID: id, Prompt: prompt})
			case p.Type == "message" && p.Role == "assistant":
				if len(turns) == 0 {
					continue
				}
				t := &turns[len(turns)-1]
				for _, b := range p.Content {
					if b.Type == "output_text" || b.Type == "text" {
						addText(t, b.Text)
					}
				}
			case strings.HasSuffix(p.Type, "_call"):
				if len(turns) > 0 {
					addTool(&turns[len(turns)-1])
				}
			}
		}
	}
	return turns
}

// lastSuffix is the highest k of the turns with id base ("base" is 1,
// "base#k" is k), or 0 when none has it.
func lastSuffix(turns []Turn, base string) int {
	k := 0
	for _, t := range turns {
		switch {
		case t.ID == base:
			k = max(k, 1)
		case strings.HasPrefix(t.ID, base+"#"):
			var n int
			if _, err := fmt.Sscanf(t.ID[len(base)+1:], "%d", &n); err == nil {
				k = max(k, n)
			}
		}
	}
	return k
}

// codexPrompt is the text a person typed in a user message, or "" when
// the message is context Codex adds (AGENTS.md, the environment, a shell
// command's output).
func codexPrompt(e codexLine) string {
	kinds := e.Payload.Meta.Kinds
	var texts []string
	for i, b := range e.Payload.Content {
		if b.Type != "input_text" && b.Type != "text" {
			continue
		}
		if len(kinds) == len(e.Payload.Content) {
			// Codex 0.160 names each item's kind; a prompt is user.text.
			if kinds[i] != "user.text" {
				continue
			}
		} else if codexContext(b.Text) {
			continue
		}
		if t := strings.TrimSpace(b.Text); t != "" {
			texts = append(texts, t)
		}
	}
	return strings.Join(texts, "\n")
}

func codexContext(text string) bool {
	t := strings.TrimSpace(text)
	for _, p := range []string{"# AGENTS.md instructions", "<environment_context>", "<user_instructions>", "<INSTRUCTIONS>", "<user_shell_command>", "<turn_aborted>", "<subagent_notification>"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// CodexMeta is what a rollout's first line says about its session.
type CodexMeta struct {
	ID  string
	Cwd string
	// Subagent is true for a session another Codex session spawned.
	Subagent bool
}

// ReadCodexMeta reads the session_meta line at the start of a rollout.
func ReadCodexMeta(path string) (CodexMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return CodexMeta{}, false
	}
	defer f.Close()
	// The first line carries Codex's base instructions: tens of KiB.
	line, err := bufio.NewReaderSize(io.LimitReader(f, 8<<20), 64<<10).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return CodexMeta{}, false
	}
	var e struct {
		Type    string `json:"type"`
		Payload struct {
			ID     string          `json:"id"`
			Cwd    string          `json:"cwd"`
			Source json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "session_meta" {
		return CodexMeta{}, false
	}
	m := CodexMeta{ID: e.Payload.ID, Cwd: e.Payload.Cwd}
	// source is a string ("cli", "vscode", "exec") for a session a person
	// runs, an object ({"subagent": …}) for a spawned one.
	src := strings.TrimSpace(string(e.Payload.Source))
	m.Subagent = strings.HasPrefix(src, "{")
	return m, true
}

// CodexLook says which rollout FindCodex looks for.
type CodexLook struct {
	// SessionID, when herdr names the session (herdr's Codex integration
	// reports it from Codex's SessionStart hook), finds the file by name.
	SessionID string
	// Cwd is the agent's folder: without a session id, the newest rollout
	// of a session started in Cwd.
	Cwd string
	// Prompt, when set, keeps only a rollout that holds this prompt (for
	// two Codex sessions in one folder: the one the console wrote to).
	Prompt string
	// Since, when set, skips rollouts not written since.
	Since time.Time
}

// codexScan bounds how many of the newest rollouts FindCodex opens.
const codexScan = 64

// FindCodex returns the rollout of a Codex session under sessionsDir
// (~/.codex/sessions), or "".
func FindCodex(sessionsDir string, look CodexLook) string {
	if sessionsDir == "" {
		return ""
	}
	if id := look.SessionID; id != "" {
		if strings.ContainsAny(id, "/\\*?[") {
			return ""
		}
		matches, _ := filepath.Glob(filepath.Join(sessionsDir, "*", "*", "*", "rollout-*-"+id+".jsonl"))
		if len(matches) > 0 {
			return newest(matches)[0].path
		}
		return ""
	}
	if look.Cwd == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(sessionsDir, "*", "*", "*", "rollout-*.jsonl"))
	files := newest(matches)
	for i, fi := range files {
		if i >= codexScan || !look.Since.IsZero() && fi.mod.Before(look.Since) {
			break
		}
		m, ok := ReadCodexMeta(fi.path)
		if !ok || m.Subagent || filepath.Clean(m.Cwd) != filepath.Clean(look.Cwd) {
			continue
		}
		if look.Prompt != "" && !codexHolds(fi.path, look.Prompt) {
			continue
		}
		return fi.path
	}
	return ""
}

type fileMod struct {
	path string
	mod  time.Time
}

// newest stats paths and sorts them newest first.
func newest(paths []string) []fileMod {
	out := make([]fileMod, 0, len(paths))
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		out = append(out, fileMod{p, st.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

// codexHolds reports whether the rollout at path has prompt as one of its
// last turns' prompts.
func codexHolds(path, prompt string) bool {
	turns, err := NewCodex(path).Turns()
	if err != nil {
		return false
	}
	want := strings.TrimSpace(prompt)
	for _, t := range turns {
		if strings.TrimSpace(t.Prompt) == want {
			return true
		}
	}
	return false
}
