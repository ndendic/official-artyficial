// Package console is the herdr console's state machine: one state per
// browser session (keyed by the Caller Ticket's session), the actions the
// pill posts, and the safety rules.
//
// Safety, enforced here and unit-tested:
//   - Reading every session is fine.
//   - Input (send, keys) to a crew session or to a session the console did
//     not open needs the two-step confirm: the first call renders a red
//     confirm bar and sends nothing; only a confirm:true call that matches
//     the pending confirm of the same browser session delivers.
//   - Keys go only to a session waiting on a dialog (status blocked), and
//     only when a person chose them. The console never answers a dialog.
//   - The console closes and teaches only sessions it opened.
//   - Sessions it opens go in the herdr workspace console_workspace,
//     created without focus.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/herdr"
	"herdr-console/render"
)

// Patcher is the part of *publisher.Caller the console uses; tests fake it.
type Patcher interface {
	PatchElements(ctx context.Context, selector, html string, opts ...publisher.PatchOption) error
	PatchSignals(ctx context.Context, signals json.RawMessage, onlyIfMissing bool) error
	Renew(ticket string)
	SessionID() string
	Expires() time.Time
}

// Config is what the console needs from the Plugin's configuration.
type Config struct {
	Crew             []string
	ConsoleWorkspace string
	// ClaudeProjects is ~/.claude/projects.
	ClaudeProjects string
	// CodexSessions is ~/.codex/sessions (Codex's rollout files).
	CodexSessions string
	// PiSessions is ~/.pi/agent/sessions (Pi's session files).
	PiSessions string
	// DataDir keeps the registry of opened sessions.
	DataDir string
}

