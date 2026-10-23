package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{"type":"user","uuid":"u1","promptId":"p1","message":{"role":"user","content":"\n\n<pasted_content id=\"bcfa\">\nreply with the word ready\n</pasted_content id=\"bcfa\">\n"}}
{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"}]}}
{"type":"assistant","uuid":"a2","message":{"role":"assistant","content":[{"type":"text","text":"Let me check."}]}}
{"type":"assistant","uuid":"a3","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}
{"type":"user","uuid":"u2","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","uuid":"a4","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{}}]}}
{"type":"user","uuid":"u3","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","uuid":"a5","message":{"role":"assistant","content":[{"type":"text","text":"ready"}]}}
{"type":"assistant","uuid":"a6","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent noise"}]}}
{"type":"system","subtype":"turn_duration"}
{"type":"user","uuid":"u4","promptId":"p2","message":{"role":"user","content":"<task-notification>\n<x/>"}}
{"type":"assistant","uuid":"a7","message":{"role":"assistant","content":[{"type":"text","text":"noted"}]}}
`

func TestFold(t *testing.T) {
	turns := Fold(nil, []byte(sample))
	if len(turns) != 2 {
		t.Fatalf("turns = %d: %+v", len(turns), turns)
	}
	first := turns[0]
	if first.ID != "p1" || first.Prompt != "reply with the word ready" || !first.Done {
		t.Fatalf("first = %+v", first)
	}
	if len(first.Parts) != 3 || first.Parts[0].Text != "Let me check." || first.Parts[1].Tools != 2 || first.Parts[2].Text != "ready" {
		t.Fatalf("parts = %+v", first.Parts)
	}
	if strings.Contains(first.Text(), "subagent") {
		t.Fatal("a sidechain entry reached the reply")
	}
	if turns[1].Prompt != "(task notification)" || turns[1].Done {
		t.Fatalf("second = %+v", turns[1])
	}
}

func TestClaudeTailsIncrementally(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "projects", "-tmp-x")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, "abc.jsonl")
	lines := strings.SplitAfter(sample, "\n")
	// Write the first lines and half of the next one.
	half := lines[2][:10]
	if err := os.WriteFile(path, []byte(lines[0]+lines[1]+half), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindClaude(filepath.Join(dir, "projects"), "abc"); got != path {
		t.Fatalf("FindClaude = %q", got)
	}
	if FindClaude(filepath.Join(dir, "projects"), "../x") != "" {
		t.Fatal("a path in the session id was accepted")
	}
	c := NewClaude(path)
	turns, err := c.Turns()
	if err != nil || len(turns) != 1 || len(turns[0].Parts) != 0 {
		t.Fatalf("turns = %+v, %v", turns, err)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(lines[2][10:] + strings.Join(lines[3:], ""))
	_ = f.Close()
	turns, _ = c.Turns()
	if len(turns) != 2 || turns[0].Text() != "Let me check.\n\nready" {
		t.Fatalf("turns = %+v", turns)
	}
}

func TestScreen(t *testing.T) {
	var s Screen
	s.Sent("reply with the word ready")
	screen := strings.Join([]string{
		"old stuff",
		"❯ reply with the word ready",
		"",
		"● ready",
		"",
		"✻ Cooked for 3s · done 9:04 PM",
		"                         ◐ medium · /effort",
		"────────────────────────",
		"❯ ",
		"  ? for shortcuts",
	}, "\n")
	turns := s.Update(screen, true)
	if len(turns) != 1 || turns[0].Text() != "ready" || !turns[0].Done {
		t.Fatalf("turns = %+v", turns)
	}
	if AfterPrompt("nothing here", "hello") != "" {
		t.Fatal("found a prompt that is not on the screen")
	}
}

// A first prompt line over 60 bytes with a multibyte rune across byte 60
// still finds its reply.
func TestAfterPromptCutsByRunes(t *testing.T) {
	prompt := strings.Repeat("a", 59) + "čćšđž and then some more words"
	screen := "❯ " + prompt + "\n● the reply\n❯ "
	if got := AfterPrompt(screen, prompt); got != "the reply" {
		t.Fatalf("got %q", got)
	}
}

// A reply that pushes the prompt off the lines read goes on from the tail
// of what was read before, and its last patch block is kept.
func TestScreenFollowsAReplyPastThePrompt(t *testing.T) {
	var s Screen
	s.Sent("write a long answer")
	first := "❯ write a long answer\n● line one of the reply\nline two of the reply\n"
	s.Update(first, false)
	// The prompt has scrolled off; the screen now starts mid-reply.
	later := "line two of the reply\nline three of the reply\n```patch-signals\n{\"done\":true}\n```\n❯ "
	turns := s.Update(later, true)
	got := turns[len(turns)-1].Parts[0].Text
	want := "line one of the reply\nline two of the reply\nline three of the reply\n```patch-signals\n{\"done\":true}\n```"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if !turns[len(turns)-1].Done {
		t.Fatal("not done")
	}
}
