package console

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-console/herdr"
)

// The recorded Codex and Pi sessions (transcript/testdata) drive the
// console end to end with a fake herdr and a fake page. Their hc* signals
// are the console's own, so the test renames them to signals a block may
// set.

func setupAgents(t *testing.T, agents ...herdr.Agent) (*Console, *herdr.Fake, *fakeCallers, Config) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := herdr.NewFake(agents...)
	fc := &fakeCallers{}
	cfg := Config{Crew: crew, ConsoleWorkspace: "Console", DataDir: t.TempDir(), ClaudeProjects: t.TempDir(),
		CodexSessions: t.TempDir(), PiSessions: t.TempDir()}
	c := New(ctx, cfg, h, nil, fc.new)
	c.Tick = 20 * time.Millisecond
	t.Cleanup(func() { c.Close(); cancel() })
	return c, h, fc, cfg
}

func recorded(t *testing.T, name string, swap ...string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.NewReplacer(swap...).Replace(string(b))
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func appendTo(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l); err != nil {
			t.Fatal(err)
		}
	}
}

// waitLong polls cond for up to 6 seconds (the follower looks for a
// missing transcript every 2 seconds).
func waitLong(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func applied(fc *fakeCallers, signals string) int {
	n := 0
	for _, p := range fc.all() {
		if p.signals == signals {
			n++
		}
	}
	return n
}

func sourceOf(c *Console, ticket string) string {
	c.mu.Lock()
	s := c.sessions[ticket]
	c.mu.Unlock()
	if s == nil {
		return ""
	}
	if f := c.follower(s); f != nil {
		return f.source()
	}
	return ""
}

func TestPiTranscript(t *testing.T) {
	c, h, fc, cfg := setupAgents(t)
	// herdr's pi integration reports the session file before pi writes it.
	path := filepath.Join(cfg.PiSessions, "--tmp-x--", "2026-10-04T22-06-05-119Z_01a1.jsonl")
	a := herdr.Agent{Pane: "w9:p3", Workspace: "w9", Name: "hctpi", Kind: "pi", Status: "idle", Cwd: "/tmp/x", SessionKind: "path", SessionID: path}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	if got := sourceOf(c, "A"); got != "screen" {
		t.Fatalf("source before the file = %q", got)
	}
	lines := recorded(t, "pi-session.jsonl", "hcPi", "piSays")
	appendTo(t, path, lines[:5]...) // header, the prompt, the tool call
	waitLong(t, "the transcript", func() bool { return sourceOf(c, "A") == "transcript" })
	appendTo(t, path, lines[5:]...)
	defer func() {
		if t.Failed() {
			t.Logf("patches:\n%s", fc.html())
		}
	}()
	waitLong(t, "both blocks", func() bool {
		return applied(fc, `{"piSays":"ok"}`) == 1 && applied(fc, `{"piSays":"two"}`) == 1
	})
	html := fc.html()
	for _, want := range []string{"The command completed successfully.", "Blue evokes calm skies", "✓ patch-signals"} {
		if !strings.Contains(html, want) {
			t.Errorf("thread misses %q", want)
		}
	}
	// Following again applies nothing twice.
	call(t, c, "A:2", Request{Action: "select", Pane: a.Pane})
	time.Sleep(150 * time.Millisecond)
	if applied(fc, `{"piSays":"ok"}`) != 1 {
		t.Fatal("re-applied")
	}
}

// A session file outside the Pi sessions folder is never read.
func TestPiSessionOutsideFolder(t *testing.T) {
	c, h, _, _ := setupAgents(t)
	outside := filepath.Join(t.TempDir(), "s.jsonl")
	appendTo(t, outside, recorded(t, "pi-session.jsonl")...)
	a := herdr.Agent{Pane: "w9:p4", Workspace: "w9", Name: "p", Kind: "pi", Status: "idle", Cwd: "/tmp", SessionKind: "path", SessionID: outside}
	h.Put(a)
	call(t, c, "A:1", Request{Action: "select", Pane: a.Pane})
	if got := sourceOf(c, "A"); got != "screen" {
		t.Fatalf("source = %q", got)
	}
}

func TestCodexTranscriptByFolder(t *testing.T) {
	c, h, fc, cfg := setupAgents(t)
	cwd := t.TempDir()
	day := filepath.Join(cfg.CodexSessions, "2026", "10", "04")
	// An earlier Codex session in the same folder, ended before the
	// console followed: its block is earlier.
	earlier := recorded(t, "codex-rollout.jsonl", "/tmp/hc-codexpi/codex", cwd, "hcCodex", "codexOld", "01a108f", "0bad08f")
	appendTo(t, filepath.Join(day, "rollout-2026-10-04T20-00-00-old.jsonl"), earlier...)
	a := herdr.Agent{Pane: "w9:p1", Workspace: "w9", Name: "hctcodex", Kind: "codex", Status: "idle", Cwd: cwd}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	if got := sourceOf(c, "A"); got != "transcript" {
		t.Fatalf("source = %q", got)
	}
	time.Sleep(100 * time.Millisecond)
	if applied(fc, `{"codexOld":"ok"}`) != 0 || !strings.Contains(fc.html(), "earlier, not applied") {
		t.Fatal("a block of a session ended before the console followed was applied, or has no chip")
	}
	// A /new: Codex starts another rollout at its first prompt, written as
	// the reply goes.
	time.Sleep(10 * time.Millisecond)
	lines := recorded(t, "codex-rollout.jsonl", "/tmp/hc-codexpi/codex", cwd, "hcCodex", "codexSays")
	path := filepath.Join(day, "rollout-2026-10-04T22-05-56-new.jsonl")
	appendTo(t, path, lines[:16]...) // up to the tool call: no answer yet
	waitLong(t, "the new rollout", func() bool {
		f := c.follower(c.sessions["A"])
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.file != nil && f.file.Path() == path
	})
	appendTo(t, path, lines[16:]...)
	waitLong(t, "both blocks", func() bool {
		return applied(fc, `{"codexSays":"ok"}`) == 1 && applied(fc, `{"codexSays":"two"}`) == 1
	})
	if !strings.Contains(fc.html(), "Blue is the colour of clear skies.") {
		t.Error("thread misses the second answer")
	}
}

// Two Codex sessions in one folder: the console reads a rollout only once
// it holds a prompt the console sent to this pane.
func TestCodexSharedFolderWaitsForAPrompt(t *testing.T) {
	cwd := t.TempDir()
	a := herdr.Agent{Pane: "w9:p1", Workspace: "w9", Name: "one", Kind: "codex", Status: "idle", Cwd: cwd}
	b := herdr.Agent{Pane: "w9:p2", Workspace: "w9", Name: "two", Kind: "codex", Status: "idle", Cwd: cwd}
	c, h, _, cfg := setupAgents(t, a, b)
	day := filepath.Join(cfg.CodexSessions, "2026", "10", "04")
	other := recorded(t, "codex-rollout.jsonl", "/tmp/hc-codexpi/codex", cwd)
	appendTo(t, filepath.Join(day, "rollout-2026-10-04T20-00-00-other.jsonl"), other...)
	call(t, c, "A:1", Request{Action: "select", Pane: a.Pane})
	if got := sourceOf(c, "A"); got != "screen" {
		t.Fatalf("source = %q (took another session's rollout)", got)
	}
	_ = h
	// Not a session the console opened: the send takes the confirm step.
	call(t, c, "A:2", Request{Action: "send", Pane: a.Pane, Text: "hello from the console"})
	res, f := call(t, c, "A:3", Request{Action: "send", Pane: a.Pane, Text: "hello from the console", Confirm: true})
	if f != nil || res["sent"] != true {
		t.Fatalf("send = %v %v", res, f)
	}
	mine := filepath.Join(day, "rollout-2026-10-04T21-00-00-mine.jsonl")
	appendTo(t, mine, other[0], `{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hello from the console"}]}}`+"\n",
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi back"}]}}`+"\n")
	waitLong(t, "this pane's rollout", func() bool { return sourceOf(c, "A") == "transcript" })
	f2 := c.follower(c.sessions["A"])
	f2.mu.Lock()
	got := f2.file.Path()
	f2.mu.Unlock()
	if got != mine {
		t.Fatalf("read %s", got)
	}
}

// The picker and the head mark Codex and Pi sessions with their own icon,
// from the kind herdr reports.
func TestPickerKindsCodexPi(t *testing.T) {
	codex := herdr.Agent{Pane: "wH:p1", Workspace: "wH", Name: "hctcodex", Kind: "codex", Status: "done", Cwd: "/tmp/a"}
	pi := herdr.Agent{Pane: "wH:p3", Workspace: "wH", Name: "hctpi", Kind: "pi", Status: "done", Cwd: "/tmp/b"}
	c, _, fc, _ := setupAgents(t, codex, pi)
	if _, f := call(t, c, "A:1", Request{Action: "open"}); f != nil {
		t.Fatal(f)
	}
	html := fc.html()
	for _, want := range []string{`data-kind="codex"`, `data-kind="pi"`} {
		if !strings.Contains(html, want) {
			t.Errorf("picker misses %s", want)
		}
	}
	call(t, c, "A:2", Request{Action: "select", Pane: codex.Pane})
	waitLong(t, "the pill agent", func() bool { return strings.Contains(fc.html(), `"kind":"codex"`) })
}
