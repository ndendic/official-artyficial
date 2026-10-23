package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-console/blocks"
)

// The fixtures are recorded from real sessions (Codex
// 0.160.0 and pi, started by herdr in a scratch folder), with instructions,
// account ids and tool output stripped:
//
//	codex-rollout.jsonl  two turns: one ran `ls -a` (a commentary message, a
//	                     tool call, the final answer), one answered directly
//	pi-session.jsonl     the same two prompts to pi
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// checkPatchTurn checks a turn answers with text and one complete
// patch-signals block whose body is body.
func checkPatchTurn(t *testing.T, turn Turn, wantText, body string) {
	t.Helper()
	if !turn.Done {
		t.Errorf("turn %s not done", turn.ID)
	}
	text := turn.Text()
	if !strings.Contains(text, wantText) {
		t.Errorf("text %q lacks %q", text, wantText)
	}
	var got []*blocks.Block
	for _, seg := range blocks.Parse(text, 0) {
		if seg.Block != nil {
			got = append(got, seg.Block)
		}
	}
	if len(got) != 1 {
		t.Fatalf("blocks = %d in %q", len(got), text)
	}
	b := got[0]
	// The block arrives whole. (Its hc* signal is the console's own, so the
	// safety rules refuse to apply it: b.Err says so. That is the console's
	// rule, not the reader's; a live test uses another name.)
	if b.Kind != blocks.Signals || !b.Closed || strings.TrimSpace(b.Body) != body || !strings.Contains(b.Raw, body) {
		t.Fatalf("block = %+v", b)
	}
}

func TestFoldCodex(t *testing.T) {
	turns := FoldCodex(nil, fixture(t, "codex-rollout.jsonl"))
	if len(turns) != 2 {
		t.Fatalf("turns = %d: %+v", len(turns), turns)
	}
	first := turns[0]
	if first.ID != "01a108f4-2907-7ea0-a3c2-6a05d6c3c30d" || !strings.HasPrefix(first.Prompt, "Run the shell command `ls -a` once.") {
		t.Fatalf("first = %+v", first)
	}
	if strings.Contains(first.Prompt, "AGENTS.md") || strings.Contains(first.Prompt, "environment_context") {
		t.Fatal("injected context became the prompt")
	}
	// commentary, the tool call folded, the final answer
	if len(first.Parts) != 3 || strings.TrimSpace(first.Parts[0].Text) != "I’ll run `ls -a` once." || first.Parts[1].Tools != 1 || first.Parts[2].Text == "" {
		t.Fatalf("parts = %+v", first.Parts)
	}
	checkPatchTurn(t, first, "Ran `ls -a` once.", `{"hcCodex":"ok"}`)
	checkPatchTurn(t, turns[1], "Blue is the colour of clear skies.", `{"hcCodex":"two"}`)
	if turns[1].ID == first.ID {
		t.Fatal("two turns share an id")
	}
}

func TestFoldPi(t *testing.T) {
	turns := FoldPi(nil, fixture(t, "pi-session.jsonl"))
	if len(turns) != 2 {
		t.Fatalf("turns = %d: %+v", len(turns), turns)
	}
	first := turns[0]
	if first.ID != "094e6d08" || !strings.HasPrefix(first.Prompt, "Run the shell command `ls -a` once.") {
		t.Fatalf("first = %+v", first)
	}
	if len(first.Parts) != 2 || first.Parts[0].Tools != 1 {
		t.Fatalf("parts = %+v", first.Parts)
	}
	checkPatchTurn(t, first, "The command completed successfully.", `{"hcPi":"ok"}`)
	checkPatchTurn(t, turns[1], "Blue evokes calm skies", `{"hcPi":"two"}`)
}

// Feeding the file a line at a time, and a line in two halves, gives the
// same turns as reading it whole, and a turn is not done before its end.
func tailLineByLine(t *testing.T, name string, open func(string) *File, want []Turn) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := open(path)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	sawUndone := false
	var turns []Turn
	for _, line := range strings.SplitAfter(string(fixture(t, name)), "\n") {
		half := len(line) / 2
		for _, piece := range []string{line[:half], line[half:]} {
			if _, err := f.WriteString(piece); err != nil {
				t.Fatal(err)
			}
			if turns, err = r.Turns(); err != nil {
				t.Fatal(err)
			}
			if n := len(turns); n > 0 && !turns[n-1].Done {
				sawUndone = true
			}
		}
	}
	if !sawUndone {
		t.Error("never saw a turn in progress")
	}
	if len(turns) != len(want) {
		t.Fatalf("turns = %d, want %d", len(turns), len(want))
	}
	for i := range want {
		if turns[i].ID != want[i].ID || turns[i].Text() != want[i].Text() || turns[i].Done != want[i].Done || turns[i].Prompt != want[i].Prompt {
			t.Fatalf("turn %d = %+v\nwant %+v", i, turns[i], want[i])
		}
	}
	// A second read without new lines changes nothing (no duplicates).
	again, _ := r.Turns()
	if len(again) != len(want) || again[len(again)-1].Text() != want[len(want)-1].Text() {
		t.Fatalf("re-read = %+v", again)
	}
	// The file replaced by a shorter one: the reader starts again.
	if err := os.WriteFile(path, []byte(strings.SplitAfter(string(fixture(t, name)), "\n")[0]), 0o644); err != nil {
		t.Fatal(err)
	}
	if turns, _ = r.Turns(); len(turns) != 0 {
		t.Fatalf("after replace = %+v", turns)
	}
}

