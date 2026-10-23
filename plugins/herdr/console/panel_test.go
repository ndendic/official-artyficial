package console

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"herdr-console/herdr"
	"herdr-console/render"
)

// setupWatched is setup with a herdr watcher (never Run: a test refreshes
// it, which wakes the listeners as a socket event would).
func setupWatched(t *testing.T, agents ...herdr.Agent) (*Console, *herdr.Fake, *fakeCallers, *herdr.Watcher) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := herdr.NewFake(agents...)
	h.SetWorkspaces(herdr.Workspace{ID: "w9", Label: "Scratch"}, herdr.Workspace{ID: "w1", Label: "Projects"})
	w := herdr.NewWatcher(h, "")
	if err := w.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	fc := &fakeCallers{}
	c := New(ctx, Config{Crew: crew, ConsoleWorkspace: "Console", DataDir: t.TempDir(), ClaudeProjects: t.TempDir()}, h, w, fc.new)
	c.Tick = 20 * time.Millisecond
	c.PanelInterval = 40 * time.Millisecond
	t.Cleanup(func() { c.Close(); cancel() })
	return c, h, fc, w
}

func panelRenders(fc *fakeCallers) (whole, lists []string) {
	for _, p := range fc.all() {
		switch p.selector {
		case render.AgentsSelector:
			whole = append(whole, p.html)
		case "#" + render.AgentsListID:
			lists = append(lists, p.html)
		}
	}
	return whole, lists
}

func badges(fc *fakeCallers) []string {
	var out []string
	for _, p := range fc.all() {
		if strings.Contains(p.signals, "herdrBadge") {
			out = append(out, p.signals)
		}
	}
	return out
}

func lastBadge(fc *fakeCallers) string {
	b := badges(fc)
	if len(b) == 0 {
		return ""
	}
	return b[len(b)-1]
}

func sessionOf(c *Console, id string) *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id]
}

func TestBadge(t *testing.T) {
	for _, tc := range []struct {
		statuses []string
		want     string
	}{
		{[]string{"idle", "working"}, ""},
		{[]string{"idle", "done"}, "done"},
		{[]string{"done", "blocked", "idle"}, "blocked"},
		{nil, ""},
	} {
		var agents []herdr.Agent
		for _, s := range tc.statuses {
			agents = append(agents, herdr.Agent{Status: s})
		}
		if got := Badge(agents); got != tc.want {
			t.Errorf("Badge(%v) = %q, want %q", tc.statuses, got, tc.want)
		}
	}
	if string(badgeSignal("")) != `{"herdrBadge":null}` || string(badgeSignal("done")) != `{"herdrBadge":"done"}` {
		t.Error("badge signal shape")
	}
}

// "agents" renders the whole panel into the region: groups per workspace
// in herdr's order with counts, the attached session marked, close only on
// a session the console opened, and the badge.
func TestAgentsActionRendersThePanel(t *testing.T) {
	c, h, fc, w := setupWatched(t, reviewer, docsSite, stranger)
	pane := startSession(t, c, h, "A:1")
	// The Fake made the herdr workspace Console last.
	_ = w.Refresh(context.Background())
	if _, f := call(t, c, "A:2", Request{Action: "select", Pane: docsSite.Pane}); f != nil {
		t.Fatal(f)
	}
	fc.reset()
	res, f := call(t, c, "A:3", Request{Action: "agents", Filter: "ex"})
	if f != nil || res["ok"] != true || res["view"] != "agents" || res["sessions"] != 4 {
		t.Fatalf("agents = %v %v", res, f)
	}
	whole, _ := panelRenders(fc)
	if len(whole) != 1 {
		t.Fatalf("whole renders = %d", len(whole))
	}
	html := whole[0]
	scratch, projects, con := strings.Index(html, "<span>Scratch</span>"), strings.Index(html, "<span>Projects</span>"), strings.Index(html, "<span>Console</span>")
	if scratch < 0 || projects < scratch || con < projects {
		t.Fatalf("groups not in herdr's order: %s", html)
	}
	for _, want := range []string{`<span>Projects</span><span class="ha-count">2</span>`, `<span>Scratch</span><span class="ha-count">1</span>`, `value="ex"`, `data-sessions="4"`} {
		if !strings.Contains(html, want) {
			t.Errorf("panel misses %s", want)
		}
	}
	cur := html[strings.Index(html, `aria-current="true"`):]
	if !strings.HasPrefix(cur, `aria-current="true" data-pane="`+docsSite.Pane+`"`) || !strings.Contains(cur[:600], "· in the pill") {
		t.Errorf("the attached row is not marked: %s", cur)
	}
	if n := strings.Count(html, `class="ha-close"`); n != 1 || !strings.Contains(html, `&#34;close&#34;,&#34;pane&#34;:&#34;`+pane+`&#34;`) {
		t.Errorf("close buttons = %d: %s", n, html)
	}
	if n := strings.Count(html, `class="ha-crew"`); n != 2 {
		t.Errorf("crew marks = %d", n)
	}
	// docs-site is done, the rest idle: the badge is done.
	if b := lastBadge(fc); b != `{"herdrBadge":"done"}` {
		t.Errorf("badge = %q", b)
	}
}

