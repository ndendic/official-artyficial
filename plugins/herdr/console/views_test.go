package console

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"herdr-console/render"
)

// The reply's "views": what a vouched Page (no Caller patches) draws.

func viewsOf(t *testing.T, res map[string]any) []View {
	t.Helper()
	v, _ := res["views"].([]View)
	return v
}

func find(views []View, target string) *View {
	for i := range views {
		if views[i].Target == target {
			return &views[i]
		}
	}
	return nil
}

// hasID reports whether any element of the tree has that id attribute.
func hasID(nodes []render.Node, id string) bool {
	for _, n := range nodes {
		if el, ok := n.(render.Element); ok {
			if el.A["id"] == id || hasID(el.K, id) {
				return true
			}
		}
	}
	return false
}

func treeText(nodes []render.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		switch v := n.(type) {
		case string:
			b.WriteString(v)
		case render.Element:
			b.WriteString(treeText(v.K))
		}
	}
	return b.String()
}

func TestOpenReturnsThePickerAsAView(t *testing.T) {
	c, _, fc := setup(t, reviewer, stranger)
	res, f := call(t, c, "A:1", Request{Action: "open"})
	if f != nil {
		t.Fatal(f)
	}
	views := viewsOf(t, res)
	if len(views) != 1 || views[0].Target != "console" || views[0].Mode != "inner" {
		t.Fatalf("views = %+v", views)
	}
	if !hasID(views[0].Tree, render.ViewID) || !strings.Contains(treeText(views[0].Tree), "Reviewer") {
		t.Errorf("the picker is not in the tree: %+v", views[0].Tree)
	}
	// The Caller patch stays the first path.
	if !strings.Contains(fc.html(), `data-view="picker"`) {
		t.Error("the Patcher did not get the picker")
	}
	// The reply marshals to the {t,a,k} shape.
	raw, err := json.Marshal(res)
	if err != nil || !strings.Contains(string(raw), `"views":[{"target":"console","mode":"inner","tree":[{"t":"div"`) {
		t.Errorf("reply json = %s (%v)", raw, err)
	}
}

func TestViewsFlagSkipsTheCallerPatches(t *testing.T) {
	c, _, fc := setup(t, reviewer)
	res, f := call(t, c, "A:1", Request{Action: "open", Views: true})
	if f != nil {
		t.Fatal(f)
	}
	if len(viewsOf(t, res)) != 1 {
		t.Fatalf("views = %+v", res["views"])
	}
	if got := fc.all(); len(got) != 0 {
		t.Errorf("a views call still patched the page: %+v", got)
	}
}

func TestConfirmBarIsAView(t *testing.T) {
	c, h, _ := setup(t, stranger)
	res, f := call(t, c, "A:1", Request{Action: "send", Pane: stranger.Pane, Text: "hi", Views: true})
	if f != nil || res["view"] != "confirm" {
		t.Fatalf("send = %v %v", res, f)
	}
	if len(h.Prompts) != 0 {
		t.Fatal("delivered without confirm")
	}
	views := viewsOf(t, res)
	if v := find(views, "console"); v == nil || v.Mode != "inner" {
		t.Errorf("the thread was not returned first: %+v", views)
	}
	v := find(views, "confirm")
	if v == nil || v.Mode != "outer" || !hasID(v.Tree, render.ConfirmID) || !strings.Contains(treeText(v.Tree), "hi") {
		t.Fatalf("confirm view = %+v", v)
	}
	// Order: the thread comes before its confirm bar.
	if views[0].Target != "console" || views[len(views)-1].Target != "confirm" {
		t.Errorf("order = %+v", views)
	}
	// The confirmed send clears the bar and the error line.
	res, f = call(t, c, "A:2", Request{Action: "send", Pane: stranger.Pane, Text: "hi", Confirm: true, Views: true})
	if f != nil || res["sent"] != true {
		t.Fatalf("confirmed = %v %v", res, f)
	}
	views = viewsOf(t, res)
	if v := find(views, "confirm"); v == nil || !hasID(v.Tree, render.ConfirmID) || treeText(v.Tree) != "" {
		t.Errorf("the confirm bar was not emptied: %+v", views)
	}
	if v := find(views, "error"); v == nil || v.Mode != "outer" {
		t.Errorf("the error line was not cleared: %+v", views)
	}
}

