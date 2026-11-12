package console

import (
	"context"
	"errors"
	"testing"
	"time"

	"herdr-console/herdr"
)

// sidebar opens a pane beside every new tab, as herdr-sidebar does.
func sidebar(h *herdr.Fake) {
	h.OnStart(func(tab string) string { return tab + "-sidebar" })
}

// waitCompanions waits until the console has recorded the companions of
// pane (it does so right after herdr's agent start returns).
func waitCompanions(t *testing.T, c *Console, pane string) []string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if o, ok := c.Opened().Get(pane); ok && len(o.Companions) > 0 {
			return o.Companions
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no companions recorded for %s", pane)
	return nil
}

// Close leaves no tab behind that holds only a plugin's sidebar.
func TestCloseClosesTheTabLeftWithItsSidebar(t *testing.T) {
	c, h, _ := setup(t)
	sidebar(h)
	pane := startSession(t, c, h, "A:1")
	o, _ := c.Opened().Get(pane)
	if got := waitCompanions(t, c, pane); len(got) != 1 || got[0] != o.Tab+"-sidebar" {
		t.Fatalf("companions = %v", got)
	}
	if _, f := call(t, c, "A:2", Request{Action: "close", Pane: pane}); f != nil {
		t.Fatalf("close = %v", f)
	}
	if len(h.Closed) != 1 || h.Closed[0] != pane {
		t.Fatalf("closed panes = %v", h.Closed)
	}
	if len(h.ClosedTabs) != 1 || h.ClosedTabs[0] != o.Tab {
		t.Fatalf("closed tabs = %v, want [%s]", h.ClosedTabs, o.Tab)
	}
	if left, _ := h.TabPanes(context.Background(), o.Workspace, o.Tab); len(left) != 0 {
		t.Fatalf("the tab still holds %v", left)
	}
}

// A pane the person split into the console's tab keeps the tab, sidebar
// and all.
func TestCloseKeepsATabThePersonSplit(t *testing.T) {
	c, h, _ := setup(t)
	sidebar(h)
	pane := startSession(t, c, h, "A:1")
	waitCompanions(t, c, pane)
	o, _ := c.Opened().Get(pane)
	h.AddPane(o.Tab, o.Workspace+":p99")
	if _, f := call(t, c, "A:2", Request{Action: "close", Pane: pane}); f != nil {
		t.Fatalf("close = %v", f)
	}
	if len(h.ClosedTabs) != 0 {
		t.Fatalf("closed tabs = %v: the person's pane went with it", h.ClosedTabs)
	}
	left, _ := h.TabPanes(context.Background(), o.Workspace, o.Tab)
	if len(left) != 2 {
		t.Fatalf("the tab holds %v, want the sidebar and the person's pane", left)
	}
}

// A session closed from the picker after its agent failed to start: its
// tab goes too.
func TestCloseAfterAFailedStartClosesTheTab(t *testing.T) {
	c, h, _ := setup(t)
	sidebar(h)
	h.SetStartError(errors.New("unknown kind"))
	if _, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir()}); f != nil {
		t.Fatalf("start = %v", f)
	}
	var o OpenedSession
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(o.Companions) == 0 {
		for _, r := range c.Opened().All() {
			o = r
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(o.Companions) == 0 {
		t.Fatalf("no companions recorded after a failed start: %+v", o)
	}
	if _, f := call(t, c, "A:2", Request{Action: "close", Pane: o.Pane}); f != nil {
		t.Fatalf("close = %v", f)
	}
	if len(h.ClosedTabs) != 1 || h.ClosedTabs[0] != o.Tab {
		t.Fatalf("closed tabs = %v, want [%s]", h.ClosedTabs, o.Tab)
	}
}

// A session recorded before companions were (console 1.2.0) keeps the old
// rule: the pane alone, never a tab whose other panes are unknown.
func TestCloseOfAnOldRecordClosesOnlyThePane(t *testing.T) {
	c, h, _ := setup(t)
	pane := startSession(t, c, h, "A:1")
	o, _ := c.Opened().Get(pane)
	o.Companions = nil
	c.Opened().Put(o)
	h.AddPane(o.Tab, o.Tab+"-sidebar")
	if _, f := call(t, c, "A:2", Request{Action: "close", Pane: pane}); f != nil {
		t.Fatalf("close = %v", f)
	}
	if len(h.ClosedTabs) != 0 {
		t.Fatalf("closed tabs = %v", h.ClosedTabs)
	}
}

// The companions survive a restart of the Plugin (the registry file).
func TestCompanionsAreKeptInTheRegistry(t *testing.T) {
	path := t.TempDir() + "/opened.json"
	r := LoadRegistry(path)
	r.Put(OpenedSession{Pane: "w1:p2", Tab: "w1:t2", Name: "a"})
	if !r.Update("w1:p2", func(o *OpenedSession) { o.Companions = []string{"w1:p3"} }) {
		t.Fatal("update of a registered pane said no")
	}
	if r.Update("w9:p9", func(o *OpenedSession) { o.Companions = []string{"x"} }) || LoadRegistry(path).Has("w9:p9") {
		t.Fatal("update registered a pane that was not there")
	}
	got, _ := LoadRegistry(path).Get("w1:p2")
	if len(got.Companions) != 1 || got.Companions[0] != "w1:p3" {
		t.Fatalf("after reload: %+v", got)
	}
}
