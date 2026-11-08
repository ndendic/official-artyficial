package console

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/herdr"
	"herdr-console/render"
)

// Review fixes for the views: what the shell can draw, which call's switch
// is which, and what a page that takes no Patcher patch is still told.

// drawable are the (target, mode) pairs pill-views.js draws: the console and
// the Agents panel inner, the confirm bar and the error line outer.
var drawable = map[string]string{"console": "inner", "agents": "inner", "confirm": "outer", "error": "outer"}

func assertDrawable(t *testing.T, res map[string]any) {
	t.Helper()
	for _, v := range viewsOf(t, res) {
		if drawable[v.Target] != v.Mode {
			t.Errorf("the shell draws no %s view of mode %s", v.Target, v.Mode)
		}
		if n := render.CountNodes(v.Tree); n > render.MaxViewNodes {
			t.Errorf("%s view has %d nodes, over the shell's budget", v.Target, n)
		}
	}
}

func elements(nodes []render.Node, pred func(render.Element) bool) []render.Element {
	var out []render.Element
	for _, n := range nodes {
		if el, ok := n.(render.Element); ok {
			if pred(el) {
				out = append(out, el)
			}
			out = append(out, elements(el.K, pred)...)
		}
	}
	return out
}

func sess(c *Console, id string) *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id]
}

// A select or a close made with the sidebar open is the whole panel, inner:
// the shell has no outer view of the list.
func TestSidebarChangesAreTheWholePanel(t *testing.T) {
	c, h, _ := setup(t, reviewer, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "agents", Views: true}); f != nil {
		t.Fatal(f)
	}
	res, f := call(t, c, "A:2", Request{Action: "select", Pane: stranger.Pane, Views: true})
	if f != nil {
		t.Fatal(f)
	}
	assertDrawable(t, res)
	v := find(viewsOf(t, res), "agents")
	if v == nil {
		t.Fatalf("the highlight did not move: views = %+v", res["views"])
	}
	cur := elements(v.Tree, func(e render.Element) bool { return e.A["aria-current"] == "true" })
	if len(cur) != 1 || cur[0].A["data-pane"] != stranger.Pane {
		t.Errorf("current rows = %+v", cur)
	}
	// A close does the same.
	pane := startSession(t, c, h, "A:3")
	res, f = call(t, c, "A:4", Request{Action: "close", Pane: pane, Views: true})
	if f != nil {
		t.Fatal(f)
	}
	assertDrawable(t, res)
	if find(viewsOf(t, res), "agents") == nil {
		t.Errorf("the closed row stayed: views = %+v", res["views"])
	}
}

// A feed or views call is one call: it does not silence what the follower
// sends the other tabs of the browser session.
func TestAFeedCallDoesNotSilenceTheSession(t *testing.T) {
	c, h, fc := setup(t)
	appendf, user, text := transcriptFixture(t, c, "shared-1")
	a := herdr.Agent{Pane: "w9:p4", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: "shared-1"}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	// Another tab of the same browser session pulls the feed.
	for i := 2; i < 5; i++ {
		if _, f := call(t, c, "A:"+string(rune('0'+i)), Request{Action: "follow", Pane: a.Pane, Feed: true}); f != nil {
			t.Fatal(f)
		}
	}
	fc.reset()
	appendf(user("p1", "go") + text("a brand new answer"))
	waitFor(t, "the new turn reaching the Patcher", func() bool { return strings.Contains(fc.html(), "a brand new answer") })
}