func TestSelectCarriesTheChipAndTheThread(t *testing.T) {
	c, _, fc := setup(t, reviewer)
	res, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true})
	if f != nil {
		t.Fatal(f)
	}
	want := map[string]any{"pane": "w1:p1", "name": "Reviewer", "kind": "claude", "status": "idle", "crew": true, "folder": "/tmp"}
	if !reflect.DeepEqual(res["agent"], want) {
		t.Errorf("agent = %#v, want %#v", res["agent"], want)
	}
	if v := find(viewsOf(t, res), "console"); v == nil || !hasID(v.Tree, render.ThreadID) {
		t.Errorf("no thread view: %+v", res["views"])
	}
	if got := fc.all(); len(got) != 0 {
		t.Errorf("patched the page: %+v", got)
	}
	// Without the flag the signal still goes the Caller way.
	c2, _, fc2 := setup(t, reviewer)
	res, _ = call(t, c2, "A:1", Request{Action: "select", Pane: reviewer.Pane})
	if !strings.Contains(fc2.html(), `"pillAgent"`) || res["agent"] == nil {
		t.Errorf("old path: html %q agent %v", fc2.html(), res["agent"])
	}
}

func TestFollowFeedReturnsTheThreadWithoutPatching(t *testing.T) {
	c, _, fc := setup(t, reviewer)
	call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane})
	fc.reset()
	res, f := call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
	if f != nil {
		t.Fatal(f)
	}
	if res["following"] != reviewer.Pane || res["agent"] == nil {
		t.Errorf("reply = %v", res)
	}
	v := find(viewsOf(t, res), "console")
	if v == nil || v.Mode != "inner" || !hasID(v.Tree, render.ThreadID) {
		t.Fatalf("views = %+v", res["views"])
	}
	if got := fc.all(); len(got) != 0 {
		t.Errorf("a feed patched the page: %+v", got)
	}
	// The same state again sends nothing new for the console...
	res, _ = call(t, c, "A:3", Request{Action: "follow", Pane: reviewer.Pane, Feed: true})
	if find(viewsOf(t, res), "console") != nil {
		t.Error("an unchanged thread was sent again")
	}
	// ...until the page says it was hidden and may have missed it.
	res, _ = call(t, c, "A:4", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Resync: true})
	if find(viewsOf(t, res), "console") == nil {
		t.Error("a resynced page got no thread")
	}
	// A page that just loaded shows no console, and a feed opens none (the
	// Patcher path draws nothing into it either).
	res, _ = call(t, c, "A:5", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Fresh: true})
	if find(viewsOf(t, res), "console") != nil {
		t.Error("a feed opened a console on a page that just loaded")
	}
}

func TestFollowFeedKeepsAnotherViewAndShowsThePendingConfirm(t *testing.T) {
	c, _, _ := setup(t, stranger)
	call(t, c, "A:1", Request{Action: "send", Pane: stranger.Pane, Text: "hello there"})
	// A fresh page lost its confirm bar (Fresh clears the pending send) and
	// shows no console: the feed draws none.
	res, _ := call(t, c, "A:2", Request{Action: "follow", Pane: stranger.Pane, Feed: true, Fresh: true})
	if v := find(viewsOf(t, res), "console"); v != nil {
		t.Fatalf("a fresh page was given a console: %+v", res["views"])
	}
	// The picker the person opened stays: a feed does not draw over it.
	call(t, c, "A:3", Request{Action: "open"})
	res, _ = call(t, c, "A:4", Request{Action: "follow", Pane: stranger.Pane, Feed: true})
	if find(viewsOf(t, res), "console") != nil {
		t.Errorf("a feed replaced the picker: %+v", res["views"])
	}
}

func TestFollowFeedShowsAPendingConfirmBar(t *testing.T) {
	c, _, _ := setup(t, stranger)
	call(t, c, "A:1", Request{Action: "send", Pane: stranger.Pane, Text: "hello there"})
	// Not fresh: the same page, the bar is up; a redraw must keep it.
	res, _ := call(t, c, "A:2", Request{Action: "follow", Pane: stranger.Pane, Feed: true})
	v := find(viewsOf(t, res), "console")
	if v == nil || !strings.Contains(treeText(v.Tree), "hello there") {
		t.Fatalf("the pending confirm is not in the feed: %+v", res["views"])
	}
}