// Request is the capability's input.
type Request struct {
	Action    string   `json:"action"`
	Pane      string   `json:"pane,omitempty"`
	Text      string   `json:"text,omitempty"`
	Confirm   bool     `json:"confirm,omitempty"`
	Keys      []string `json:"keys,omitempty"`
	Kind      string   `json:"kind,omitempty"`
	OtherKind string   `json:"other_kind,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	Name      string   `json:"name,omitempty"`
	Filter    string   `json:"filter,omitempty"`
	// Fresh marks a follow from a page that just loaded: its console is
	// empty (Caller patches are not durable), whatever was shown before.
	Fresh bool `json:"fresh,omitempty"`
	// Resync marks a follow from a page that was hidden and is shown
	// again: its Stream was closed meanwhile, so patches may be missing.
	// The thread is rendered whole and pillAgent sent again.
	Resync bool `json:"resync,omitempty"`
}

// Fault is a refusal, in the nine codes.
type Fault struct {
	Code    string
	Message string
}

func (f *Fault) Error() string { return f.Code + ": " + f.Message }

const (
	invalid     = "invalid_argument"
	denied      = "permission_denied"
	notFound    = "not_found"
	unavailable = "unavailable"
)

// MaxText is the longest message the console sends.
const MaxText = 16 << 10

// Primer is what "teach" sends, once, to a session the console opened.
const Primer = "You are talking to a person through the herdr console in an RT Station Workspace. " +
	"To change the page they are looking at, put a patch block in your reply; the console applies it once the closing fence arrives and shows a chip for it.\n\n" +
	"Elements: a fenced block with info string `patch-elements selector=\"#aside\" mode=\"append\"` and HTML inside. " +
	"mode is outer (default), inner, append, prepend, before, after, replace or remove. selector may be left out when the HTML is one element with an id (outer). " +
	"Optional region=\"name\" targets a Patch Region (its root, with inner, append or prepend).\n\n" +
	"Signals: a fenced block with info string `patch-signals` and one JSON object inside, e.g. {\"theme\":\"dark\"}.\n\n" +
	"Markup only, nothing that runs: no <script>, no on* attributes, no Datastar or data-pill-* attributes (plain data-variant or data-state are fine), " +
	"no <iframe>, <form>, <style> or javascript: links, only known HTML or SVG elements, at most 64 KB per block. " +
	"Leave the console itself (#pill-console, hc-* ids, pill* and hc* signals) alone. Reply with the word ok."

// Console serves the actions.
type Console struct {
	cfg       Config
	herdr     herdr.Client
	watch     *herdr.Watcher
	newCaller func(ticket string) Patcher
	opened    *Registry
	life      context.Context
	now       func() time.Time
	// Tick is the follower's period (default 250 ms: at most 4 answer
	// sends a second, under the 8 a second the stream rule allows).
	Tick time.Duration
	// PanelInterval is the Agents panel's shortest time between two live
	// re-renders (default 500 ms: at most 2 a second).
	PanelInterval time.Duration

	// spaceMu guards the cache of herdr's workspaces (the panel's groups).
	spaceMu   sync.Mutex
	spaceList []herdr.Workspace
	spaceAt   time.Time

	mu       sync.Mutex
	sessions map[string]*session
}

// New makes a Console. life bounds every follower goroutine.
func New(life context.Context, cfg Config, h herdr.Client, w *herdr.Watcher, newCaller func(string) Patcher) *Console {
	if cfg.ConsoleWorkspace == "" {
		cfg.ConsoleWorkspace = "Console"
	}
	path := ""
	if cfg.DataDir != "" {
		path = filepath.Join(cfg.DataDir, "opened.json")
	}
	return &Console{cfg: cfg, herdr: h, watch: w, newCaller: newCaller, opened: LoadRegistry(path), life: life, now: time.Now, Tick: 250 * time.Millisecond, PanelInterval: 500 * time.Millisecond, sessions: map[string]*session{}}
}

// Opened exposes the registry (tests and status).
func (c *Console) Opened() *Registry { return c.opened }

type pending struct {
	pane string
	text string
	keys []string
}

type session struct {
	id     string
	caller Patcher
	// followMu serializes ensureFollower for this session.
	followMu sync.Mutex

	mu       sync.Mutex
	selected string
	view     string // "", picker, form, thread, starting
	pending  *pending
	// confirm is true while the confirm bar (#hc-confirm, part of the
	// thread view only) shows a pending send or key press.
	confirm bool
	follow  *follower
	// memory is what the console knew of each pane it followed before
	// (paneMemory), so picking a pane again neither re-applies nor drops
	// its blocks.
	memory map[string]*paneMemory

	// The Agents panel (panel.go). panelMu serializes its renders, so the
	// last patch is the newest state; badgeMu the herdrBadge sends.
	panelMu sync.Mutex
	badgeMu sync.Mutex
	// panelAt is the last "agents" call, panelLife one ticket's life then:
	// the panel counts as open until panelAt+panelLife.
	panelAt     time.Time
	panelLife   time.Duration
	panelFilter string
	// panelSent is the list the page last heard; panelWhole asks the next
	// render to fill the region whole (a page that has no panel yet).
	panelSent  string
	panelWhole bool
	panel      *panelLoop
	// badge is the herdrBadge the page last heard, when badgeKnown.
	badge      string
	badgeKnown bool
}

// Handle runs one action for the browser session that ticket names. With
// no ticket nothing can be rendered, so it answers a plain result.
func (c *Console) Handle(ctx context.Context, ticket string, req Request) (map[string]any, *Fault) {
	if ticket == "" {
		return map[string]any{"ok": false, "message": "artyficial.herdr.console renders into the browser session that calls it: call it from the pill of a signed-in page"}, nil
	}
	s := c.session(ticket)
	switch req.Action {
	case "open":
		return c.open(ctx, s, req.Filter)
	case "select":
		return c.selectPane(ctx, s, req.Pane)
	case "send":
		return c.send(ctx, s, req)
	case "keys":
		return c.keys(ctx, s, req)
	case "new":
		return c.newForm(ctx, s, render.FormValues{})
	case "start":
		return c.start(ctx, s, req)
	case "close":
		return c.closePane(ctx, s, req.Pane)
	case "teach":
		return c.teach(ctx, s, req.Pane)
	case "follow":
		return c.followAction(ctx, s, req.Pane, req.Fresh, req.Resync)
	case "dismiss":
		return c.dismiss(ctx, s)
	case "agents":
		return c.agentsAction(ctx, s, req.Filter)
	}
	return nil, c.fail(ctx, s, invalid, "unknown action "+fmt.Sprintf("%q", req.Action)+": use open, select, send, keys, new, start, close, teach, follow, dismiss or agents")
}

func (c *Console) session(ticket string) *session {
	probe := c.newCaller(ticket)
	id := probe.SessionID()
	if id == "" {
		id = "ticket:" + ticket // unreadable ticket: state lives for this call only
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.sessions[id]
	if s == nil {
		s = &session{id: id, caller: probe}
		c.sessions[id] = s
	} else {
		s.caller.Renew(ticket)
	}
	return s
}

// fail shows msg in the console's error line and returns the Fault.
func (c *Console) fail(ctx context.Context, s *session, code, msg string) *Fault {
	s.mu.Lock()
	view := s.view
	s.mu.Unlock()
	var err error
	if view == "" {
		err = c.region(ctx, s, render.View("error", render.Error(msg)))
	} else {
		err = s.caller.PatchElements(ctx, "#"+render.ErrorID, render.Error(msg), publisher.WithMode("outer"))
	}
	if err != nil {
		log.Printf("show error: %v", err)
	}
	return &Fault{Code: code, Message: msg}
}

// region replaces the whole console (the root of the Patch Region).
func (c *Console) region(ctx context.Context, s *session, html string) error {
	return s.caller.PatchElements(ctx, render.RegionSelector, html, publisher.WithRegion("console"), publisher.WithMode("inner"))
}

func (c *Console) setView(ctx context.Context, s *session, name, body string) error {
	s.mu.Lock()
	s.view = name
	s.confirm = false
	s.mu.Unlock()
	return c.region(ctx, s, render.View(name, body))
}

func patchFault(err error) *Fault {
	var ce *publisher.CallerError
	if errors.As(err, &ce) {
		return &Fault{Code: ce.Code, Message: ce.Message}
	}
	return &Fault{Code: unavailable, Message: "cannot patch the page: " + err.Error()}
}

func herdrFault(err error) *Fault {
	msg := err.Error()
	switch herdr.Code(err) {
	case "agent_not_found":
		return &Fault{Code: notFound, Message: "that herdr session is gone"}
	case "agent_blocked":
		return &Fault{Code: invalid, Message: "the session waits on a dialog: answer it first"}
	case "herdr_missing":
		return &Fault{Code: unavailable, Message: msg}
	}
	return &Fault{Code: unavailable, Message: "herdr: " + msg}
}

// agents returns the live agents, from the watcher when there is one.
func (c *Console) agents(ctx context.Context) ([]herdr.Agent, error) {
	if c.watch != nil {
		if a := c.watch.Agents(); len(a) > 0 || c.watch.Err() == nil {
			return a, nil
		}
	}
	return c.herdr.List(ctx)
}

func (c *Console) agent(ctx context.Context, pane string) (herdr.Agent, error) {
	if pane == "" {
		return herdr.Agent{}, &herdr.Error{Code: "agent_not_found", Message: "no pane"}
	}
	return c.herdr.Get(ctx, pane)
}

// IsCrew reports whether an agent is one of the crew sessions.
func (c *Console) IsCrew(a herdr.Agent) bool {
	for _, n := range c.cfg.Crew {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		if strings.ToLower(a.Name) == n || strings.ToLower(strings.TrimSpace(a.Title)) == n {
			return true
		}
	}
	return false
}

// NeedsConfirm reports whether input to a needs the two-step confirm:
// crew sessions and sessions the console did not open.
func (c *Console) NeedsConfirm(a herdr.Agent) bool {
	return c.IsCrew(a) || !c.opened.Has(a.Pane)
}

// ---- open, select ----

func (c *Console) open(ctx context.Context, s *session, filter string) (map[string]any, *Fault) {
	agents, err := c.agents(ctx)
	if err != nil {
		return nil, c.fail(ctx, s, unavailable, herdrFault(err).Message)
	}
	spaces, _ := c.herdr.Workspaces(ctx)
	s.mu.Lock()
	selected := s.selected
	s.mu.Unlock()
	groups := c.groups(agents, spaces, selected, strings.ToLower(strings.TrimSpace(filter)))
	if err := c.setView(ctx, s, "picker", render.Picker(groups, filter)); err != nil {
		return nil, patchFault(err)
	}
	return map[string]any{"ok": true, "view": "picker", "sessions": len(agents)}, nil
}

func (c *Console) groups(agents []herdr.Agent, spaces []herdr.Workspace, selected, filter string) []render.Group {
	label := map[string]string{}
	order := map[string]int{}
	for i, w := range spaces {
		label[w.ID] = w.Label
		order[w.ID] = i
	}
	byWS := map[string][]render.Row{}
	var wsIDs []string
	var opened []render.Row
	for _, a := range agents {
		o, isOpened := c.opened.Get(a.Pane)
		row := render.Row{Pane: a.Pane, Name: a.Label(), Folder: a.Cwd, Kind: a.Kind, Status: a.Status, Crew: c.IsCrew(a), Opened: isOpened, Selected: a.Pane == selected, Taught: o.Taught}
		if filter != "" && !strings.Contains(strings.ToLower(row.Name+" "+row.Folder), filter) {
			continue
		}
		if isOpened {
			opened = append(opened, row)
			continue
		}
		if _, ok := byWS[a.Workspace]; !ok {
			wsIDs = append(wsIDs, a.Workspace)
		}
		byWS[a.Workspace] = append(byWS[a.Workspace], row)
	}
	sort.SliceStable(wsIDs, func(i, j int) bool { return order[wsIDs[i]] < order[wsIDs[j]] })
	var groups []render.Group
	for _, id := range wsIDs {
		title := label[id]
		if title == "" {
			title = id
		}
		groups = append(groups, render.Group{Title: title, Rows: byWS[id]})
	}
	groups = append(groups, render.Group{Title: "Opened from the console", Rows: opened})
	return groups
}

func (c *Console) pillAgent(a herdr.Agent) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"pillAgent": map[string]any{
		"pane": a.Pane, "name": a.Label(), "kind": kindOf(a.Kind), "status": statusOf(a.Status), "crew": c.IsCrew(a), "folder": a.Cwd,
	}})
	return raw
}

