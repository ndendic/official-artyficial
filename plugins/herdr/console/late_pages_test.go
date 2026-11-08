package console

import (
	"strings"
	"testing"
	"time"
)

// A session started from the pill of a vouched Page (open Stream) and then
// ended: the end must reach the page's next reply. The start's pick stamps
// the follower as a views page, not as a Patcher page.
func TestStartedThenEndedOnAnOpenVouchedStream(t *testing.T) {
	c, h := setupOpenStream(t)
	c.StartWait = 3 * time.Second
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	if f != nil {
		t.Fatal(f)
	}
	ag, _ := res["agent"].(map[string]any)
	pane, _ := ag["pane"].(string)
	if pane == "" {
		t.Fatalf("start gave no chip: %v", res["agent"])
	}
	// the vouched page's feed, as pill.js runs it every 2 s
	for i := 0; i < 2; i++ {
		call(t, c, "A:2", Request{Action: "follow", Pane: pane, Feed: true, Views: true})
	}
	h.Remove(pane)
	s := sess(c, "A")
	waitFor(t, "the end in the outbox", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.hasLateAgent
	})
	res, _ = call(t, c, "A:3", Request{Action: "open", Views: true})
	if v, ok := res["agent"]; !ok || v != nil {
		t.Errorf("the end of a session started on this vouched Page never reached it: reply agent=%v (present %v)", v, ok)
	}
	if !strings.Contains(viewsText(res), "ended") {
		t.Errorf("no 'ended' line in the reply: %+v", res["views"])
	}
}

func viewsText(res map[string]any) string {
	var b strings.Builder
	v, _ := res["views"].([]View)
	for _, x := range v {
		b.WriteString(treeText(x.Tree))
	}
	return b.String()
}

// An unvouched tab (no views) and a vouched tab (views) share one browser
// session: whichever picked the pane first, the unvouched tab's late draws
// still go through the Patcher, and the vouched tab still finds them in its
// next reply.
func TestMixedTabsKeepThePatcherInBothOrders(t *testing.T) {
	for _, tc := range []struct {
		name  string
		first Request // the first tab's pick
		next  Request // the other tab's call
	}{
		{"unvouched picks, vouched feeds",
			Request{Action: "select", Pane: reviewer.Pane},
			Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true}},
		{"vouched picks, unvouched picks",
			Request{Action: "select", Pane: reviewer.Pane, Views: true},
			Request{Action: "select", Pane: reviewer.Pane}},
		{"vouched picks, unvouched follows",
			Request{Action: "select", Pane: reviewer.Pane, Views: true},
			Request{Action: "follow", Pane: reviewer.Pane}},
		{"unvouched picks, vouched picks",
			Request{Action: "select", Pane: reviewer.Pane},
			Request{Action: "select", Pane: reviewer.Pane, Views: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, h, fc := setup(t, reviewer)
			if _, f := call(t, c, "A:1", tc.first); f != nil {
				t.Fatal(f)
			}
			if _, f := call(t, c, "A:2", tc.next); f != nil {
				t.Fatal(f)
			}
			fc.reset()
			h.Remove(reviewer.Pane)
			waitFor(t, "the end reaching the Patcher", func() bool {
				return strings.Contains(fc.html(), "ended") && strings.Contains(fc.html(), `"pillAgent":null`)
			})
			s := sess(c, "A")
			s.mu.Lock()
			late := s.hasLateAgent
			s.mu.Unlock()
			if !late {
				t.Errorf("the vouched tab's copy of the end is not in the outbox")
			}
		})
	}
}

// A start from an unvouched tab, picked later by a vouched one, still
// patches. And a call without views is never the one that flips a session
// to outbox only: a views-only follower stays views-only.
func TestUnvouchedAloneNeverWaitsInTheOutbox(t *testing.T) {
	c, h, fc := setup(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane}); f != nil {
		t.Fatal(f)
	}
	fc.reset()
	h.Remove(reviewer.Pane)
	waitFor(t, "the end reaching the Patcher", func() bool { return strings.Contains(fc.html(), "ended") })
	s := sess(c, "A")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasLateAgent || len(s.lateViews) != 0 {
		t.Errorf("the outbox holds what the Patcher took")
	}
}

// The same without any feed call in between: the start alone proves its
// page draws views, so the end goes to the outbox and not to the Patcher
// (which a vouched Stream would drop).
func TestStartedThenEndedWithoutAFeed(t *testing.T) {
	c, h := setupOpenStream(t)
	c.StartWait = 3 * time.Second
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Views: true})
	if f != nil {
		t.Fatal(f)
	}
	ag, _ := res["agent"].(map[string]any)
	pane, _ := ag["pane"].(string)
	if pane == "" {
		t.Fatalf("start gave no chip: %v", res["agent"])
	}
	h.Remove(pane)
	s := sess(c, "A")
	waitFor(t, "the end in the outbox", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.hasLateAgent
	})
	if f := c.follower(s); f != nil && f.pages.Load() != pageViews {
		t.Errorf("a start from a views page stamped the follower %d", f.pages.Load())
	}
}
