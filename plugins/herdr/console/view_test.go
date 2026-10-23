package console

import (
	"strings"
	"testing"

	"herdr-console/render"
)

func confirmPatches(fc *fakeCallers) int {
	n := 0
	for _, p := range fc.all() {
		if p.selector == "#"+render.ConfirmID {
			n++
		}
	}
	return n
}

func regionRenders(fc *fakeCallers, view string) int {
	n := 0
	for _, p := range fc.all() {
		if p.selector == render.RegionSelector && strings.Contains(p.html, `data-view="`+view+`"`) {
			n++
		}
	}
	return n
}

// The confirm bar lives only in the thread view: once another view
// replaced it, clearing it sends nothing (a patch to #hc-confirm would
// find no target in the browser).
func TestConfirmIsClearedOnlyWhereItIsShown(t *testing.T) {
	c, h, fc := setup(t, stranger)
	if res, f := call(t, c, "A:1", Request{Action: "send", Pane: stranger.Pane, Text: "hi"}); f != nil || res["view"] != "confirm" {
		t.Fatalf("send = %v %v", res, f)
	}
	if confirmPatches(fc) != 1 {
		t.Fatalf("confirm bar patches = %d, want 1", confirmPatches(fc))
	}
	// The picker replaces the thread view, and the bar with it.
	if _, f := call(t, c, "A:2", Request{Action: "open"}); f != nil {
		t.Fatal(f)
	}
	fc.reset()
	if res, f := call(t, c, "A:3", Request{Action: "dismiss"}); f != nil || res["view"] != "picker" {
		t.Fatalf("dismiss = %v %v", res, f)
	}
	if n := confirmPatches(fc); n != 0 {
		t.Fatalf("dismiss outside the thread patched #hc-confirm %d times", n)
	}

	// A confirmed send from the picker shows the thread whole (its bar slot
	// empty) and sends no separate clear.
	call(t, c, "A:4", Request{Action: "select", Pane: stranger.Pane})
	call(t, c, "A:5", Request{Action: "send", Pane: stranger.Pane, Text: "hi"})
	call(t, c, "A:6", Request{Action: "open"})
	fc.reset()
	res, f := call(t, c, "A:7", Request{Action: "send", Pane: stranger.Pane, Text: "hi", Confirm: true})
	if f != nil || res["sent"] != true || len(h.Prompts) != 1 {
		t.Fatalf("confirmed send = %v %v %+v", res, f, h.Prompts)
	}
	if regionRenders(fc, "thread") == 0 {
		t.Fatal("the thread view was not rendered for the send")
	}
	if n := confirmPatches(fc); n != 0 {
		t.Fatalf("a send from another view patched #hc-confirm %d times", n)
	}
}

// In the thread view the bar is cleared with one patch when a send goes.
func TestConfirmIsClearedInTheThread(t *testing.T) {
	c, _, fc := setup(t, stranger)
	call(t, c, "A:1", Request{Action: "select", Pane: stranger.Pane})
	call(t, c, "A:2", Request{Action: "send", Pane: stranger.Pane, Text: "hi"})
	fc.reset()
	if res, f := call(t, c, "A:3", Request{Action: "dismiss"}); f != nil || res["view"] != "thread" {
		t.Fatalf("dismiss = %v %v", res, f)
	}
	if n := confirmPatches(fc); n != 1 {
		t.Fatalf("dismiss in the thread patched #hc-confirm %d times, want 1", n)
	}
}

// A page that just loaded has an empty console: a "fresh" follow forgets
// the view, so the next send renders the thread before its confirm bar.
func TestFreshFollowForgetsTheView(t *testing.T) {
	c, _, fc := setup(t, stranger)
	call(t, c, "A:1", Request{Action: "select", Pane: stranger.Pane})
	if _, f := call(t, c, "A:2", Request{Action: "follow", Pane: stranger.Pane, Fresh: true}); f != nil {
		t.Fatal(f)
	}
	fc.reset()
	call(t, c, "A:3", Request{Action: "send", Pane: stranger.Pane, Text: "hi"})
	all := fc.all()
	thread, bar := -1, -1
	for i, p := range all {
		if thread < 0 && p.selector == render.RegionSelector && strings.Contains(p.html, `data-view="thread"`) {
			thread = i
		}
		if bar < 0 && p.selector == "#"+render.ConfirmID && strings.Contains(p.html, `class="hc-confirm"`) {
			bar = i
		}
	}
	if thread < 0 || bar < 0 || thread > bar {
		t.Fatalf("thread render at %d, confirm bar at %d: the bar needs the thread first", thread, bar)
	}
}

// A close from the Agents sidebar leaves the pill's console alone: the
// thread of another session stays; only the picker, or the closed
// session's own thread, is re-rendered as the picker.
func TestCloseRerendersOnlyThePickerOrItsOwnThread(t *testing.T) {
	c, h, fc := setup(t, stranger)
	a := startSession(t, c, h, "A:1")
	b := startSession(t, c, h, "A:2")
	if _, f := call(t, c, "A:3", Request{Action: "select", Pane: stranger.Pane}); f != nil {
		t.Fatal(f)
	}
	fc.reset()
	res, f := call(t, c, "A:4", Request{Action: "close", Pane: a})
	if f != nil || res["closed"] != a {
		t.Fatalf("close = %v %v", res, f)
	}
	if n := regionRenders(fc, "picker"); n != 0 {
		t.Fatalf("a close beside another thread rendered the picker %d times", n)
	}
	if c.Opened().Has(a) {
		t.Fatal("the pane is still opened")
	}
	// From the picker, the picker follows the close.
	call(t, c, "A:5", Request{Action: "open"})
	fc.reset()
	if res, f := call(t, c, "A:6", Request{Action: "close", Pane: b}); f != nil || res["view"] != "picker" {
		t.Fatalf("close from the picker = %v %v", res, f)
	}
	if n := regionRenders(fc, "picker"); n != 1 {
		t.Fatalf("picker renders = %d, want 1", n)
	}
}