func kindOf(k string) string {
	if k == "" {
		return "other"
	}
	return k
}

func statusOf(s string) string {
	switch s {
	case "idle", "working", "blocked", "done":
		return s
	}
	return "unknown"
}

func (c *Console) head(a herdr.Agent, source string) render.Head {
	return render.Head{Pane: a.Pane, Name: a.Label(), Kind: a.Kind, Status: a.Status, Folder: a.Cwd, Crew: c.IsCrew(a), Source: source}
}

func (c *Console) selectPane(ctx context.Context, s *session, pane string) (map[string]any, *Fault) {
	a, err := c.agent(ctx, pane)
	if err != nil {
		return nil, c.fail(ctx, s, notFound, herdrFault(err).Message)
	}
	s.mu.Lock()
	s.selected = a.Pane
	s.pending = nil
	s.mu.Unlock()
	if err := s.caller.PatchSignals(ctx, c.pillAgent(a), false); err != nil {
		return nil, patchFault(err)
	}
	f := c.ensureFollower(s, a)
	if err := c.showThread(ctx, s, f); err != nil {
		return nil, patchFault(err)
	}
	// The Agents panel, when open, moves its highlight to this session.
	c.pushPanel(ctx, s)
	return map[string]any{"ok": true, "view": "thread", "pane": a.Pane, "source": f.source()}, nil
}

