package console

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"time"

	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/herdr"
	"herdr-console/render"
)

// The Agents panel (the sidebar's "agents" Patch Region) and the ribbon
// badge (the signal herdrBadge).
//
// "agents" renders the panel whole and starts a live loop for the browser
// session: while its Caller Ticket is valid and the panel was opened within
// one ticket's life (the shell calls "agents" again on its follow calls
// while the panel stays open), every change the herdr watcher reports
// re-renders the list, coalesced and at most once per PanelInterval. A
// patch no page heard (delivered: false) is sent again on a later round;
// the loop ends on ticket expiry or permission_denied, and the next
// "agents" or "follow" call starts it again.

// MaxPanelRows bounds the rows of one render, under the region's 64 KB
// Accept Rule.
const MaxPanelRows = 64

// DefaultPanelLife is how long the panel counts as open after an "agents"
// call when the ticket's expiry cannot be read.
const DefaultPanelLife = 5 * time.Minute

// panelLoop is one session's live panel loop.
type panelLoop struct {
	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
}

func (p *panelLoop) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// Badge is the ribbon badge for agents: "blocked" when any session waits on
// a dialog, else "done" when any finished, else "" (no badge, null).
func Badge(agents []herdr.Agent) string {
	done := false
	for _, a := range agents {
		switch a.Status {
		case "blocked":
			return "blocked"
		case "done":
			done = true
		}
	}
	if done {
		return "done"
	}
	return ""
}

func badgeSignal(b string) json.RawMessage {
	if b == "" {
		return json.RawMessage(`{"herdrBadge":null}`)
	}
	raw, _ := json.Marshal(map[string]string{"herdrBadge": b})
	return raw
}

// sendBadge patches herdrBadge when it changed since the last one this
// session's page heard (always when force). A send no page heard is not
// counted, so the next call sends it again.
func (c *Console) sendBadge(ctx context.Context, s *session, force bool) error {
	s.badgeMu.Lock()
	defer s.badgeMu.Unlock()
	agents, err := c.agents(ctx)
	if err != nil {
		return nil // nothing known: leave the badge as it is
	}
	b := Badge(agents)
	s.mu.Lock()
	same := s.badgeKnown && s.badge == b
	s.mu.Unlock()
	if same && !force {
		return nil
	}
	if err := s.caller.PatchSignals(ctx, badgeSignal(b), false); err != nil {
		return err
	}
	s.mu.Lock()
	s.badge, s.badgeKnown = b, true
	s.mu.Unlock()
	return nil
}

// spaces returns herdr's workspaces, cached: a list is asked again after
// spaceTTL, or at once when an agent names a workspace the cache lacks.
func (c *Console) spaces(ctx context.Context, agents []herdr.Agent) []herdr.Workspace {
	c.spaceMu.Lock()
	defer c.spaceMu.Unlock()
	stale := c.now().Sub(c.spaceAt) > spaceTTL
	if !stale {
		known := map[string]bool{}
		for _, w := range c.spaceList {
			known[w.ID] = true
		}
		for _, a := range agents {
			if a.Workspace != "" && !known[a.Workspace] {
				stale = true
				break
			}
		}
	}
	if stale {
		if got, err := c.herdr.Workspaces(ctx); err == nil {
			c.spaceList, c.spaceAt = got, c.now()
		}
	}
	return c.spaceList
}

const spaceTTL = 10 * time.Second

// panelList builds the panel's groups: one per herdr workspace, in herdr's
// order (sessions in a workspace herdr does not list come last), each in
// the order herdr reports its agents.
func (c *Console) panelList(ctx context.Context, s *session) render.AgentsList {
	agents, err := c.agents(ctx)
	if err != nil {
		return render.AgentsList{Error: herdrFault(err).Message}
	}
	spaces := c.spaces(ctx, agents)
	label := map[string]string{}
	order := map[string]int{}
	for i, w := range spaces {
		label[w.ID] = w.Label
		order[w.ID] = i
	}
	s.mu.Lock()
	selected := s.selected
	s.mu.Unlock()
	byWS := map[string][]render.AgentRow{}
	var ids []string
	shown := 0
	var l render.AgentsList
	for _, a := range agents {
		if shown >= MaxPanelRows {
			l.More++
			continue
		}
		shown++
		if _, ok := byWS[a.Workspace]; !ok {
			ids = append(ids, a.Workspace)
		}
		byWS[a.Workspace] = append(byWS[a.Workspace], render.AgentRow{
			Pane: a.Pane, Name: a.Label(), Folder: a.Cwd, Kind: a.Kind, Status: a.Status,
			Crew: c.IsCrew(a), Opened: c.opened.Has(a.Pane), Here: a.Pane == selected,
		})
	}
	sort.SliceStable(ids, func(i, j int) bool {
		oi, iok := order[ids[i]]
		oj, jok := order[ids[j]]
		if iok != jok {
			return iok
		}
		return oi < oj
	})
	for _, id := range ids {
		title := label[id]
		if title == "" {
			title = id
		}
		if title == "" {
			title = "Other"
		}
		l.Groups = append(l.Groups, render.AgentGroup{ID: id, Title: title, Rows: byWS[id]})
	}
	return l
}