// While the panel is open, status changes re-render the list without a
// call, coalesced and throttled, and move the badge.
func TestPanelIsLiveAndThrottled(t *testing.T) {
	c, h, fc, w := setupWatched(t, reviewer, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "agents"}); f != nil {
		t.Fatal(f)
	}
	if b := lastBadge(fc); b != `{"herdrBadge":null}` {
		t.Fatalf("badge = %q", b)
	}
	fc.reset()
	ctx := context.Background()
	start := time.Now()
	// 30 changes in about 300 ms.
	for i := 0; i < 30; i++ {
		a := stranger
		a.Status = []string{"working", "idle"}[i%2]
		a.Cwd = fmt.Sprintf("/tmp/c%d", i) // every change shows
		h.Put(a)
		_ = w.Refresh(ctx)
		time.Sleep(10 * time.Millisecond)
	}
	b := stranger
	b.Status = "blocked"
	h.Put(b)
	_ = w.Refresh(ctx)
	waitFor(t, "the blocked row", func() bool {
		_, lists := panelRenders(fc)
		return len(lists) > 0 && strings.Contains(lists[len(lists)-1], `data-pane="`+stranger.Pane+`"`) && strings.Contains(lists[len(lists)-1], `data-status="blocked"`)
	})
	elapsed := time.Since(start)
	_, lists := panelRenders(fc)
	if max := int(elapsed/c.PanelInterval) + 2; len(lists) > max {
		t.Errorf("%d list renders in %v, want at most %d", len(lists), elapsed, max)
	}
	if len(lists) < 2 {
		t.Errorf("only %d list renders for 31 changes", len(lists))
	}
	waitFor(t, "the blocked badge", func() bool { return lastBadge(fc) == `{"herdrBadge":"blocked"}` })
	// Nothing changes: nothing is sent.
	time.Sleep(150 * time.Millisecond)
	fc.reset()
	time.Sleep(150 * time.Millisecond)
	if _, lists := panelRenders(fc); len(lists) != 0 {
		t.Errorf("%d renders with no change", len(lists))
	}
}

// The loop ends when the ticket expires, and the next follow starts it
// again; a list no page heard is sent again once a page listens.
func TestPanelStopsOnExpiryAndRestartsOnFollow(t *testing.T) {
	c, h, fc, w := setupWatched(t, reviewer, stranger)
	ctx := context.Background()
	if _, f := call(t, c, "A:1", Request{Action: "agents"}); f != nil {
		t.Fatal(f)
	}
	s := sessionOf(c, "A")
	s.mu.Lock()
	first := s.panel
	s.mu.Unlock()
	fc.mu.Lock()
	fc.expires = time.Now().Add(-time.Minute)
	fc.mu.Unlock()
	waitFor(t, "the loop to end", func() bool { return first.ctx.Err() != nil })

	a := stranger
	a.Status = "working"
	h.Put(a)
	_ = w.Refresh(ctx)
	time.Sleep(120 * time.Millisecond)
	if _, lists := panelRenders(fc); len(lists) != 0 {
		t.Fatalf("an ended loop rendered: %v", lists)
	}

	fc.mu.Lock()
	fc.expires = time.Time{}
	fc.mu.Unlock()
	fc.setAbsent(true) // the page is not back yet
	if _, f := call(t, c, "A:2", Request{Action: "follow", Resync: true}); f != nil {
		t.Fatal(f)
	}
	s.mu.Lock()
	second := s.panel
	s.mu.Unlock()
	if second == first || second.ctx.Err() != nil {
		t.Fatal("follow did not start the loop again")
	}
	time.Sleep(150 * time.Millisecond)
	fc.setAbsent(false)
	waitFor(t, "the list once a page listens", func() bool {
		_, lists := panelRenders(fc)
		return len(lists) == 1 && strings.Contains(lists[0], `data-status="working"`)
	})
	time.Sleep(150 * time.Millisecond)
	if _, lists := panelRenders(fc); len(lists) != 1 {
		t.Errorf("the list was sent %d times", len(lists))
	}
}