// showThread renders the whole thread view of the follower's pane.
func (c *Console) showThread(ctx context.Context, s *session, f *follower) error {
	s.mu.Lock()
	s.view = "thread"
	s.mu.Unlock()
	return f.renderAll(ctx)
}

// showConfirm patches the confirm bar into the thread view (shown first
// when another view is up: the bar's slot lives only there).
func (c *Console) showConfirm(ctx context.Context, s *session, f *follower, html string) error {
	s.mu.Lock()
	inThread := s.view == "thread"
	s.mu.Unlock()
	if !inThread {
		if err := c.showThread(ctx, s, f); err != nil {
			return err
		}
	}
	if err := s.caller.PatchElements(ctx, "#"+render.ConfirmID, html, publisher.WithMode("outer")); err != nil {
		return err
	}
	s.mu.Lock()
	s.confirm = true
	s.mu.Unlock()
	return nil
}

// clearConfirm empties the confirm bar, only when it is on the page: the
// thread view is shown and holds a bar. Another view has no #hc-confirm,
// and a patch to it would only warn in the browser.
func (c *Console) clearConfirm(ctx context.Context, s *session) error {
	s.mu.Lock()
	shown := s.view == "thread" && s.confirm
	s.confirm = false
	s.mu.Unlock()
	if !shown {
		return nil
	}
	return s.caller.PatchElements(ctx, "#"+render.ConfirmID, emptyConfirm(), publisher.WithMode("outer"))
}