// A block an agent wrote is applied by the Patcher, once, whatever feed
// calls were made; one no page took waits, and is chipped only when it was
// delivered.
func TestAgentBlockIsDeliveredNotJustChipped(t *testing.T) {
	c, h, fc := setup(t)
	appendf, user, text := transcriptFixture(t, c, "blocks-1")
	a := herdr.Agent{Pane: "w9:p6", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: "blocks-1"}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	if _, f := call(t, c, "A:2", Request{Action: "follow", Pane: a.Pane, Feed: true}); f != nil {
		t.Fatal(f)
	}
	fence := "```"
	block := func(id string) string {
		return text(fence + "patch-elements selector=\"#aside\" mode=\"append\"\n<p id=\"" + id + "\">x</p>\n" + fence + "\nDone.")
	}
	chips := func() (n, ok int) {
		f := c.follower(sess(c, "A"))
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, chip := range f.chips {
			n++
			if chip.OK {
				ok++
			}
		}
		return
	}
	appendf(user("p1", "go") + block("first-block"))
	waitFor(t, "the block at the Patcher", func() bool { return appliedTo(fc, "#aside", "first-block") == 1 })
	waitFor(t, "its chip", func() bool { n, ok := chips(); return n == 1 && ok == 1 })
	// Nobody listens: not delivered, so not chipped, and sent once the page is back.
	fc.setAbsent(true)
	appendf(user("p2", "again") + block("second-block"))
	time.Sleep(300 * time.Millisecond)
	if n, _ := chips(); n != 1 {
		t.Fatalf("a block no page took was chipped (%d chips)", n)
	}
	fc.setAbsent(false)
	waitFor(t, "the second block", func() bool { return appliedTo(fc, "#aside", "second-block") == 1 })
	waitFor(t, "its chip", func() bool { n, ok := chips(); return n == 2 && ok == 2 })
}

// A close from the sidebar that redraws nothing still names the chip.
func TestCloseWithoutViewsClearsTheChip(t *testing.T) {
	c, h, _ := setup(t)
	pane := startSession(t, c, h, "A:1")
	call(t, c, "A:2", Request{Action: "select", Pane: pane})
	res, f := call(t, c, "A:3", Request{Action: "close", Pane: pane})
	if f != nil {
		t.Fatal(f)
	}
	if v, ok := res["agent"]; !ok || v != nil {
		t.Errorf("agent = %v (present %v), want null", v, ok)
	}
}

