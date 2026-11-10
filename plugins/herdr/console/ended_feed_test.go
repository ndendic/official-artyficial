package console

import (
	"testing"
	"time"
)

// The vouched Page's own next call after an end is its 2 s feed for the pane
// it still shows (pill.js feedStep: {action:follow, pane: current.pane, feed:true}).
func TestEndedReachesTheVouchedFeed(t *testing.T) {
	c, h := setupOpenStream(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
	h.Remove(reviewer.Pane)
	s := sess(c, "A")
	waitFor(t, "end in outbox", func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.hasLateAgent })
	for i := 0; i < 3; i++ {
		res, f := call(t, c, "A:3", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
		t.Logf("feed %d: res=%v fault=%v", i, res, f)
		if res != nil {
			if v, ok := res["agent"]; ok && v == nil {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.mu.Lock()
	t.Errorf("three feeds after the end: none carried agent:null; outbox still holds agent=%v views=%d", s.hasLateAgent, len(s.lateViews))
	s.mu.Unlock()
}

// Same for a session started from the pill (C1's case), followed by the feed.
func TestStartedThenEndedThroughTheFeed(t *testing.T) {
	c, h := setupOpenStream(t)
	c.StartWait = 3 * time.Second
	res, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Name: "mine", Views: true})
	if f != nil {
		t.Fatal(f)
	}
	ag, _ := res["agent"].(map[string]any)
	pane, _ := ag["pane"].(string)
	t.Logf("start name=%v agent=%v", res["name"], ag)
	h.Remove(pane)
	s := sess(c, "A")
	waitFor(t, "end in outbox", func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.hasLateAgent })
	res, f = call(t, c, "A:2", Request{Action: "follow", Pane: pane, Feed: true, Views: true})
	if res == nil || res["agent"] != nil {
		t.Errorf("feed after end: res=%v fault=%+v", res, f)
	}
}

// The end is delivered by the feed alone (no 'open' drains it), carried on
// the reply of the first feed for the gone pane, exactly once, and the
// feeds after it find nothing to repeat and no fault.
func TestEndedIsDeliveredOnceByTheFeedAlone(t *testing.T) {
	c, h := setupOpenStream(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	h.Remove(reviewer.Pane)
	s := sess(c, "A")
	waitFor(t, "end in outbox", func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.hasLateAgent && len(s.lateViews) > 0 })
	res, f := call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
	if f != nil || res == nil {
		t.Fatalf("feed for a gone pane: res=%v fault=%+v", res, f)
	}
	if v, ok := res["agent"]; !ok || v != nil {
		t.Errorf("agent = %v (present %v), want null", v, ok)
	}
	if res["following"] != "" {
		t.Errorf("following = %v, want empty", res["following"])
	}
	views, _ := res["views"].([]View)
	if len(views) == 0 {
		t.Errorf("the end did not ride on the feed reply: %v", res)
	}
	s.mu.Lock()
	left := s.hasLateAgent || len(s.lateViews) != 0
	s.mu.Unlock()
	if left {
		t.Error("the outbox was not drained by the reply that carried it")
	}
	res, f = call(t, c, "A:3", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
	if f != nil || res == nil {
		t.Fatalf("second feed: res=%v fault=%+v", res, f)
	}
	if v, _ := res["views"].([]View); len(v) != 0 {
		t.Errorf("the end was delivered twice: %d views on the second feed", len(v))
	}
}
