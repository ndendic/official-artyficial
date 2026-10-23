//go:build live

// The panel live test reads a real herdr (list and workspaces only: it
// opens, sends and focuses nothing) and renders the Agents panel with a
// fake page.
//
//	HERDR_LIVE=1 go test -tags live -run TestLiveAgentsPanel -v ./console
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

func TestLiveAgentsPanel(t *testing.T) {
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
	c := New(ctx, Config{Crew: crew, ConsoleWorkspace: "Console-test", DataDir: t.TempDir()}, cli, watch, fc.new)
	defer c.Close()
	for end := time.Now().Add(5 * time.Second); len(watch.Agents()) == 0 && time.Now().Before(end); {
		time.Sleep(100 * time.Millisecond)
	}
	res, f := c.Handle(ctx, "LIVEP:1", Request{Action: "agents"})
	if f != nil {
		t.Fatalf("agents: %v", f)
	}
	whole, _ := panelRenders(fc)
	if len(whole) != 1 || !strings.Contains(whole[0], `class="ha-panel"`) {
		t.Fatalf("no panel: %v", whole)
	}
	if len(whole[0]) > 64<<10 {
		t.Errorf("the panel is %d bytes, over the region's 64 KB", len(whole[0]))
	}
	spaces, _ := cli.Workspaces(ctx)
	t.Logf("result %v; %d workspaces; %d bytes; badge %s; live events %v", res, len(spaces), len(whole[0]), lastBadge(fc), watch.Live())
	for _, w := range spaces {
		if !strings.Contains(whole[0], `data-ws="`+w.ID+`"`) {
			t.Logf("workspace %s holds no agent", w.ID)
		}
	}
}