func TestAgentsReturnsThePanelAndFeedDoesNotStartALoop(t *testing.T) {
	c, _, fc := setup(t, reviewer, stranger)
	res, f := call(t, c, "A:1", Request{Action: "agents", Views: true})
	if f != nil {
		t.Fatal(f)
	}
	v := find(viewsOf(t, res), "agents")
	if v == nil || v.Mode != "inner" || !strings.Contains(treeText(v.Tree), "scratch") {
		t.Fatalf("views = %+v", res["views"])
	}
	if got := fc.all(); len(got) != 0 {
		t.Errorf("patched: %+v", got)
	}
	// A new session: the feed, with nothing patched and no loop.
	res, f = call(t, c, "B:1", Request{Action: "agents", Feed: true})
	if f != nil {
		t.Fatal(f)
	}
	if v := find(viewsOf(t, res), "agents"); v == nil || !strings.Contains(treeText(v.Tree), "scratch") {
		t.Fatalf("feed views = %+v", res["views"])
	}
	if got := fc.all(); len(got) != 0 {
		t.Errorf("a feed patched the page: %+v", got)
	}
	s := c.session("B:2")
	s.mu.Lock()
	loop := s.panel
	s.mu.Unlock()
	if loop != nil {
		t.Error("a feed started the live loop")
	}
	// Unchanged: nothing again.
	res, _ = call(t, c, "B:2", Request{Action: "agents", Feed: true})
	if find(viewsOf(t, res), "agents") != nil {
		t.Error("an unchanged list was sent again")
	}
}

// The chip rides every reply that names a session, flag or not: a page
// that reloads follows with {pane, fresh} and no flag, and the chip is what
// it needs (a page that takes signal patches simply ignores it).
func TestReplyAlwaysCarriesTheChip(t *testing.T) {
	c, _, _ := setup(t, reviewer)
	res, f := call(t, c, "A:1", Request{Action: "follow"})
	if f != nil {
		t.Fatal(f)
	}
	raw, _ := json.Marshal(res)
	if string(raw) != `{"agent":null,"following":"","ok":true}` {
		t.Errorf("reply = %s", raw)
	}
	// A reloaded page: no flag, no element patch, still the chip.
	res, _ = call(t, c, "A:3", Request{Action: "follow", Pane: reviewer.Pane, Fresh: true})
	if res["agent"] == nil || res["following"] != reviewer.Pane {
		t.Errorf("reply = %v", res)
	}
	if _, ok := res["views"]; ok {
		t.Errorf("a follow that draws nothing carried views: %v", res["views"])
	}
}

func TestCloseClearsTheChipInTheReply(t *testing.T) {
	c, h, _ := setup(t)
	pane := startSession(t, c, h, "A:1")
	call(t, c, "A:2", Request{Action: "select", Pane: pane, Views: true})
	res, f := call(t, c, "A:3", Request{Action: "close", Pane: pane, Views: true})
	if f != nil {
		t.Fatal(f)
	}
	if v, ok := res["agent"]; !ok || v != nil {
		t.Errorf("agent = %v (present %v), want null", v, ok)
	}
}

func TestFitReplyKeepsTheNewestWithinTheCap(t *testing.T) {
	mk := func(target string, n int) View {
		return View{Target: target, Mode: "inner", Tree: []render.Node{render.Element{T: "pre", K: []render.Node{strings.Repeat("x", n)}}}}
	}
	vs := []View{mk("console", 200<<10), mk("agents", 200<<10), mk("confirm", 200<<10), mk("error", 200<<10), mk("console", 200<<10)}
	got := fitReply(vs)
	b, _ := json.Marshal(got)
	if len(b) > maxReplyViewBytes || len(got) == 0 || got[len(got)-1].Target != "console" {
		t.Fatalf("%d views, %d bytes, last %v", len(got), len(b), got[len(got)-1].Target)
	}
	if one := []View{mk("console", 10)}; len(fitReply(one)) != 1 {
		t.Fatal("a single view was dropped")
	}
}

