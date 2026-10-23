package transcript

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// Pi's session file (~/.pi/agent/sessions/--<cwd>--/<time>_<id>.jsonl),
// session format version 3 (checked with pi and herdr's pi
// integration, which reports the file's path as the agent session): one
// JSON object a line with type, id and parentId. The reader uses the
// "message" entries:
//
//   - role "user": a prompt (content a string, or text and image blocks);
//   - role "assistant": text, thinking and toolCall blocks, and stopReason
//     ("toolUse" while the agent goes on; "stop", "length", "error" or
//     "aborted" when the turn ended);
//   - role "toolResult", "bashExecution" and the rest: not reply text.
//
// Pi writes each message whole when it is complete. A session is a tree
// (/tree can branch it); the reader follows the file's order, which is the
// branch being worked on unless a person moved back in the tree.

// NewPi reads a Pi session file from its start on the first Turns call.
func NewPi(path string) *File {
	return newFile(path, func() folder { return foldFunc(FoldPi) })
}

type piLine struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Message *struct {
		Role       string          `json:"role"`
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stopReason"`
	} `json:"message"`
}

type piBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// FoldPi folds Pi session lines into turns. It is pure.
func FoldPi(turns []Turn, data []byte) []Turn {
	scan := newScanner(data)
	for scan.Scan() {
		var e piLine
		if json.Unmarshal(scan.Bytes(), &e) != nil || e.Type != "message" || e.Message == nil {
			continue
		}
		m := e.Message
		var blocks []piBlock
		if json.Unmarshal(m.Content, &blocks) != nil {
			var s string
			if json.Unmarshal(m.Content, &s) != nil {
				continue
			}
			blocks = []piBlock{{Type: "text", Text: s}}
		}
		switch m.Role {
		case "user":
			var texts []string
			for _, b := range blocks {
				if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
					texts = append(texts, strings.TrimSpace(b.Text))
				}
			}
			if len(texts) == 0 {
				continue
			}
			id := e.ID
			if n := len(turns); n > 0 {
				if turns[n-1].ID == id {
					continue
				}
				turns[n-1].Done = true
			}
			turns = append(turns, Turn{ID: id, Prompt: strings.Join(texts, "\n")})
		case "assistant":
			if len(turns) == 0 {
				continue
			}
			t := &turns[len(turns)-1]
			for _, b := range blocks {
				switch b.Type {
				case "text":
					addText(t, b.Text)
				case "toolCall":
					addTool(t)
				}
			}
			switch m.StopReason {
			case "stop", "length", "error", "aborted":
				t.Done = true
			}
		}
	}
	return keepLast(turns)
}

// PiSession returns path when it is a Pi session file under sessionsDir
// (~/.pi/agent/sessions), as herdr names it; else "". The console reads
// only files there, whatever a session report says.
func PiSession(sessionsDir, path string) string {
	if sessionsDir == "" || path == "" || !filepath.IsAbs(path) || filepath.Ext(path) != ".jsonl" {
		return ""
	}
	rel, err := filepath.Rel(filepath.Clean(sessionsDir), filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	return filepath.Clean(path)
}
