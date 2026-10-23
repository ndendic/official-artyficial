// Package transcript reads an agent's replies from its own transcript
// file, as raw Markdown so patch blocks arrive intact: Claude's project
// JSONL, Codex's rollout and Pi's session file. Every other agent has the
// screen reader. (Codex and Pi render Markdown on screen and drop the
// fences, so their screen never shows a patch block.)
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Turn is one prompt and the agent's reply.
type Turn struct {
	// ID is stable for the life of the turn (Claude's promptId, or the
	// uuid of the prompt entry).
	ID     string
	Prompt string
	Parts  []Part
	// Done is true once the agent finished the turn.
	Done bool
}

// Part is a run of reply text, or a run of tool calls folded into one.
type Part struct {
	Text  string
	Tools int
}

// Text is the reply's Markdown with the tool runs left out.
func (t Turn) Text() string {
	var b []string
	for _, p := range t.Parts {
		if p.Text != "" {
			b = append(b, p.Text)
		}
	}
	return strings.Join(b, "\n\n")
}

// MaxTurns is how many turns a reader keeps.
const MaxTurns = 12

// File tails an agent's own transcript file incrementally: Claude's
// project JSONL, a Codex rollout or a Pi session. Each Turns call reads
// only what was appended, keeps a partial last line for the next call,
// and starts again when the file shrank (replaced or truncated).
type File struct {
	mu     sync.Mutex
	path   string
	offset int64
	rest   []byte
	turns  []Turn
	newF   func() folder
	f      folder
}

// folder adds complete JSONL lines to turns. It may keep state between
// calls (Codex's current turn id); a new one is made when the file starts
// again.
type folder interface {
	fold(turns []Turn, data []byte) []Turn
}

type foldFunc func([]Turn, []byte) []Turn

func (fn foldFunc) fold(t []Turn, d []byte) []Turn { return fn(t, d) }

// Claude is a Claude Code transcript.
type Claude = File

// FindClaude returns the transcript path of a Claude session id under
// projectsDir (~/.claude/projects), or "".
func FindClaude(projectsDir, sessionID string) string {
	if sessionID == "" || strings.ContainsAny(sessionID, "/\\*?[") {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(projectsDir, "*", sessionID+".jsonl"))
	if len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// NewClaude reads a Claude transcript from its start on the first Turns
// call.
func NewClaude(path string) *File {
	return newFile(path, func() folder { return foldFunc(Fold) })
}

func newFile(path string, newF func() folder) *File {
	return &File{path: path, newF: newF, f: newF()}
}

// Path is the transcript file.
func (c *File) Path() string { return c.path }

// Turns reads what was appended since the last call and returns the last
// MaxTurns turns, oldest first.
func (c *File) Turns() ([]Turn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, err := os.Open(c.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < c.offset { // truncated or replaced: start again
		c.offset, c.rest, c.turns, c.f = 0, nil, nil, c.newF()
	}
	if st.Size() > c.offset {
		if _, err := f.Seek(c.offset, io.SeekStart); err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, st.Size()-c.offset))
		if err != nil {
			return nil, err
		}
		c.offset += int64(len(data))
		data = append(c.rest, data...)
		last := bytes.LastIndexByte(data, '\n')
		if last < 0 {
			c.rest = data
			data = nil
		} else {
			c.rest = append([]byte(nil), data[last+1:]...)
			data = data[:last+1]
		}
		if len(data) > 0 {
			c.turns = keepLast(c.f.fold(c.turns, data))
		}
	}
	return append([]Turn(nil), c.turns...), nil
}

// keepLast returns the last MaxTurns turns.
func keepLast(turns []Turn) []Turn {
	if len(turns) > MaxTurns {
		return append([]Turn(nil), turns[len(turns)-MaxTurns:]...)
	}
	return turns
}

// addText adds reply text to t, joined to a text part just before it.
func addText(t *Turn, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if n := len(t.Parts); n > 0 && t.Parts[n-1].Tools == 0 {
		t.Parts[n-1].Text += "\n\n" + text
	} else {
		t.Parts = append(t.Parts, Part{Text: text})
	}
}

// addTool folds one tool call into the run of tool calls it follows.
func addTool(t *Turn) {
	if n := len(t.Parts); n > 0 && t.Parts[n-1].Tools > 0 {
		t.Parts[n-1].Tools++
	} else {
		t.Parts = append(t.Parts, Part{Tools: 1})
	}
}

// newScanner scans JSONL lines up to 32 MiB (a tool result can be large).
func newScanner(data []byte) *bufio.Scanner {
	scan := bufio.NewScanner(bytes.NewReader(data))
	scan.Buffer(make([]byte, 64<<10), 32<<20)
	return scan
}

type entry struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	UUID      string `json:"uuid"`
	PromptID  string `json:"promptId"`
	Sidechain bool   `json:"isSidechain"`
	Meta      bool   `json:"isMeta"`
	Message   *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Fold adds the JSONL lines in data to turns and returns the last
// MaxTurns. It is pure, so tests can feed it lines.
func Fold(turns []Turn, data []byte) []Turn {
	scan := newScanner(data)
	for scan.Scan() {
		var e entry
		if json.Unmarshal(scan.Bytes(), &e) != nil || e.Sidechain {
			continue
		}
		switch e.Type {
		case "user":
			if e.Meta || e.Message == nil {
				continue
			}
			prompt, ok := promptText(e.Message.Content)
			if !ok {
				continue // a tool result
			}
			id := e.PromptID
			if id == "" {
				id = e.UUID
			}
			if n := len(turns); n > 0 && turns[n-1].ID == id {
				continue
			}
			if n := len(turns); n > 0 {
				turns[n-1].Done = true
			}
			turns = append(turns, Turn{ID: id, Prompt: prompt})
		case "assistant":
			if e.Message == nil || len(turns) == 0 {
				continue
			}
			var blocks []block
			if json.Unmarshal(e.Message.Content, &blocks) != nil {
				var s string
				if json.Unmarshal(e.Message.Content, &s) == nil {
					blocks = []block{{Type: "text", Text: s}}
				}
			}
			t := &turns[len(turns)-1]
			for _, b := range blocks {
				switch b.Type {
				case "text":
					addText(t, b.Text)
				case "tool_use", "server_tool_use":
					addTool(t)
				}
			}
		case "system":
			if e.Subtype == "turn_duration" && len(turns) > 0 {
				turns[len(turns)-1].Done = true
			}
		}
	}
	return keepLast(turns)
}

// promptText returns the text of a user entry, and false when the entry is
// a tool result rather than a prompt.
func promptText(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanPrompt(s), true
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return "", false
	}
	var texts []string
	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			return "", false
		case "text":
			texts = append(texts, b.Text)
		}
	}
	if len(texts) == 0 {
		return "", false
	}
	return cleanPrompt(strings.Join(texts, "\n")), true
}

var pasted = regexp.MustCompile(`</?pasted_content[^>]*>`)

func cleanPrompt(s string) string {
	// Claude Code wraps pasted text (herdr submits a prompt as a paste).
	t := strings.TrimSpace(pasted.ReplaceAllString(s, ""))
	switch {
	case strings.HasPrefix(t, "<task-notification>"):
		return "(task notification)"
	case strings.HasPrefix(t, "<command-name>"):
		if i := strings.Index(t, "</command-name>"); i > 0 {
			return t[len("<command-name>"):i]
		}
	case strings.HasPrefix(t, "<local-command"), strings.HasPrefix(t, "<system-reminder>"):
		return "(system)"
	}
	return t
}