// agentsAction renders the panel whole and keeps it live.
func (c *Console) agentsAction(ctx context.Context, s *session, filter string) (map[string]any, *Fault) {
	if len(filter) > 128 {
		return nil, &Fault{Code: invalid, Message: "the find text is over 128 characters"}
	}
	now := c.now()
	s.mu.Lock()
	s.panelAt = now
	s.panelLife = DefaultPanelLife
	if exp := s.caller.Expires(); exp.Unix() > 0 && exp.After(now) {
		s.panelLife = exp.Sub(now)
	}
	s.panelFilter = filter
	s.mu.Unlock()
	l, err := c.renderPanel(ctx, s, true)
	c.ensurePanelLoop(s)
	if err != nil {
		return nil, patchFault(err)
	}
	if l.Error != "" {
		return nil, &Fault{Code: unavailable, Message: l.Error}
	}
	return map[string]any{"ok": true, "view": "agents", "sessions": l.Sessions()}, nil
}

// panelWatched reports whether the panel counts as open: an "agents" call
// came within one ticket's life.
func (c *Console) panelWatched(s *session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.panelAt.IsZero() && c.now().Sub(s.panelAt) <= s.panelLife
}

// renderPanel patches the panel: whole (the region's root, inner), or only
// its list (#ha-list, outer) when that changed; then the badge. A whole
// render no page heard is done whole again by the next push.
func (c *Console) renderPanel(ctx context.Context, s *session, whole bool) (render.AgentsList, error) {
	s.panelMu.Lock()
	defer s.panelMu.Unlock()
	l := c.panelList(ctx, s)
	list := render.AgentsListHTML(l)
	s.mu.Lock()
	whole = whole || s.panelWhole
	filter, sent := s.panelFilter, s.panelSent
	s.mu.Unlock()
	var err error
	switch {
	case whole:
		err = s.caller.PatchElements(ctx, render.AgentsSelector, render.AgentsPanel(l, filter), publisher.WithRegion(render.AgentsRegion), publisher.WithMode("inner"))
	case list != sent:
		err = s.caller.PatchElements(ctx, "#"+render.AgentsListID, list, publisher.WithMode("outer"))
	}
	s.mu.Lock()
	switch {
	case err == nil:
		s.panelSent, s.panelWhole = list, false
	case whole && errors.Is(err, publisher.ErrNotDelivered):
		s.panelWhole = true
	}
	s.mu.Unlock()
	if err != nil {
		return l, err
	}
	return l, c.sendBadge(ctx, s, whole)
}

// pushPanel re-renders the panel's list now, when the panel is open (after
// a select or a close, so the highlight and the rows follow at once).
func (c *Console) pushPanel(ctx context.Context, s *session) {
	if !c.panelWatched(s) {
		return
	}
	if _, err := c.renderPanel(ctx, s, false); err != nil && !errors.Is(err, publisher.ErrNotDelivered) {
		log.Printf("agents panel: %v", err)
	}
	s.mu.Lock()
	p := s.panel
	s.mu.Unlock()
	if p != nil && p.ctx.Err() == nil {
		p.poke() // a list no page heard is sent again by the loop
	}
}

// resumePanel runs on every "follow": a page that just loaded (fresh) or
// was shown again (resync) may lack the panel's last patches, and an ended
// loop starts again while the panel is open.
func (c *Console) resumePanel(s *session, fresh, resync bool) {
	s.mu.Lock()
	if fresh || resync {
		s.panelSent = ""
		s.badgeKnown = false
	}
	if fresh {
		// The region is empty on a new page: the next push renders it whole.
		s.panelWhole = true
	}
	s.mu.Unlock()
	if c.panelWatched(s) {
		c.ensurePanelLoop(s)
	}
}

// ensurePanelLoop starts the session's live loop unless one runs, and
// wakes it.
func (c *Console) ensurePanelLoop(s *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.panel != nil && s.panel.ctx.Err() == nil {
		s.panel.poke()
		return
	}
	ctx, cancel := context.WithCancel(c.life)
	p := &panelLoop{ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
	s.panel = p
	// Subscribe before the loop runs: a change between the render that
	// started it and its first round is not lost.
	var sub <-chan struct{}
	unsub := func() {}
	if c.watch != nil {
		sub, unsub = c.watch.Subscribe()
	}
	// Its first round sends what the page may lack (nothing when the
	// render that started it was heard).
	p.poke()
	go c.panelRun(s, p, sub, unsub)
}

// ticketEnded reports whether the session's current ticket has expired.
func ticketEnded(s *session) bool {
	exp := s.caller.Expires()
	return !exp.IsZero() && exp.Unix() > 0 && time.Now().After(exp)
}

// panelRun re-renders the panel's list on watcher changes, coalesced, at
// most once per PanelInterval, until the ticket ends, the panel is no
// longer open, or core refuses the ticket.
func (c *Console) panelRun(s *session, p *panelLoop, sub <-chan struct{}, unsub func()) {
	defer p.cancel()
	defer unsub()
	tick := time.NewTicker(c.PanelInterval)
	defer tick.Stop()
	dirty := false
	var last time.Time
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-sub:
			dirty = true
		case <-p.wake:
			dirty = true
		case <-tick.C:
		}
		if ticketEnded(s) || !c.panelWatched(s) {
			return
		}
		if !dirty || time.Since(last) < c.PanelInterval {
			continue // the next tick sends what came meanwhile, once
		}
		last = time.Now()
		ctx, cancel := context.WithTimeout(p.ctx, 10*time.Second)
		_, err := c.renderPanel(ctx, s, false)
		cancel()
		var ce *publisher.CallerError
		switch {
		case err == nil:
			dirty = false
		case errors.Is(err, publisher.ErrNotDelivered):
			// No page listens (a hidden tab): send it again later.
		case errors.As(err, &ce) && ce.Code == denied:
			log.Printf("agents panel: %v", err)
			return
		case finalRefusal(err):
			log.Printf("agents panel: %v", err)
			dirty = false
		}
	}
}
