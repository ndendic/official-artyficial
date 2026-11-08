package console

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/render"
)

// View is one piece of the reply's "views" list: what a vouched Page draws
// itself, because core refuses it Caller patches. target is one of four
// fixed names, mode inner (the target's content) or outer (the target
// itself, which keeps its id), tree the HTML as {t,a,k} nodes (render.Tree).
type View struct {
	Target string        `json:"target"`
	Mode   string        `json:"mode"`
	Tree   []render.Node `json:"tree"`
}

// The four targets.
const (
	targetConsole = "console"
	targetAgents  = "agents"
	targetConfirm = "confirm"
	targetError   = "error"
)

// viewTarget maps the selector of a patch to the view that carries it. A
// selector the shell has no fixed target for (an element inside the thread,
// a block an agent wrote) is not a view.
func viewTarget(selector string) (target, mode string, ok bool) {
	switch selector {
	case render.RegionSelector:
		return targetConsole, "inner", true
	case render.AgentsSelector:
		return targetAgents, "inner", true
	case "#" + render.AgentsListID:
		return targetAgents, "outer", true
	case "#" + render.ConfirmID:
		return targetConfirm, "outer", true
	case "#" + render.ErrorID:
		return targetError, "outer", true
	}
	return "", "", false
}

// collector gathers the views of one Handle call. Only patches made with
// that call's context are its own: a follower's or the panel loop's later
// patches carry their own contexts.
type collector struct {
	mu    sync.Mutex
	views []View
	// only: nothing reaches the Patcher during this call (a feed).
	only bool
}

type collectorKey struct{}

func collectorOf(ctx context.Context) *collector {
	c, _ := ctx.Value(collectorKey{}).(*collector)
	return c
}

func (c *collector) add(target, mode, html string) {
	tree, err := render.Tree(html)
	if err != nil {
		return
	}
	c.mu.Lock()
	c.views = append(c.views, View{Target: target, Mode: mode, Tree: tree})
	c.mu.Unlock()
}

func (c *collector) list() []View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]View(nil), c.views...)
}

// recorder wraps a session's Patcher: it copies each elements patch of a
// Handle call into that call's views, then passes it on (the Patcher stays
// the first path). When the page draws views itself (viewsOnly, or a
// collect-only call) the Patcher is not used: core would refuse it.
type recorder struct {
	Patcher
	viewsOnly *atomic.Bool
}

func (r *recorder) skip(ctx context.Context) bool {
	if r.viewsOnly.Load() {
		return true
	}
	c := collectorOf(ctx)
	return c != nil && c.only
}

func (r *recorder) PatchElements(ctx context.Context, selector, html string, opts ...publisher.PatchOption) error {
	if c := collectorOf(ctx); c != nil {
		if target, mode, ok := viewTarget(selector); ok {
			c.add(target, mode, html)
		}
	}
	if r.skip(ctx) {
		return nil
	}
	return r.Patcher.PatchElements(ctx, selector, html, opts...)
}

func (r *recorder) PatchSignals(ctx context.Context, signals json.RawMessage, onlyIfMissing bool) error {
	if r.skip(ctx) {
		return nil
	}
	return r.Patcher.PatchSignals(ctx, signals, onlyIfMissing)
}
