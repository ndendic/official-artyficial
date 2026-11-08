package console

import (
	"context"
	"encoding/json"
	"sync"

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
// a block an agent wrote, the list #ha-list: the shell draws no outer view
// of it, so a list change goes out as the whole panel, see renderPanel) is
// not a view.
func viewTarget(selector string) (target, mode string, ok bool) {
	switch selector {
	case render.RegionSelector:
		return targetConsole, "inner", true
	case render.AgentsSelector:
		return targetAgents, "inner", true
	case "#" + render.ConfirmID:
		return targetConfirm, "outer", true
	case "#" + render.ErrorID:
		return targetError, "outer", true
	}
	return "", "", false
}

// collector gathers the views of one Handle call. Only patches made with
// that call's context are its own: a follower's or the panel loop's later
// patches carry their own contexts and never see it.
//
// Which page draws views is the call's business, not the session's: the
// browser session is shared by every tab, and a vouched Page and an
// unvouched one can both belong to it. So "only" (no Patcher) holds for the
// calls that asked for it, and for nothing else: the follower and the
// panel loop always patch, and a patch no page took is tried again by the
// trackers they already have.
type collector struct {
	mu    sync.Mutex
	views []View
	// only: nothing reaches the Patcher during this call (Request.Views or
	// Request.Feed).
	only bool
	// late is set on a collector that belongs to no call (a draw that
	// runs after the reply, or never had one): the Patcher is tried, and a
	// view or chip it could not take waits in the session's outbox for the
	// next call's reply.
	late *session
	// lateOnly: the page this draw belongs to draws views itself (its
	// opening call asked for views, or its session's page has), so the draw
	// goes to the outbox and never to the Patcher. On a vouched Page the
	// Stream is open, core accepts the Caller patch and the sink then drops
	// it: only the Patcher's error would have filled the outbox, and it
	// never comes.
	lateOnly bool
	// pages: the kinds of page (pageViews, pagePatches) the draw belongs to.
	// lateOnly is pageViews alone; both bits, or none known, send the draw
	// to the Patcher and to the outbox (lateBoth): an unvouched tab takes
	// the patch, a vouched one the reply, and neither is drawn twice
	// by the same page.
	pages    int32
	lateBoth bool
}

type collectorKey struct{}

func collectorOf(ctx context.Context) *collector {
	c, _ := ctx.Value(collectorKey{}).(*collector)
	return c
}

// viewsOnly reports whether the call that made ctx draws views itself.
func viewsOnly(ctx context.Context) bool {
	c := collectorOf(ctx)
	return c != nil && c.only
}

// lateContext is ctx for a draw that outlives its call: its patches that
// no page takes are kept for the session's next reply.
// pages says which kinds of page it belongs to (pagesOf, follower.pages):
// views alone goes to the outbox only; patches alone keeps the Patcher first
// (the outbox only on its error); both, or neither known, uses both.
func lateContext(ctx context.Context, s *session, pages int32) context.Context {
	return context.WithValue(ctx, collectorKey{}, &collector{late: s, pages: pages,
		lateOnly: pages == pageViews, lateBoth: pages == 0 || pages == pageViews|pagePatches})
}

func (c *collector) add(target, mode, html string) {
	tree, err := render.Tree(html)
	if err != nil {
		return
	}
	c.append(View{Target: target, Mode: mode, Tree: render.FitTree(tree, render.MaxViewNodes)})
}

func (c *collector) append(v View) {
	c.mu.Lock()
	c.views = append(c.views, v)
	c.mu.Unlock()
}

func (c *collector) list() []View {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]View(nil), c.views...)
}

// maxLate bounds the views one session keeps for its next reply.
const maxLate = 8

// addLate keeps a view for the session's next reply.
func (s *session) addLate(v View) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lateViews) >= maxLate {
		s.lateViews = s.lateViews[1:]
	}
	s.lateViews = append(s.lateViews, v)
}

// addLateSignals keeps the chip (pillAgent) of a signal patch no page took.
func (s *session) addLateSignals(raw json.RawMessage) {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	v, ok := m["pillAgent"]
	if !ok {
		return
	}
	var agent any
	if json.Unmarshal(v, &agent) != nil {
		return
	}
	s.mu.Lock()
	s.lateAgent, s.hasLateAgent = agent, true
	s.mu.Unlock()
}

// takeLate hands over what waited for a reply.
func (s *session) takeLate() (views []View, agent any, hasAgent bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	views, agent, hasAgent = s.lateViews, s.lateAgent, s.hasLateAgent
	s.lateViews, s.lateAgent, s.hasLateAgent = nil, nil, false
	return
}

// restoreLate puts back what takeLate took when no reply could carry it
// (the call failed), ahead of anything kept meanwhile.
func (s *session) restoreLate(views []View, agent any, hasAgent bool) {
	if len(views) == 0 && !hasAgent {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lateViews = append(views, s.lateViews...)
	if len(s.lateViews) > maxLate {
		s.lateViews = s.lateViews[len(s.lateViews)-maxLate:]
	}
	if hasAgent && !s.hasLateAgent {
		s.lateAgent, s.hasLateAgent = agent, true
	}
}

// recorder wraps a session's Patcher: it copies each elements patch of a
// Handle call into that call's views, then passes it on (the Patcher stays
// the first path). When the call draws views itself (a views or feed call)
// the Patcher is not used: core would refuse it.
type recorder struct {
	Patcher
	s *session
}

func (r *recorder) PatchElements(ctx context.Context, selector, html string, opts ...publisher.PatchOption) error {
	c := collectorOf(ctx)
	if c == nil {
		return r.Patcher.PatchElements(ctx, selector, html, opts...)
	}
	target, mode, isView := viewTarget(selector)
	if c.late != nil {
		if c.lateOnly {
			if isView {
				if tree, terr := render.Tree(html); terr == nil {
					c.late.addLate(View{Target: target, Mode: mode, Tree: render.FitTree(tree, render.MaxViewNodes)})
				}
			}
			return nil
		}
		err := r.Patcher.PatchElements(ctx, selector, html, opts...)
		if isView && (err != nil || c.lateBoth) {
			if tree, terr := render.Tree(html); terr == nil {
				c.late.addLate(View{Target: target, Mode: mode, Tree: render.FitTree(tree, render.MaxViewNodes)})
				return nil
			}
		}
		return err
	}
	if isView {
		c.add(target, mode, html)
	}
	if c.only {
		return nil
	}
	return r.Patcher.PatchElements(ctx, selector, html, opts...)
}

func (r *recorder) PatchSignals(ctx context.Context, signals json.RawMessage, onlyIfMissing bool) error {
	c := collectorOf(ctx)
	if c == nil {
		return r.Patcher.PatchSignals(ctx, signals, onlyIfMissing)
	}
	if c.late != nil {
		if c.lateOnly {
			c.late.addLateSignals(signals)
			return nil
		}
		err := r.Patcher.PatchSignals(ctx, signals, onlyIfMissing)
		if err != nil || c.lateBoth {
			c.late.addLateSignals(signals)
			return nil
		}
		return nil
	}
	if c.only {
		return nil
	}
	return r.Patcher.PatchSignals(ctx, signals, onlyIfMissing)
}
