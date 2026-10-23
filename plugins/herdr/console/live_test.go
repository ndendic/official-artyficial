//go:build live

// The live test drives a real herdr server through the console, with a fake
// Caller in place of the page. It touches only what it creates: a herdr
// workspace labelled Console-test (no focus), one Claude session in a
// temporary folder, and it closes both at the end.
//
//	HERDR_LIVE=1 go test -tags live -run TestLive -v ./console
package console

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-console/herdr"
	"herdr-console/transcript"
)

func TestLive(t *testing.T) {
	if os.Getenv("HERDR_LIVE") != "1" {
		t.Skip("set HERDR_LIVE=1")
	}
	home, _ := os.UserHomeDir()
	socket := filepath.Join(home, ".config", "herdr", "herdr.sock")
	cli := &herdr.CLI{Bin: herdr.FindBin("herdr"), Socket: socket}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := herdr.NewWatcher(cli, socket)
	go watch.Run(ctx)

	fc := &fakeCallers{}
	c := New(ctx, Config{Crew: crew, ConsoleWorkspace: "Console-test", ClaudeProjects: filepath.Join(home, ".claude", "projects"), DataDir: t.TempDir()}, cli, watch, fc.new)
	defer c.Close()

	// Only ever clean up what this test opened.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		for _, o := range c.Opened().All() {
			_ = cli.ClosePane(cctx, o.Pane)
		}
		spaces, _ := cli.Workspaces(cctx)
		for _, w := range spaces {
			if w.Label == "Console-test" {
				// The workspace is ours only if it holds no tab now.
				out, err := exec(cctx, cli, "tab", "list", "--workspace", w.ID)
				if err == nil && !strings.Contains(out, `"tab_id"`) {
					_, _ = exec(cctx, cli, "workspace", "close", w.ID)
				}
			}
		}
	})

	dir := t.TempDir()
	res, f := c.Handle(ctx, "LIVE:1", Request{Action: "start", Kind: "claude", Cwd: dir, Name: "hc-live-test"})
	if f != nil {
		t.Fatalf("start: %v", f)
	}
	t.Logf("start: %v", res)

	var pane string
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && pane == "" {
		for _, o := range c.Opened().All() {
			pane = o.Pane
		}
		time.Sleep(200 * time.Millisecond)
	}
	if pane == "" {
		t.Fatal("no pane opened")
	}
	t.Logf("watcher on socket events: %v", watch.Live())
	t.Logf("opened %s", pane)

	// Wait until the agent is idle, answering a startup dialog of OUR
	// session through the keys action, as a person would from the card.
	answered := 0
	for time.Now().Before(deadline) {
		a, err := cli.Get(ctx, pane)
		if err == nil && (a.Status == "idle" || a.Status == "done") {
			break
		}
		if err == nil && a.Status == "blocked" && answered < 3 {
			screen, _ := cli.Read(ctx, pane, 40)
			text, keys := DialogText(screen)
			t.Logf("dialog (%v):\n%s", keys, text)
			// Claude's folder-trust dialog lists "No, exit" first and
			// selected: move down to "Yes, I trust this folder" first.
			choice := []string{"enter"}
			if strings.Contains(text, "trust this folder") && !strings.Contains(lineWith(text, "Yes, I trust"), "❯") {
				choice = []string{"down", "enter"}
			}
			if _, f := c.Handle(ctx, "LIVE:2", Request{Action: "keys", Pane: pane, Keys: choice}); f != nil {
				t.Fatalf("keys: %v", f)
			}
			answered++
			time.Sleep(2 * time.Second)
			continue
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !strings.Contains(fc.html(), `class="hc-dialog"`) && answered > 0 {
		t.Log("note: the dialog card was not rendered while blocked")
	}

	if _, f := c.Handle(ctx, "LIVE:3", Request{Action: "select", Pane: pane}); f != nil {
		t.Fatalf("select: %v", f)
	}
	res, f = c.Handle(ctx, "LIVE:4", Request{Action: "send", Pane: pane, Text: "reply with the word ready"})
	if f != nil || res["sent"] != true {
		t.Fatalf("send to the opened session = %v %v", res, f)
	}
	// The reply arrives through the transcript and streams into the thread.
	got := false
	for end := time.Now().Add(90 * time.Second); time.Now().Before(end) && !got; time.Sleep(500 * time.Millisecond) {
		for _, p := range fc.all() {
			if strings.Contains(p.html, `class="hc-a"`) && strings.Contains(strings.ToLower(p.html), "ready</p>") {
				got = true
			}
		}
	}
	if !got {
		t.Fatalf("no streamed reply 'ready' in the patches; last: %s", tail(fc.html(), 2000))
	}
	a, _ := cli.Get(ctx, pane)
	path := transcript.FindClaude(filepath.Join(home, ".claude", "projects"), a.SessionID)
	t.Logf("transcript: %s (source %s)", path, c.follower(c.sessions["LIVE"]).source())
	// The screen fallback reads the same reply.
	screen, err := cli.Read(ctx, pane, 200)
	if err != nil {
		t.Fatal(err)
	}
	reply := transcript.AfterPrompt(screen, "reply with the word ready")
	t.Logf("screen reply: %q", reply)
	if !strings.Contains(strings.ToLower(reply), "ready") {
		t.Errorf("the screen reader did not find the reply")
	}

	if _, f := c.Handle(ctx, "LIVE:5", Request{Action: "close", Pane: pane}); f != nil {
		t.Fatalf("close: %v", f)
	}
	if _, err := cli.Get(ctx, pane); err == nil {
		t.Fatal("the session is still live after close")
	}
}

func exec(ctx context.Context, cli *herdr.CLI, args ...string) (string, error) {
	out, err := cli.Raw(ctx, args...)
	return string(out), err
}

func lineWith(text, sub string) string {
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
