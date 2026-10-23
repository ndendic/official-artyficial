//go:build live

// The Codex and Pi live test drives a real herdr through the console with a
// fake page: for each agent it opens a session in the herdr workspace
// Console-test (no focus) in a temporary folder, sends one prompt that asks
// for a sentence and a patch-signals block, and checks the console reads
// the reply from the agent's own transcript and applies the block once. It
// closes only the panes it opened.
//
//	HERDR_LIVE=1 go test -tags live -run TestLiveCodexPi -v ./console
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

func TestLiveCodexPi(t *testing.T) {
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
	c := New(ctx, Config{Crew: crew, ConsoleWorkspace: "Console-test", DataDir: t.TempDir(),
		ClaudeProjects: filepath.Join(home, ".claude", "projects"),
		CodexSessions:  filepath.Join(home, ".codex", "sessions"),
		PiSessions:     filepath.Join(home, ".pi", "agent", "sessions")}, cli, watch, fc.new)
	defer c.Close()
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		for _, o := range c.Opened().All() {
			_ = cli.ClosePane(cctx, o.Pane)
		}
	})

	for _, k := range []struct{ kind, signal string }{{"codex", "liveCodex"}, {"pi", "livePi"}} {
		t.Run(k.kind, func(t *testing.T) {
			ticket := func(n int) string { return "LIVE" + k.kind + ":" + string(rune('1'+n)) }
			dir := t.TempDir()
			before := map[string]bool{}
			for _, o := range c.Opened().All() {
				before[o.Pane] = true
			}
			if _, f := c.Handle(ctx, ticket(0), Request{Action: "start", Kind: k.kind, Cwd: dir, Name: "hc-live-" + k.kind}); f != nil {
				t.Fatalf("start: %v", f)
			}
			var pane string
			deadline := time.Now().Add(90 * time.Second)
			for time.Now().Before(deadline) && pane == "" {
				for _, o := range c.Opened().All() {
					if !before[o.Pane] {
						pane = o.Pane
					}
				}
				time.Sleep(200 * time.Millisecond)
			}
			if pane == "" {
				t.Fatal("no pane opened")
			}
			for time.Now().Before(deadline) {
				a, err := cli.Get(ctx, pane)
				if err == nil && a.Kind == k.kind && (a.Status == "idle" || a.Status == "done") {
					t.Logf("%s: kind %q session %q (%s)", pane, a.Kind, a.SessionID, a.SessionKind)
					break
				}
				if err == nil && a.Status == "blocked" {
					screen, _ := cli.Read(ctx, pane, 40)
					t.Fatalf("%s is blocked on a dialog:\n%s", k.kind, screen)
				}
				time.Sleep(500 * time.Millisecond)
			}
			if _, f := c.Handle(ctx, ticket(1), Request{Action: "select", Pane: pane}); f != nil {
				t.Fatalf("select: %v", f)
			}
			prompt := "Reply with one short sentence, then exactly this fenced block and nothing after it:\n\n```patch-signals\n{\"" + k.signal + "\":\"ok\"}\n```"
			res, f := c.Handle(ctx, ticket(2), Request{Action: "send", Pane: pane, Text: prompt})
			if f != nil || res["sent"] != true {
				t.Fatalf("send = %v %v", res, f)
			}
			want := `{"` + k.signal + `":"ok"}`
			got := 0
			for end := time.Now().Add(120 * time.Second); time.Now().Before(end) && got == 0; time.Sleep(500 * time.Millisecond) {
				got = applied(fc, want)
			}
			if got != 1 {
				t.Fatalf("the block was applied %d times; last patches: %s", got, tail(fc.html(), 2000))
			}
			// A few more rounds: never applied again.
			time.Sleep(3 * time.Second)
			if n := applied(fc, want); n != 1 {
				t.Fatalf("applied %d times", n)
			}
			src := c.follower(c.sessions["LIVE"+k.kind]).source()
			t.Logf("source: %s", src)
			if src != "transcript" {
				t.Errorf("the reply came from the %s, not the transcript", src)
			}
			if !strings.Contains(fc.html(), "✓ patch-signals") {
				t.Error("no applied chip")
			}
			if _, f := c.Handle(ctx, ticket(3), Request{Action: "close", Pane: pane}); f != nil {
				t.Fatalf("close: %v", f)
			}
		})
	}
}