// The person closed the console: the feed does not open it again, and it
// shows the thread again once the person opens it.
func TestFeedDoesNotReopenADismissedConsole(t *testing.T) {
	c, _, _ := setup(t, reviewer)
	call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true})
	call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
	res, _ := call(t, c, "A:3", Request{Action: "dismiss", Views: true})
	if v := find(viewsOf(t, res), "console"); v == nil || len(v.Tree) != 0 {
		t.Fatalf("dismiss did not empty the console: %+v", res["views"])
	}
	for i := 0; i < 2; i++ {
		res, _ = call(t, c, "A:4", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
		if find(viewsOf(t, res), "console") != nil {
			t.Fatalf("round %d: the feed reopened a dismissed console: %+v", i, res["views"])
		}
	}
	call(t, c, "A:5", Request{Action: "select", Pane: reviewer.Pane, Views: true})
	res, _ = call(t, c, "A:6", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
	_ = res // the thread is open again: the next change is drawn
	s := sess(c, "A")
	s.mu.Lock()
	view := s.view
	s.mu.Unlock()
	if view != "thread" {
		t.Errorf("view = %q after select", view)
	}
}

// The feed's panel carries no filter: the Find field is the page's own,
// and the last "agents" call's text must not overwrite what is typed.
func TestAgentsFeedLeavesTheFindField(t *testing.T) {
	c, _, _ := setup(t, reviewer, stranger)
	findVal := func(res map[string]any) (string, bool) {
		v := find(viewsOf(t, res), "agents")
		if v == nil {
			return "", false
		}
		els := elements(v.Tree, func(e render.Element) bool { return e.T == "input" && strings.Contains(e.A["class"], "ha-find") })
		if len(els) != 1 {
			t.Fatalf("find fields = %d", len(els))
		}
		return els[0].A["value"], true
	}
	res, _ := call(t, c, "A:1", Request{Action: "agents", Filter: "abc", Views: true})
	if got, ok := findVal(res); !ok || got != "abc" {
		t.Fatalf("the call's own panel lost its filter: %q %v", got, ok)
	}
	res, _ = call(t, c, "A:2", Request{Action: "agents", Feed: true})
	if got, ok := findVal(res); !ok || got != "" {
		t.Errorf("the feed put the old filter back: %q %v", got, ok)
	}
}

// A start made from a page that takes no Patcher patch: the reply carries
// the thread and the chip the start ended with, and a start that fails
// shows its error.
func TestStartAnswersWithThreadAndChip(t *testing.T) {
	c, _, fc := setup(t)
	fc.setAbsent(true) // no Caller patch reaches this page
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	if f != nil {
		t.Fatal(f)
	}
	assertDrawable(t, res)
	views := viewsOf(t, res)
	var last *View
	for i := range views {
		if views[i].Target == "console" {
			last = &views[i]
		}
	}
	if last == nil || !hasID(last.Tree, "hc-head") || !strings.Contains(treeText(last.Tree), res["name"].(string)) {
		t.Fatalf("the reply ends on %+v, not the thread", last)
	}
	agent, _ := res["agent"].(map[string]any)
	if agent["name"] != res["name"] {
		t.Errorf("agent = %v", res["agent"])
	}
	if res["view"] != "thread" {
		t.Errorf("view = %v", res["view"])
	}
}

func TestStartErrorReachesThePage(t *testing.T) {
	c, h, fc := setup(t)
	fc.setAbsent(true)
	h.SetOpenError(errors.New("herdr says no"))
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	if f != nil {
		t.Fatal(f)
	}
	assertDrawable(t, res)
	v := find(viewsOf(t, res), "error")
	if v == nil || !strings.Contains(treeText(v.Tree), "herdr says no") {
		t.Fatalf("the start error is not in the reply: %+v", res["views"])
	}
}

// A start slower than the call's wait leaves its end for the next reply.
func TestSlowStartEndsInTheNextReply(t *testing.T) {
	c, _, fc := setup(t)
	fc.setAbsent(true)
	c.StartWait = time.Nanosecond
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	if f != nil {
		t.Fatal(f)
	}
	all := viewsOf(t, res)
	var agent any = res["agent"]
	waitFor(t, "the start to end", func() bool {
		s := sess(c, "A")
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.view == "thread" && s.hasLateAgent || res["agent"] != nil
	})
	res, f = call(t, c, "A:2", Request{Action: "open", Views: true})
	if f != nil {
		t.Fatal(f)
	}
	all = append(all, viewsOf(t, res)...)
	if res["agent"] != nil {
		agent = res["agent"]
	}
	if agent == nil {
		t.Error("no reply carried the chip")
	}
	found := false
	for _, v := range all {
		if v.Target == "console" && hasID(v.Tree, "hc-head") {
			found = true
		}
	}
	if !found {
		t.Errorf("no reply carried the thread: %+v", all)
	}
}

// A session that ends while the page takes no Patcher patch: the next
// reply clears the chip and says so.
func TestEndedSessionReachesThePage(t *testing.T) {
	c, h, fc := setup(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	fc.setAbsent(true)
	h.Remove(reviewer.Pane)
	waitFor(t, "the end to be noticed", func() bool {
		s := sess(c, "A")
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.hasLateAgent
	})
	res, f := call(t, c, "A:2", Request{Action: "open", Views: true})
	if f != nil {
		t.Fatal(f)
	}
	assertDrawable(t, res)
	if v, ok := res["agent"]; !ok || v != nil {
		t.Errorf("agent = %v (present %v), want null", v, ok)
	}
	if v := find(viewsOf(t, res), "error"); v == nil || !strings.Contains(treeText(v.Tree), "ended") {
		t.Errorf("no ended line: %+v", res["views"])
	}
	// Once only.
	res, _ = call(t, c, "A:3", Request{Action: "open", Views: true})
	if find(viewsOf(t, res), "error") != nil {
		t.Error("the ended line came again")
	}
}

// One long answer does not make the shell refuse the whole console.
func TestLongAnswerFitsTheShellsBudget(t *testing.T) {
	c, h, _ := setup(t)
	appendf, user, text := transcriptFixture(t, c, "long-1")
	a := herdr.Agent{Pane: "w9:p7", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: "long-1"}
	h.Put(a)
	appendf(user("p1", "list") + text(strings.Repeat("- item `x` here\n", 700)) + user("p2", "again") + text("short second answer"))
	res, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane, Views: true})
	if f != nil {
		t.Fatal(f)
	}
	assertDrawable(t, res)
	v := find(viewsOf(t, res), "console")
	if v == nil {
		t.Fatal("no console view")
	}
	txt := treeText(v.Tree)
	if !strings.Contains(txt, render.CutNote) {
		t.Error("the long answer was not cut with a note")
	}
	if !strings.Contains(txt, "short second answer") || !hasID(v.Tree, "hc-head") {
		t.Error("the cut took more than the long answer")
	}
	// The feed stays inside the budget too.
	res, _ = call(t, c, "A:2", Request{Action: "follow", Pane: a.Pane, Feed: true, Resync: true})
	assertDrawable(t, res)
}