// ---- send, keys ----

func (c *Console) send(ctx context.Context, s *session, req Request) (map[string]any, *Fault) {
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return nil, c.fail(ctx, s, invalid, "write a message first")
	}
	if len(text) > MaxText {
		return nil, c.fail(ctx, s, invalid, fmt.Sprintf("the message is over %d KB", MaxText>>10))
	}
	a, err := c.agent(ctx, req.Pane)
	if err != nil {
		return nil, c.fail(ctx, s, notFound, herdrFault(err).Message)
	}
	f := c.ensureFollower(s, a)
	s.mu.Lock()
	if s.view != "thread" {
		s.mu.Unlock()
		if err := c.showThread(ctx, s, f); err != nil {
			return nil, patchFault(err)
		}
		s.mu.Lock()
	}
	confirmed := req.Confirm && s.pending != nil && s.pending.pane == a.Pane && s.pending.text == text && len(s.pending.keys) == 0
	if c.NeedsConfirm(a) && !confirmed {
		s.pending = &pending{pane: a.Pane, text: text}
		s.mu.Unlock()
		if err := c.showConfirm(ctx, s, f, render.Confirm(a.Pane, a.Label(), a.Kind, text, nil, c.IsCrew(a))); err != nil {
			return nil, patchFault(err)
		}
		return map[string]any{"ok": true, "view": "confirm", "sent": false}, nil
	}
	s.pending = nil
	s.mu.Unlock()
	_ = c.clearConfirm(ctx, s)
	if a.Status == "blocked" {
		return nil, c.fail(ctx, s, invalid, a.Label()+" waits on a dialog: answer it in the card first")
	}
	f.expect(text)
	if err := c.herdr.Prompt(ctx, a.Pane, text); err != nil {
		f.unexpect()
		hf := herdrFault(err)
		return nil, c.fail(ctx, s, hf.Code, hf.Message)
	}
	f.wake()
	_ = s.caller.PatchElements(ctx, "#"+render.ErrorID, render.ClearError(), publisher.WithMode("outer"))
	return map[string]any{"ok": true, "view": "thread", "sent": true, "pane": a.Pane}, nil
}

func emptyConfirm() string { return `<div id="` + render.ConfirmID + `" hidden></div>` }

var keyRe = regexp.MustCompile(`^(?:[1-9]|enter|esc|tab|up|down|left|right|space|y|n)$`)

func (c *Console) keys(ctx context.Context, s *session, req Request) (map[string]any, *Fault) {
	if len(req.Keys) == 0 || len(req.Keys) > 4 {
		return nil, c.fail(ctx, s, invalid, "choose one key from the dialog card")
	}
	for _, k := range req.Keys {
		if !keyRe.MatchString(k) {
			return nil, c.fail(ctx, s, invalid, "the key "+fmt.Sprintf("%q", k)+" is not one a dialog offers (1-9, enter, esc, tab, arrows, space, y, n)")
		}
	}
	a, err := c.agent(ctx, req.Pane)
	if err != nil {
		return nil, c.fail(ctx, s, notFound, herdrFault(err).Message)
	}
	if a.Status != "blocked" {
		return nil, c.fail(ctx, s, invalid, a.Label()+" is not waiting on a dialog: keys go only to a blocked session")
	}
	s.mu.Lock()
	confirmed := req.Confirm && s.pending != nil && s.pending.pane == a.Pane && strings.Join(s.pending.keys, " ") == strings.Join(req.Keys, " ")
	if c.NeedsConfirm(a) && !confirmed {
		s.pending = &pending{pane: a.Pane, keys: append([]string(nil), req.Keys...)}
		s.mu.Unlock()
		if err := c.showConfirm(ctx, s, c.ensureFollower(s, a), render.Confirm(a.Pane, a.Label(), a.Kind, "", req.Keys, c.IsCrew(a))); err != nil {
			return nil, patchFault(err)
		}
		return map[string]any{"ok": true, "view": "confirm", "sent": false}, nil
	}
	s.pending = nil
	s.mu.Unlock()
	_ = c.clearConfirm(ctx, s)
	if err := c.herdr.SendKeys(ctx, a.Pane, req.Keys); err != nil {
		hf := herdrFault(err)
		return nil, c.fail(ctx, s, hf.Code, hf.Message)
	}
	if f := c.follower(s); f != nil {
		f.wake()
	}
	return map[string]any{"ok": true, "sent": true}, nil
}

