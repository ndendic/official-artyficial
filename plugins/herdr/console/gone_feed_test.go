package console

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"herdr-console/herdr"
)

// hookClient lets a test act inside the console's own herdr.Get.
type hookClient struct {
	herdr.Client
	onGet func(pane string) error
}

func (h *hookClient) Get(ctx context.Context, pane string) (herdr.Agent, error) {
	if h.onGet != nil {
		if err := h.onGet(pane); err != nil {
			return herdr.Agent{}, err
		}
	}
	return h.Client.Get(ctx, pane)
}

func setupHooked(t *testing.T, agents ...herdr.Agent) (*Console, *herdr.Fake, *hookClient) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	fake := herdr.NewFake(agents...)
	hc := &hookClient{Client: fake}
	c := New(ctx, Config{Crew: crew, ConsoleWorkspace: "Console", DataDir: t.TempDir(), ClaudeProjects: t.TempDir()}, hc, nil,
		func(ticket string) Patcher { return openStream{(&fakeCallers{}).new(ticket)} })
	c.Tick = 20 * time.Millisecond
	t.Cleanup(func() { c.Close(); cancel() })
	return c, fake, hc
}

// A herdr failure that is not agent_not_found is not the end of the session.
func TestFeedKeepsTheSessionOnATransientHerdrError(t *testing.T) {
	c, _, hc := setupHooked(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	hc.onGet = func(string) error { return &herdr.Error{Code: "timeout", Message: "i/o timeout"} }
	res, f := call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
	hc.onGet = nil
	if f == nil {
		t.Fatalf("a transient error answered ok: %v", res)
	}
	if v, ok := res["agent"]; ok && v == nil {
		t.Errorf("agent:null on a transient error")
	}
	s := sess(c, "A")
	s.mu.Lock()
	sel := s.selected
	s.mu.Unlock()
	if sel != reviewer.Pane {
		t.Errorf("selected = %q, the pane is alive", sel)
	}
}

// The feed that sees the pane gone before the follower does carries the end.
func TestFeedCarriesTheEndWhenItSeesTheEndFirst(t *testing.T) {
	c, h := setupOpenStream(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
	h.Remove(reviewer.Pane)
	res, f := call(t, c, "A:3", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
	if f != nil || res == nil {
		t.Fatalf("res=%v fault=%v", res, f)
	}
	if v, ok := res["agent"]; !ok || v != nil {
		t.Errorf("agent = %v (present %v), want null", v, ok)
	}
	views, _ := res["views"].([]View)
	hasErr := false
	for _, v := range views {
		if v.Target == targetError {
			hasErr = true
		}
	}
	if !hasErr {
		t.Fatalf("the reply has no end notice: %v", res)
	}
	// The follower must not add the same end again.
	time.Sleep(1500 * time.Millisecond)
	s := sess(c, "A")
	s.mu.Lock()
	left := len(s.lateViews) != 0 || s.hasLateAgent
	s.mu.Unlock()
	if left {
		t.Error("the end was put in the outbox a second time")
	}
}

// A chip set by a start that came up while the feed ran is not cleared.
func TestGoneFeedKeepsANewerChip(t *testing.T) {
	c, h, hc := setupHooked(t, reviewer)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: reviewer.Pane, Views: true}); f != nil {
		t.Fatal(f)
	}
	h.Remove(reviewer.Pane)
	s := sess(c, "A")
	hc.onGet = func(string) error {
		s.addLateSignals(json.RawMessage(`{"pillAgent":{"pane":"B:9"}}`))
		return nil
	}
	// the hook adds the chip, then the real Get answers agent_not_found
	res, f := call(t, c, "A:2", Request{Action: "follow", Pane: reviewer.Pane, Feed: true, Views: true})
	hc.onGet = nil
	if f != nil || res == nil {
		t.Fatalf("res=%v fault=%v", res, f)
	}
	m, _ := res["agent"].(map[string]any)
	if m == nil || m["pane"] != "B:9" {
		t.Errorf("agent = %v, want the newer chip", res["agent"])
	}
}