// permission_denied ends the loop for good; "agents" starts it again.
func TestPanelStopsOnDenied(t *testing.T) {
	c, h, fc, w := setupWatched(t, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "agents"}); f != nil {
		t.Fatal(f)
	}
	s := sessionOf(c, "A")
	s.mu.Lock()
	first := s.panel
	s.mu.Unlock()
	fc.mu.Lock()
	fc.deny = true
	fc.mu.Unlock()
	a := stranger
	a.Status = "done"
	h.Put(a)
	_ = w.Refresh(context.Background())
	waitFor(t, "the loop to end", func() bool { return first.ctx.Err() != nil })
	fc.mu.Lock()
	fc.deny = false
	fc.mu.Unlock()
	if _, f := call(t, c, "A:2", Request{Action: "agents"}); f != nil {
		t.Fatal(f)
	}
	s.mu.Lock()
	again := s.panel
	s.mu.Unlock()
	if again == first || again.ctx.Err() != nil {
		t.Fatal("agents did not start the loop again")
	}
}

// A select from the panel is the picker's select (pillAgent, the thread)
// and moves the panel's highlight at once.
func TestSelectFromThePanelMovesTheHighlight(t *testing.T) {
	c, _, fc, _ := setupWatched(t, reviewer, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "agents"}); f != nil {
		t.Fatal(f)
	}
	fc.reset()
	res, f := call(t, c, "A:2", Request{Action: "select", Pane: stranger.Pane})
	if f != nil || res["view"] != "thread" || res["pane"] != stranger.Pane {
		t.Fatalf("select = %v %v", res, f)
	}
	if !strings.Contains(fc.html(), `"pillAgent":{`) || regionRenders(fc, "thread") == 0 {
		t.Fatal("select from the panel is not the picker's select")
	}
	_, lists := panelRenders(fc)
	if len(lists) == 0 {
		t.Fatal("the highlight did not move")
	}
	l := lists[len(lists)-1]
	if !strings.Contains(l, `aria-current="true" data-pane="`+stranger.Pane+`"`) || strings.Count(l, "aria-current") != 1 {
		t.Errorf("highlight = %s", l)
	}
	// A select with the panel never opened patches no panel.
	c2, _, fc2, _ := setupWatched(t, stranger)
	if _, f := call(t, c2, "B:1", Request{Action: "select", Pane: stranger.Pane}); f != nil {
		t.Fatal(f)
	}
	if whole, lists := panelRenders(fc2); len(whole)+len(lists) != 0 {
		t.Error("a closed panel was patched")
	}
}

// With the session chip shown (a session selected) the badge follows
// every session's status, panel open or not.
func TestBadgeFollowsStatusWithTheChip(t *testing.T) {
	c, h, fc, w := setupWatched(t, reviewer, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: stranger.Pane}); f != nil {
		t.Fatal(f)
	}
	waitFor(t, "a first badge", func() bool { return lastBadge(fc) == `{"herdrBadge":null}` })
	a := reviewer
	a.Status = "blocked"
	h.Put(a)
	_ = w.Refresh(context.Background())
	waitFor(t, "the blocked badge", func() bool { return lastBadge(fc) == `{"herdrBadge":"blocked"}` })
	a.Status = "done"
	h.Put(a)
	_ = w.Refresh(context.Background())
	waitFor(t, "the done badge", func() bool { return lastBadge(fc) == `{"herdrBadge":"done"}` })
	n := len(badges(fc))
	time.Sleep(150 * time.Millisecond)
	if len(badges(fc)) != n {
		t.Error("an unchanged badge was sent again")
	}
}

// A fresh page gets the panel whole again on the next push.
func TestFreshFollowRendersThePanelWhole(t *testing.T) {
	c, _, fc, _ := setupWatched(t, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "agents"}); f != nil {
		t.Fatal(f)
	}
	fc.reset()
	if _, f := call(t, c, "A:2", Request{Action: "follow", Fresh: true}); f != nil {
		t.Fatal(f)
	}
	waitFor(t, "the panel whole", func() bool { whole, _ := panelRenders(fc); return len(whole) == 1 })
	if b := lastBadge(fc); b != `{"herdrBadge":null}` {
		t.Errorf("badge after a fresh page = %q", b)
	}
}

func TestAgentsRefusesALongFilter(t *testing.T) {
	c, _, fc, _ := setupWatched(t, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "agents", Filter: strings.Repeat("x", 129)}); f == nil || f.Code != invalid {
		t.Fatalf("long filter = %v", f)
	}
	if whole, lists := panelRenders(fc); len(whole)+len(lists) != 0 {
		t.Error("a refused call patched the panel")
	}
}