// ---- new, start ----

func (c *Console) recentFolders(ctx context.Context) []string {
	seen := map[string]bool{}
	var out []string
	agents, _ := c.agents(ctx)
	for _, a := range agents {
		if a.Cwd != "" && !seen[a.Cwd] {
			seen[a.Cwd] = true
			out = append(out, a.Cwd)
		}
	}
	for _, o := range c.opened.All() {
		if o.Cwd != "" && !seen[o.Cwd] {
			seen[o.Cwd] = true
			out = append(out, o.Cwd)
		}
	}
	sort.Strings(out)
	return out
}

func otherKinds() []string {
	var out []string
	for _, k := range herdr.Kinds {
		if k != "claude" && k != "codex" && k != "pi" {
			out = append(out, k)
		}
	}
	return out
}

func (c *Console) newForm(ctx context.Context, s *session, v render.FormValues) (map[string]any, *Fault) {
	if err := c.setView(ctx, s, "form", render.Form(otherKinds(), c.recentFolders(ctx), v)); err != nil {
		return nil, patchFault(err)
	}
	return map[string]any{"ok": true, "view": "form"}, nil
}

var nameClean = regexp.MustCompile(`[^a-z0-9_-]+`)

// ExpandFolder turns "~/x" into an absolute, clean path.
func ExpandFolder(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h := herdr.Home(); h != "" {
			p = filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// DefaultName is <kind>-<folder>, cut to herdr's name rule.
func DefaultName(kind, cwd string) string {
	base := nameClean.ReplaceAllString(strings.ToLower(filepath.Base(cwd)), "-")
	n := strings.Trim(kind+"-"+base, "-_")
	if len(n) > 32 {
		n = strings.TrimRight(n[:32], "-_")
	}
	return n
}

func (c *Console) start(ctx context.Context, s *session, req Request) (map[string]any, *Fault) {
	v := render.FormValues{Kind: req.Kind, OtherKind: req.OtherKind, Cwd: req.Cwd, Name: req.Name}
	formErr := func(code, msg string) (map[string]any, *Fault) {
		_, _ = c.newForm(ctx, s, v)
		return nil, c.fail(ctx, s, code, msg)
	}
	kind := req.Kind
	if kind == "more" || kind == "" && req.OtherKind != "" {
		kind = req.OtherKind
	}
	if !herdr.KnownKind(kind) {
		return formErr(invalid, "choose an agent herdr can start")
	}
	cwd := ExpandFolder(req.Cwd)
	if cwd == "" || !filepath.IsAbs(cwd) {
		return formErr(invalid, "give the folder as an absolute path or one under ~")
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return formErr(invalid, "the folder "+cwd+" does not exist")
	}
	agents, err := c.herdr.List(ctx)
	if err != nil {
		return formErr(unavailable, herdrFault(err).Message)
	}
	taken := map[string]bool{}
	for _, a := range agents {
		if a.Name != "" {
			taken[a.Name] = true
		}
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		base := DefaultName(kind, cwd)
		name = base
		for i := 2; taken[name] || c.crewName(name); i++ {
			suffix := fmt.Sprintf("-%d", i)
			name = strings.TrimRight(base[:min(len(base), 32-len(suffix))], "-_") + suffix
		}
	}
	if !herdr.NamePattern.MatchString(name) {
		return formErr(invalid, "the name must be lower case letters, digits, - or _, start with a letter, at most 32 characters")
	}
	if taken[name] {
		return formErr(invalid, "a live session is already named "+name)
	}
	if c.crewName(name) {
		return formErr(invalid, name+" is a crew name: choose another")
	}
	if err := c.setView(ctx, s, "starting", render.Starting(name, kind, cwd)); err != nil {
		return nil, patchFault(err)
	}
	go c.startAsync(s, name, kind, cwd)
	return map[string]any{"ok": true, "view": "starting", "name": name}, nil
}

func (c *Console) crewName(name string) bool {
	for _, n := range c.cfg.Crew {
		if strings.EqualFold(strings.TrimSpace(n), name) {
			return true
		}
	}
	return false
}

func (c *Console) startAsync(s *session, name, kind, cwd string) {
	ctx, cancel := context.WithTimeout(c.life, 3*time.Minute)
	defer cancel()
	o, err := c.herdr.OpenTab(ctx, c.cfg.ConsoleWorkspace, cwd, name)
	if err != nil {
		c.fail(ctx, s, unavailable, "could not open a tab in herdr: "+herdrFault(err).Message)
		return
	}
	// Record it before the agent starts, so a failed start can still be
	// closed from the picker.
	c.opened.Put(OpenedSession{Pane: o.Pane, Tab: o.Tab, Workspace: o.Workspace, Name: name, Kind: kind, Cwd: cwd, OpenedAt: c.now(), OpenedBy: s.id})
	err = c.herdr.Start(ctx, name, kind, o.Pane)
	if err != nil && herdr.Code(err) != "agent_not_ready" {
		c.fail(ctx, s, unavailable, "herdr opened "+o.Pane+" but could not start "+kind+": "+herdrFault(err).Message+". Close it from the picker.")
		return
	}
	if c.watch != nil {
		_ = c.watch.Refresh(ctx)
	}
	s.mu.Lock()
	still := s.view == "starting"
	s.mu.Unlock()
	if !still {
		return
	}
	if _, f := c.selectPane(ctx, s, o.Pane); f != nil {
		log.Printf("select %s after start: %s", o.Pane, f.Message)
	}
}

// ---- close, teach, follow, dismiss ----

func (c *Console) closePane(ctx context.Context, s *session, pane string) (map[string]any, *Fault) {
	o, ok := c.opened.Get(pane)
	if !ok {
		return nil, c.fail(ctx, s, denied, "the console closes only sessions it opened")
	}
	// The pane must still be the agent the console started: herdr may give
	// a stale id (the workspace went away, the pane moved) to another pane.
	if a, err := c.herdr.Get(ctx, pane); err == nil && a.Name != o.Name {
		c.opened.Delete(pane)
		return nil, c.fail(ctx, s, denied, "the pane "+pane+" is no longer the session "+o.Name+" the console opened; close it in herdr")
	}
	// The pane alone: another pane the user split into that tab stays.
	if err := c.herdr.ClosePane(ctx, o.Pane); err != nil && herdr.Code(err) != "pane_not_found" {
		hf := herdrFault(err)
		return nil, c.fail(ctx, s, hf.Code, hf.Message)
	}
	c.opened.Delete(pane)
	s.mu.Lock()
	// An empty selection is unknown state (a restarted Plugin): the chip
	// may still show the closed session, so clear it then too.
	wasSelected := s.selected == pane || s.selected == ""
	// The console region is re-rendered only where it showed this list or
	// this session: the picker, or the closed session's thread. A close from
	// the Agents sidebar must not replace another session's thread, nor
	// open a dismissed pill on the picker.
	repick := s.view == "picker" || (s.view == "thread" && s.selected == pane)
	if wasSelected {
		s.selected = ""
	}
	if s.follow != nil && s.follow.pane == pane {
		s.follow.stop()
		s.follow = nil
	}
	// herdr may give the pane id to another pane later.
	delete(s.memory, pane)
	s.mu.Unlock()
	if wasSelected {
		_ = s.caller.PatchSignals(ctx, json.RawMessage(`{"pillAgent":null}`), false)
	}
	if c.watch != nil {
		_ = c.watch.Refresh(ctx)
	}
	res := map[string]any{"ok": true}
	if repick {
		var f *Fault
		if res, f = c.open(ctx, s, ""); f != nil {
			c.pushPanel(ctx, s)
			return nil, f
		}
	}
	c.pushPanel(ctx, s)
	res["closed"] = pane
	return res, nil
}

func (c *Console) teach(ctx context.Context, s *session, pane string) (map[string]any, *Fault) {
	o, ok := c.opened.Get(pane)
	if !ok {
		return nil, c.fail(ctx, s, denied, "the console teaches only sessions it opened")
	}
	if o.Taught {
		return nil, c.fail(ctx, s, invalid, o.Name+" already has the patch-block primer")
	}
	a, err := c.agent(ctx, pane)
	if err != nil {
		return nil, c.fail(ctx, s, notFound, herdrFault(err).Message)
	}
	if c.IsCrew(a) {
		return nil, c.fail(ctx, s, denied, "the console does not teach a crew session")
	}
	if a.Status != "idle" && a.Status != "done" {
		return nil, c.fail(ctx, s, invalid, a.Label()+" is "+statusOf(a.Status)+": teach it when it is idle")
	}
	if err := c.herdr.Prompt(ctx, pane, Primer); err != nil {
		hf := herdrFault(err)
		return nil, c.fail(ctx, s, hf.Code, hf.Message)
	}
	o.Taught = true
	c.opened.Put(o)
	res, f := c.selectPane(ctx, s, pane)
	if f != nil {
		return nil, f
	}
	res["taught"] = true
	return res, nil
}

// followAction renews the ticket (Handle already did) and keeps the
// selected session streaming. It patches pillAgent again, so a reloaded
// page gets its chip back. After a Plugin restart the selection is gone;
// a pane in the call adopts it again.
func (c *Console) followAction(ctx context.Context, s *session, pane string, fresh, resync bool) (map[string]any, *Fault) {
	s.mu.Lock()
	if fresh {
		// A page that just loaded shows no console: nothing it held is on
		// the page any more, so the next call renders its view whole.
		s.view = ""
		s.confirm = false
		s.pending = nil
	}
	if s.selected == "" && pane != "" {
		s.selected = pane
	}
	pane = s.selected
	s.mu.Unlock()
	// The Agents panel and the ribbon badge: back in step, and live again
	// while the panel is open.
	c.resumePanel(s, fresh, resync)
	if err := c.sendBadge(ctx, s, false); err != nil && !errors.Is(err, publisher.ErrNotDelivered) {
		log.Printf("badge: %v", err)
	}
	if pane == "" {
		_ = s.caller.PatchSignals(ctx, json.RawMessage(`{"pillAgent":null}`), false)
		return map[string]any{"ok": true, "following": ""}, nil
	}
	a, err := c.agent(ctx, pane)
	if err != nil {
		s.mu.Lock()
		s.selected = ""
		s.mu.Unlock()
		_ = s.caller.PatchSignals(ctx, json.RawMessage(`{"pillAgent":null}`), false)
		return nil, c.fail(ctx, s, notFound, "the selected session is gone")
	}
	f := c.ensureFollower(s, a)
	if resync {
		// The follower sends it all, again on a later round if the
		// page's Stream is not back yet.
		f.resync()
		return map[string]any{"ok": true, "following": pane}, nil
	}
	if err := s.caller.PatchSignals(ctx, c.pillAgent(a), false); err != nil {
		return nil, patchFault(err)
	}
	return map[string]any{"ok": true, "following": pane}, nil
}

func (c *Console) dismiss(ctx context.Context, s *session) (map[string]any, *Fault) {
	s.mu.Lock()
	hadPending := s.pending != nil
	s.pending = nil
	s.mu.Unlock()
	if hadPending {
		if err := c.clearConfirm(ctx, s); err != nil {
			return nil, patchFault(err)
		}
		s.mu.Lock()
		view := s.view
		s.mu.Unlock()
		return map[string]any{"ok": true, "view": view}, nil
	}
	s.mu.Lock()
	s.view = ""
	s.mu.Unlock()
	if err := c.region(ctx, s, ""); err != nil {
		return nil, patchFault(err)
	}
	return map[string]any{"ok": true, "view": ""}, nil
}

// Close stops every follower.
func (c *Console) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.sessions {
		s.mu.Lock()
		if s.follow != nil {
			s.follow.stop()
		}
		if s.panel != nil {
			s.panel.cancel()
		}
		s.mu.Unlock()
	}
}