// Views built from large output (collector.add fits each) stay a reply core accepts.
func TestHandleReplyStaysUnderTheCoreCap(t *testing.T) {
	col := &collector{only: true}
	for i := 0; i < 6; i++ {
		col.add("console", "inner", "<div class=\"hc-a\"><pre>"+strings.Repeat("z", 900<<10)+"</pre></div>")
	}
	b, _ := json.Marshal(fitReply(col.list()))
	if len(b) > 1<<20 {
		t.Fatalf("reply views are %d bytes", len(b))
	}
}

func nodesOfViews(vs []View) int {
	n := 0
	for _, v := range vs {
		n += render.CountNodes(v.Tree)
	}
	return n
}

// pill-views.js refuses a reply over 16000 nodes in all: 40 views of one
// target come out as the last one.
func TestFitReplyKeepsTheLastViewPerTargetAndBoundsNodes(t *testing.T) {
	mk := func(target, mode, tag string, n int) View {
		ks := make([]render.Node, n)
		for i := range ks {
			ks[i] = render.Element{T: "i"}
		}
		return View{Target: target, Mode: mode, Tree: []render.Node{render.Element{T: "div", A: map[string]string{"id": tag}, K: ks}}}
	}
	var vs []View
	for i := 0; i < 40; i++ {
		vs = append(vs, mk("console", "inner", "c"+strconv.Itoa(i), 499)) // 500 nodes: 20000 in all
	}
	vs = append(vs, mk("agents", "inner", "a", 10), mk("confirm", "outer", "hc-confirm", 5))
	got := fitReply(vs)
	if n := nodesOfViews(got); n > 15000 {
		t.Fatalf("%d nodes", n)
	}
	count := map[string]int{}
	for _, v := range got {
		count[v.Target]++
	}
	if count["console"] != 1 || count["agents"] != 1 || count["confirm"] != 1 {
		t.Fatalf("views per target %v", count)
	}
	if got[0].Tree[0].(render.Element).A["id"] != "c39" {
		t.Errorf("not the last console view: %v", got[0].Tree[0].(render.Element).A["id"])
	}
}

// Distinct targets over the node budget lose the oldest first; an earlier
// outer view stays when a later inner one follows it.
func TestFitReplyDropsOldestOverTheNodeBudget(t *testing.T) {
	mk := func(target, mode string, n int) View {
		return View{Target: target, Mode: mode, Tree: []render.Node{render.Element{T: "div", K: make([]render.Node, n)}}}
	}
	fill := func(v View) View {
		el := v.Tree[0].(render.Element)
		for i := range el.K {
			el.K[i] = render.Element{T: "i"}
		}
		return v
	}
	vs := []View{fill(mk("confirm", "outer", 9000)), fill(mk("console", "inner", 9000)), fill(mk("agents", "inner", 2000))}
	got := fitReply(vs)
	if n := nodesOfViews(got); n > 15000 || len(got) != 2 || got[0].Target != "console" || got[1].Target != "agents" {
		t.Fatalf("%d nodes, %d views, first %v", nodesOfViews(got), len(got), got[0].Target)
	}
	// over budget, an earlier outer view survives a later inner one (the
	// inner one only replaces the content), a later outer supersedes an
	// earlier inner one, and a later inner supersedes an earlier inner
	pad := fill(mk("agents", "inner", 4000))
	outer, inner := fill(mk("console", "outer", 5000)), fill(mk("console", "inner", 5000))
	got = fitReply([]View{outer, inner, pad, fill(mk("console", "inner", 5000))})
	if len(got) != 3 || got[0].Mode != "outer" || got[1].Target != "agents" {
		t.Errorf("outer then inner: %d views", len(got))
	}
	got = fitReply([]View{inner, outer, pad, fill(mk("error", "outer", 5000))})
	if len(got) != 3 || got[0].Mode != "outer" || got[0].Target != "console" {
		t.Errorf("inner then outer: %d views", len(got))
	}
	got = fitReply([]View{pad, fill(mk("confirm", "outer", 5000)), fill(mk("confirm", "outer", 5000)), fill(mk("error", "outer", 5000))})
	if len(got) != 3 || got[0].Target != "agents" {
		t.Errorf("outer then outer: %d views", len(got))
	}
}