func TestCodexTailsIncrementally(t *testing.T) {
	tailLineByLine(t, "codex-rollout.jsonl", NewCodex, FoldCodex(nil, fixture(t, "codex-rollout.jsonl")))
}

func TestPiTailsIncrementally(t *testing.T) {
	tailLineByLine(t, "pi-session.jsonl", NewPi, FoldPi(nil, fixture(t, "pi-session.jsonl")))
}

func TestCodexPromptWithoutKinds(t *testing.T) {
	// Older Codex: no content_item_kinds; the context is known by its tags.
	data := `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n<cwd>/x</cwd>\n</environment_context>"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<user_instructions>be brief</user_instructions>"}]}}
{"type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}}
{"type":"response_item","payload":{"type":"reasoning","summary":[]}}
{"type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{}"}}
{"type":"response_item","payload":{"type":"function_call_output","output":"ok"}}
{"type":"response_item","payload":{"type":"local_shell_call"}}
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"and again"}]}}
{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"t1"}}
`
	turns := FoldCodex(nil, []byte(data))
	if len(turns) != 2 || turns[0].ID != "t1" || turns[0].Prompt != "hello" || turns[1].ID != "t1#2" || turns[1].Prompt != "and again" {
		t.Fatalf("turns = %+v", turns)
	}
	if len(turns[0].Parts) != 2 || turns[0].Parts[0].Tools != 2 || turns[0].Parts[1].Text != "hi" || !turns[0].Done || !turns[1].Done {
		t.Fatalf("parts = %+v", turns)
	}
}

func writeRollout(t *testing.T, dir, day, name, meta string, mod time.Time, body string) string {
	t.Helper()
	d := filepath.Join(dir, day)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(d, name)
	if err := os.WriteFile(p, []byte(meta+"\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFindCodex(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	meta := func(id, cwd, source string) string {
		return `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `","source":` + source + `,"base_instructions":{"text":"` + strings.Repeat("x", 100<<10) + `"}}}`
	}
	prompt := func(text string) string {
		return `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + text + `"}]}}` + "\n"
	}
	old := writeRollout(t, dir, "2026/10/03", "rollout-2026-10-03T10-00-00-aaa.jsonl", meta("aaa", "/w/a", `"cli"`), now.Add(-time.Hour), prompt("first"))
	sub := writeRollout(t, dir, "2026/10/04", "rollout-2026-10-04T10-00-00-sss.jsonl", meta("sss", "/w/a", `{"subagent":{}}`), now, "")
	other := writeRollout(t, dir, "2026/10/04", "rollout-2026-10-04T09-00-00-bbb.jsonl", meta("bbb", "/w/b", `"cli"`), now.Add(-time.Minute), "")
	recent := writeRollout(t, dir, "2026/10/04", "rollout-2026-10-04T09-30-00-ccc.jsonl", meta("ccc", "/w/a/", `"vscode"`), now.Add(-30*time.Second), prompt("second"))
	_ = sub
	cases := []struct {
		name string
		look CodexLook
		want string
	}{
		{"newest in the folder, not a subagent", CodexLook{Cwd: "/w/a"}, recent},
		{"by id", CodexLook{SessionID: "aaa"}, old},
		{"by id, unknown", CodexLook{SessionID: "zzz"}, ""},
		{"by id, a path", CodexLook{SessionID: "../aaa"}, ""},
		{"the one holding the prompt", CodexLook{Cwd: "/w/a", Prompt: "first"}, old},
		{"another folder", CodexLook{Cwd: "/w/b"}, other},
		{"since", CodexLook{Cwd: "/w/a", Since: now.Add(-10 * time.Minute), Prompt: "first"}, ""},
		{"no folder", CodexLook{}, ""},
	}
	for _, c := range cases {
		if got := FindCodex(dir, c.look); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if m, ok := ReadCodexMeta(recent); !ok || m.ID != "ccc" || m.Subagent {
		t.Fatalf("meta = %+v %v", m, ok)
	}
}

func TestPiSession(t *testing.T) {
	dir := "/home/u/.pi/agent/sessions"
	ok := dir + "/--tmp-x--/2026-10-04T22-06-05-119Z_01a1.jsonl"
	if PiSession(dir, ok) != ok {
		t.Fatal("refused a session file")
	}
	for _, bad := range []string{"", "relative.jsonl", "/etc/passwd", dir + "/../secrets.jsonl", dir + "/--x--/a.txt", dir} {
		if PiSession(dir, bad) != "" {
			t.Errorf("accepted %q", bad)
		}
	}
	if PiSession("", ok) != "" {
		t.Fatal("accepted a file with no sessions folder")
	}
}

func TestPiAssistantTurnStates(t *testing.T) {
	data := `{"type":"session","version":3,"id":"s"}
{"type":"message","id":"u1","message":{"role":"user","content":"plain string prompt"}}
{"type":"message","id":"a1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"text","text":"working"},{"type":"toolCall","name":"read"},{"type":"toolCall","name":"bash"}],"stopReason":"toolUse"}}
{"type":"message","id":"r1","message":{"role":"toolResult","content":[{"type":"text","text":"tool output, not reply"}]}}
`
	turns := FoldPi(nil, []byte(data))
	if len(turns) != 1 || turns[0].Prompt != "plain string prompt" || turns[0].Done || turns[0].Text() != "working" || turns[0].Parts[1].Tools != 2 {
		t.Fatalf("turns = %+v", turns)
	}
	turns = FoldPi(turns, []byte(`{"type":"message","id":"a2","message":{"role":"assistant","content":[],"stopReason":"aborted"}}`+"\n"))
	if !turns[0].Done {
		t.Fatal("an aborted turn is not done")
	}
}