// ---- what the dedupe and loop guards are for ----

func TestFreshPageGetsThePanelAgain(t *testing.T) {
	c, _, _ := setup(t, reviewer)
	call(t, c, "A:1", Request{Action: "agents", Feed: true})
	res, _ := call(t, c, "A:2", Request{Action: "agents", Feed: true})
	if find(viewsOf(t, res), "agents") != nil {
		t.Fatal("an unchanged panel was sent again")
	}
	call(t, c, "A:3", Request{Action: "follow", Feed: true, Fresh: true})
	res, _ = call(t, c, "A:4", Request{Action: "agents", Feed: true})
	if find(viewsOf(t, res), "agents") == nil {
		t.Error("a page that just loaded got no panel")
	}
}

func TestResyncedPageGetsThePanelAgain(t *testing.T) {
	c, _, _ := setup(t, reviewer)
	call(t, c, "A:1", Request{Action: "agents", Feed: true})
	call(t, c, "A:2", Request{Action: "follow", Feed: true, Resync: true})
	res, _ := call(t, c, "A:3", Request{Action: "agents", Feed: true})
	if find(viewsOf(t, res), "agents") == nil {
		t.Error("a resynced page got no panel")
	}
}

// An "agents" call that is not a feed may have drawn the panel by Patcher,
// which a page that draws views never heard: the feed sends it again.
func TestAgentsCallResetsTheFeedsMemory(t *testing.T) {
	c, _, _ := setup(t, reviewer)
	call(t, c, "A:1", Request{Action: "agents", Feed: true})
	call(t, c, "A:2", Request{Action: "agents"})
	res, _ := call(t, c, "A:3", Request{Action: "agents", Feed: true})
	if find(viewsOf(t, res), "agents") == nil {
		t.Error("the feed kept its memory across an agents call")
	}
}

// Picking the thread again (select) resets the feed's memory: the page was
// just given a thread of its own render, and the feed's next one is sent.
func TestSelectAndPickerResetTheFeedsThread(t *testing.T) {
	c, _, _ := setup(t, reviewer)
	call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true})
	res, _ := call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
	if find(viewsOf(t, res), "console") == nil {
		t.Fatal("the feed gave the thread after a select")
	}
	res, _ = call(t, c, "A:3", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
	if find(viewsOf(t, res), "console") != nil {
		t.Fatal("an unchanged thread was sent again")
	}
	call(t, c, "A:4", Request{Action: "open", Views: true})
	call(t, c, "A:5", Request{Action: "select", Pane: reviewer.Pane, Views: true})
	res, _ = call(t, c, "A:6", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
	if find(viewsOf(t, res), "console") == nil {
		t.Error("the feed kept its memory across a picker and a select")
	}
}

// A call that draws views starts no live loop (nothing to send to), one that
// does not starts it.
func TestViewsCallStartsNoPanelLoop(t *testing.T) {
	c, _, _ := setup(t, reviewer)
	call(t, c, "A:1", Request{Action: "agents", Views: true})
	s := sess(c, "A")
	s.mu.Lock()
	loop := s.panel
	s.mu.Unlock()
	if loop != nil {
		t.Error("a views call started the panel loop")
	}
	call(t, c, "B:1", Request{Action: "agents"})
	s = sess(c, "B")
	s.mu.Lock()
	loop = s.panel
	s.mu.Unlock()
	if loop == nil {
		t.Error("a Patcher call started no panel loop")
	}
}

// A views call sends no signal, so the badge it did not send is not
// recorded as heard: the next Patcher call sends it.
func TestViewsCallDoesNotRecordTheBadgeAsSent(t *testing.T) {
	c, _, fc, _ := setupWatched(t, docsSite) // done: the badge is "done"
	if _, f := call(t, c, "A:1", Request{Action: "follow", Pane: docsSite.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	if len(badges(fc)) != 0 {
		t.Fatalf("a views call sent the badge: %v", badges(fc))
	}
	if _, f := call(t, c, "A:2", Request{Action: "follow", Pane: docsSite.Pane}); f != nil {
		t.Fatal(f)
	}
	if !strings.Contains(lastBadge(fc), "done") {
		t.Errorf("the badge never reached the Patcher: %v", badges(fc))
	}
}

// ---- late draws on an open vouched Stream (review round 7, B1) ----

// openStream is a Patcher as an open vouched Stream makes it: core accepts
// every Caller patch (nil), and the sink then drops it.
type openStream struct{ Patcher }

func (openStream) PatchElements(context.Context, string, string, ...publisher.PatchOption) error {
	return nil
}
func (openStream) PatchSignals(context.Context, json.RawMessage, bool) error { return nil }

func setupOpenStream(t *testing.T, agents ...herdr.Agent) (*Console, *herdr.Fake) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := herdr.NewFake(agents...)
	c := New(ctx, Config{Crew: crew, ConsoleWorkspace: "Console", DataDir: t.TempDir(), ClaudeProjects: t.TempDir()}, h, nil,
		func(ticket string) Patcher { return openStream{(&fakeCallers{}).new(ticket)} })
	c.Tick = 20 * time.Millisecond
	t.Cleanup(func() { c.Close(); cancel() })
	return c, h
}

func TestStartOnAnOpenVouchedStream(t *testing.T) {
	c, _ := setupOpenStream(t)
	c.StartWait = time.Nanosecond
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	if f != nil {
		t.Fatal(f)
	}
	all := viewsOf(t, res)
	var agent any = res["agent"]
	waitFor(t, "the start to end", func() bool {
		s := sess(c, "A")
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.hasLateAgent && len(s.lateViews) > 0
	})
	res, f = call(t, c, "A:2", Request{Action: "open", Views: true})
	if f != nil {
		t.Fatal(f)
	}
	all = append(all, viewsOf(t, res)...)
	if res["agent"] != nil {
		agent = res["agent"]
	}
	if agent == nil {
		t.Error("no reply carried the chip")
	}
	thread := false
	for _, v := range all {
		if v.Target == "console" && hasID(v.Tree, "hc-head") {
			thread = true
		}
	}
	if !thread {
		t.Errorf("no reply carried the thread: %+v", all)
	}
}

func TestEndedSessionOnAnOpenVouchedStream(t *testing.T) {
	c, h := setupOpenStream(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	h.Remove(reviewer.Pane)
	waitFor(t, "the end to be noticed", func() bool {
		s := sess(c, "A")
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.hasLateAgent
	})
	res, f := call(t, c, "A:2", Request{Action: "open", Views: true})
	if f != nil {
		t.Fatal(f)
	}
	if v, ok := res["agent"]; !ok || v != nil {
		t.Errorf("agent = %v (present %v), want null", v, ok)
	}
	if v := find(viewsOf(t, res), "error"); v == nil || !strings.Contains(treeText(v.Tree), "ended") {
		t.Errorf("no ended line: %+v", res["views"])
	}
}

func TestStartErrorOnAnOpenVouchedStream(t *testing.T) {
	c, h := setupOpenStream(t)
	h.SetOpenError(errors.New("herdr says no"))
	c.StartWait = time.Nanosecond
	if _, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true}); f != nil {
		t.Fatal(f)
	}
	waitFor(t, "the error", func() bool {
		s := sess(c, "A")
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.lateViews) > 0
	})
	res, _ := call(t, c, "A:2", Request{Action: "open", Views: true})
	if v := find(viewsOf(t, res), "error"); v == nil || !strings.Contains(treeText(v.Tree), "herdr says no") {
		t.Errorf("the start error is not in the reply: %+v", res["views"])
	}
}

// A page that never asked for views keeps the Patcher first: nothing waits
// in the outbox when the Patcher took the draw.
func TestLateDrawWithoutViewsStillPatches(t *testing.T) {
	c, _, fc := setup(t)
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir()})
	if f != nil {
		t.Fatal(f)
	}
	_ = res
	waitFor(t, "the thread patch", func() bool { return strings.Contains(fc.html(), "hc-head") })
	s := sess(c, "A")
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lateViews) != 0 || s.hasLateAgent {
		t.Errorf("the outbox holds %d views though the Patcher took them", len(s.lateViews))
	}
}
