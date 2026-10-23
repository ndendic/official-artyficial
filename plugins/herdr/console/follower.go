package console

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/blocks"
	"herdr-console/herdr"
	"herdr-console/render"
	"herdr-console/transcript"
)

// ShownTurns is how many turns the thread shows.
const ShownTurns = 3

// follower keeps one browser session's thread in step with one pane: it
// reads the agent's replies, applies patch blocks once, and streams the
// growing answer by re-sending it (outer, stable id).
type follower struct {
	c    *Console
	s    *session
	pane string

	ctx    context.Context
	cancel context.CancelFunc
	wakeCh chan struct{}

	mu    sync.Mutex
	agent herdr.Agent
	// file is the agent's own transcript (Claude, Codex, Pi), or nil
	// while the screen is read; fileKey names the session it is of.
	file    *transcript.File
	fileKey string
	// lookedFor is the session key last looked up, lastLook when.
	lookedFor string
	lastLook  time.Time
	// prompts are the last prompts the console sent to this pane.
	prompts []string
	screen  *transcript.Screen
	turns   []transcript.Turn

	earlier map[string]bool
	chips   map[string]render.Chip
	tries   map[string]int
	sent    map[string]string // answer id -> last HTML sent
	shown   []string          // turn ids of the last full render
	headSig string
	dialog  bool
	// whole asks the next round to render the thread whole (the page was
	// hidden and may have missed patches); pill asks it to send pillAgent
	// again (a send no page heard).
	whole bool
	pill  bool

	expectText string
	expectAt   time.Time
	expectN    int
	lastScreen time.Time
}

// ensureFollower returns the follower of s for a, starting one (and
// stopping the one of another pane) when needed. It selects a.
//
// Lock order: a follower's round holds f.mu and then takes s.mu, so this
// never holds s.mu while it takes a follower's mu; s.followMu keeps two
// calls of one session from starting two followers.
func (c *Console) ensureFollower(s *session, a herdr.Agent) *follower {
	s.followMu.Lock()
	defer s.followMu.Unlock()
	s.mu.Lock()
	s.selected = a.Pane
	prev := s.follow
	if prev != nil && prev.pane == a.Pane && prev.ctx.Err() == nil {
		s.mu.Unlock()
		return prev
	}
	s.mu.Unlock()
	var mem *paneMemory
	if prev != nil {
		prev.stop()
		m := prev.memory()
		s.mu.Lock()
		s.remember(prev.pane, m)
		mem = s.memory[a.Pane]
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		mem = s.memory[a.Pane]
		s.mu.Unlock()
	}
	ctx, cancel := context.WithCancel(c.life)
	f := &follower{c: c, s: s, pane: a.Pane, ctx: ctx, cancel: cancel, wakeCh: make(chan struct{}, 1), agent: a,
		earlier: map[string]bool{}, chips: map[string]render.Chip{}, tries: map[string]int{}, sent: map[string]string{}}
	// What this browser's console knew of the pane when it last followed
	// it (another pane picked since, or the ticket expired while the tab
	// was hidden): its chips, the blocks it left earlier, the prompts it
	// sent. It is restored before the source is opened (Codex finds its
	// rollout by those prompts). f runs nowhere yet: no lock needed.
	if mem != nil {
		for k, v := range mem.earlier {
			f.earlier[k] = v
		}
		for k, v := range mem.chips {
			f.chips[k] = v
		}
		f.prompts = append([]string(nil), mem.prompts...)
	}
	f.openSource()
	f.lookedFor, f.lastLook = sessionKey(a), time.Now()
	f.read()
	switch {
	case mem != nil && prev != nil && prev.pane == a.Pane:
		// The same pane again after the follower ended: blocks closed in
		// between are applied now, and none is applied twice.
	case mem != nil:
		// Back from another pane: a block that closed meanwhile is applied
		// when the console sent its turn's prompt (an answer to this
		// console); one in a turn typed elsewhere is earlier.
		f.markEarlierLocked(func(t transcript.Turn) bool { return !f.sentLocked(t.Prompt) })
	default:
		// Blocks already closed when the console starts to follow were
		// written for an earlier reader: they are shown, not applied.
		f.markEarlierLocked(func(transcript.Turn) bool { return true })
	}
	s.mu.Lock()
	s.follow = f
	s.mu.Unlock()
	go f.run()
	return f
}

// paneMemory is what one browser's console knew of a pane it followed.
type paneMemory struct {
	earlier map[string]bool
	chips   map[string]render.Chip
	prompts []string
}

// maxMemory bounds the panes a session remembers.
const maxMemory = 32

// memory is what f knows of its pane, copied.
func (f *follower) memory() *paneMemory {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := &paneMemory{earlier: make(map[string]bool, len(f.earlier)), chips: make(map[string]render.Chip, len(f.chips)), prompts: append([]string(nil), f.prompts...)}
	for k, v := range f.earlier {
		m.earlier[k] = v
	}
	for k, v := range f.chips {
		m.chips[k] = v
	}
	return m
}

// remember keeps m for when the console follows pane again. Caller holds
// s.mu.
func (s *session) remember(pane string, m *paneMemory) {
	if s.memory == nil {
		s.memory = map[string]*paneMemory{}
	}
	if _, ok := s.memory[pane]; !ok && len(s.memory) >= maxMemory {
		for k := range s.memory {
			delete(s.memory, k)
			break
		}
	}
	s.memory[pane] = m
}

// resync asks the next round to render the thread whole and send
// pillAgent again: the page was hidden and may have missed patches.
func (f *follower) resync() {
	f.mu.Lock()
	f.whole, f.pill = true, true
	f.mu.Unlock()
	f.wake()
}

func (c *Console) follower(s *session) *follower {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.follow
}

func (f *follower) stop() { f.cancel() }

func (f *follower) wake() {
	select {
	case f.wakeCh <- struct{}{}:
	default:
	}
}

func (f *follower) source() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sourceLocked()
}

// sessionKey names the session a pane runs, for the agents whose
// transcript the console reads: herdr's session id or path (Claude, Pi,
// Codex when herdr's Codex integration reports it), or the folder of a
// Codex session herdr does not name. "" for every other agent.
func sessionKey(a herdr.Agent) string {
	switch a.Kind {
	case "claude", "pi":
		return a.SessionID
	case "codex":
		if a.SessionID != "" {
			return a.SessionID
		}
		if a.Cwd != "" {
			return "cwd:" + a.Cwd
		}
	}
	return ""
}

// openSource picks the reader: the agent's own transcript when the
// console can find its file (Claude and Pi by herdr's session report,
// Codex by its session id or folder), else the screen.
func (f *follower) openSource() {
	a := f.agent
	f.file, f.fileKey = nil, ""
	if file := f.findTranscript(a); file != nil {
		f.file, f.fileKey = file, sessionKey(a)
		return
	}
	if f.screen == nil {
		f.screen = &transcript.Screen{}
	}
}

func (f *follower) findTranscript(a herdr.Agent) *transcript.File {
	cfg := f.c.cfg
	switch a.Kind {
	case "claude":
		if a.SessionID != "" && cfg.ClaudeProjects != "" {
			if path := transcript.FindClaude(cfg.ClaudeProjects, a.SessionID); path != "" {
				return transcript.NewClaude(path)
			}
		}
	case "pi":
		// herdr's pi integration reports the session file's path.
		if path := transcript.PiSession(cfg.PiSessions, a.SessionID); path != "" {
			if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
				return transcript.NewPi(path)
			}
		}
	case "codex":
		if path := f.findCodex(a); path != "" {
			return transcript.NewCodex(path)
		}
	}
	return nil
}

// findCodex finds a Codex session's rollout: by the id herdr reports, else
// the newest rollout of a session in the pane's folder. When another Codex
// session runs in the same folder, only a rollout that holds a prompt the
// console sent here is taken; before one is sent, the screen is read.
func (f *follower) findCodex(a herdr.Agent) string {
	dir := f.c.cfg.CodexSessions
	if dir == "" {
		return ""
	}
	if a.SessionID != "" && a.SessionKind != "path" {
		return transcript.FindCodex(dir, transcript.CodexLook{SessionID: a.SessionID})
	}
	look := transcript.CodexLook{Cwd: a.Cwd}
	if f.codexShared(a) {
		if len(f.prompts) == 0 {
			return ""
		}
		look.Prompt = f.prompts[len(f.prompts)-1]
	}
	return transcript.FindCodex(dir, look)
}

// codexShared reports whether another live Codex session has a's folder.
func (f *follower) codexShared(a herdr.Agent) bool {
	var agents []herdr.Agent
	if f.c.watch != nil {
		agents = f.c.watch.Agents()
	}
	if len(agents) == 0 {
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		agents, _ = f.c.herdr.List(ctx)
		cancel()
	}
	for _, o := range agents {
		if o.Pane != a.Pane && o.Kind == "codex" && filepath.Clean(o.Cwd) == filepath.Clean(a.Cwd) {
			return true
		}
	}
	return false
}

// expect records a prompt the console just sent.
func (f *follower) expect(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expectText, f.expectAt = text, time.Now()
	f.expectN++
	f.prompts = append(f.prompts, text)
	if len(f.prompts) > transcript.MaxTurns {
		f.prompts = f.prompts[len(f.prompts)-transcript.MaxTurns:]
	}
	if f.file == nil {
		f.screen.Sent(text)
	}
}

func (f *follower) unexpect() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expectText = ""
}

// read refreshes turns from the source. Caller holds no lock.
func (f *follower) read() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readLocked()
}

func (f *follower) readLocked() {
	if f.file != nil {
		turns, err := f.file.Turns()
		if err == nil {
			f.turns = turns
		}
		if f.expectText != "" {
			for _, t := range f.turns {
				if strings.TrimSpace(t.Prompt) == strings.TrimSpace(f.expectText) {
					f.expectText = ""
				}
			}
		}
		if f.expectText != "" && time.Since(f.expectAt) > 30*time.Second {
			f.expectText = ""
		}
		return
	}
	turns := f.screen.Turns()
	if n := len(turns); n > 0 && !turns[n-1].Done && time.Since(f.lastScreen) > time.Second {
		f.lastScreen = time.Now()
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		text, err := f.c.herdr.Read(ctx, f.pane, screenLines)
		cancel()
		if err == nil {
			settled := (f.agent.Status == "idle" || f.agent.Status == "done") && time.Since(f.expectAt) > 3*time.Second
			turns = f.screen.Update(text, settled)
		}
	}
	f.turns = turns
	f.expectText = "" // the screen reader carries its own turn
}

// screenLines is how much of an agent screen the screen reader reads.
const screenLines = 1000

func blockKey(pane, turn string, index int) string {
	return fmt.Sprintf("%s|%s|%d", pane, turn, index)
}

// eachBlock calls fn for every patch block of a turn, in order.
func (f *follower) eachBlock(t transcript.Turn, fn func(key string, b *blocks.Block)) {
	idx := 0
	for _, p := range t.Parts {
		if p.Text == "" {
			continue
		}
		for _, seg := range blocks.Parse(p.Text, idx) {
			if seg.Block != nil {
				fn(blockKey(f.pane, t.ID, seg.Block.Index), seg.Block)
				idx = seg.Block.Index + 1
			}
		}
	}
}

func (f *follower) run() {
	// Every way out ends the follower, so ensureFollower starts a new one
	// on the next pill call ("follow", "select") instead of keeping this
	// one as if it still ran.
	defer f.cancel()
	tick := time.NewTicker(f.c.Tick)
	defer tick.Stop()
	var sub <-chan struct{}
	if f.c.watch != nil {
		ch, unsub := f.c.watch.Subscribe()
		defer unsub()
		sub = ch
	}
	lastGet := time.Time{}
	for {
		select {
		case <-f.ctx.Done():
			return
		case <-tick.C:
		case <-sub:
		case <-f.wakeCh:
		}
		if exp := f.s.caller.Expires(); !exp.IsZero() && exp.Unix() > 0 && time.Now().After(exp) {
			return // the ticket ended: the next pill call ("follow") starts again
		}
		// Status: from the watcher; without one, ask herdr once a second.
		var a herdr.Agent
		ok := false
		if f.c.watch != nil {
			a, ok = f.c.watch.Agent(f.pane)
		}
		if !ok && time.Since(lastGet) > time.Second {
			lastGet = time.Now()
			ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
			got, err := f.c.herdr.Get(ctx, f.pane)
			cancel()
			if err == nil {
				a, ok = got, true
			} else if herdr.Code(err) == "agent_not_found" {
				f.gone()
				return
			}
		}
		if err := f.step(a, ok); err != nil {
			var ce *publisher.CallerError
			if errors.As(err, &ce) && ce.Code == denied {
				log.Printf("follower %s: %v", f.pane, err)
				return
			}
		}
	}
}

func (f *follower) gone() {
	ctx, cancel := context.WithTimeout(f.c.life, 5*time.Second)
	defer cancel()
	f.s.mu.Lock()
	if f.s.selected == f.pane {
		f.s.selected = ""
	}
	f.s.mu.Unlock()
	_ = f.s.caller.PatchSignals(ctx, []byte(`{"pillAgent":null}`), false)
	_ = f.s.caller.PatchElements(ctx, "#"+render.ErrorID, render.Error("the session "+f.agent.Label()+" ended"), publisher.WithMode("outer"))
}

// step does one round: status, replies, blocks, and the patches that
// changed. It holds f.mu throughout, so renders do not interleave.
func (f *follower) step(a herdr.Agent, haveAgent bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	statusChanged := false
	if haveAgent {
		statusChanged = a.Status != f.agent.Status || a.Label() != f.agent.Label()
		if key := sessionKey(a); key != "" && key != f.fileKey && (key != f.lookedFor || time.Since(f.lastLook) > 2*time.Second) {
			// A different session in the pane (a /clear, a /new, a new
			// agent): what its transcript already holds is earlier. The
			// same session whose file only now appeared (Claude, Codex
			// and Pi write it at the first prompt) is new since the
			// console followed it.
			newSession := key != f.lookedFor
			f.lookedFor, f.lastLook = key, time.Now()
			f.agent = a
			f.openSource()
			if f.file != nil {
				f.turns = nil
				f.readLocked()
				if newSession {
					f.markEarlierLocked(func(transcript.Turn) bool { return true })
				}
			}
		} else if f.file != nil && strings.HasPrefix(key, "cwd:") && key == f.fileKey && time.Since(f.lastLook) > 3*time.Second {
			// A Codex session herdr does not name, found by its folder:
			// a /new there starts another rollout, so look again.
			f.lastLook = time.Now()
			f.agent = a
			if path := f.findCodex(a); path != "" && path != f.file.Path() {
				f.file = transcript.NewCodex(path)
				f.turns = nil
				f.readLocked()
				// It may be a few seconds old: its turns that ended before
				// the console saw them are earlier, unless the console
				// sent their prompt.
				f.markEarlierLocked(func(t transcript.Turn) bool { return t.Done && !f.sentLocked(t.Prompt) })
			}
		}
		f.agent = a
	}
	f.readLocked()
	if err := f.applyBlocks(ctx); err != nil {
		return err
	}
	if statusChanged || f.pill {
		f.pill = true
		if err := f.s.caller.PatchSignals(ctx, f.c.pillAgent(f.agent), false); err != nil {
			return err
		}
		f.pill = false
	}
	// The ribbon badge follows every session's status while the chip is
	// shown, panel open or not.
	if f.c.watch != nil {
		if err := f.c.sendBadge(ctx, f.s, false); err != nil {
			return err
		}
	}
	f.s.mu.Lock()
	inThread := f.s.view == "thread" && f.s.selected == f.pane
	f.s.mu.Unlock()
	if !inThread {
		return nil
	}
	views := f.viewsLocked()
	if f.whole || !sameIDs(views, f.shown) {
		return f.renderAllLocked(ctx)
	}
	if sig := f.headSigLocked(); sig != f.headSig {
		f.headSig = sig
		if err := f.s.caller.PatchElements(ctx, "", render.HeadHTML(f.c.head(f.agent, f.sourceLocked())), publisher.WithMode("outer")); err != nil {
			return err
		}
	}
	for _, v := range views {
		html := render.Answer(v)
		id := render.AnswerID(v.ID)
		if f.sent[id] == html {
			continue
		}
		if err := f.s.caller.PatchElements(ctx, "", html, publisher.WithMode("outer")); err != nil {
			return err
		}
		f.sent[id] = html
	}
	return f.dialogLocked(ctx)
}

func (f *follower) sourceLocked() string {
	if f.file != nil {
		return "transcript"
	}
	return "screen"
}

// markEarlierLocked marks the closed blocks of the turns that match as
// earlier: shown, never applied.
func (f *follower) markEarlierLocked(match func(transcript.Turn) bool) {
	for _, t := range f.turns {
		if !match(t) {
			continue
		}
		f.eachBlock(t, func(key string, b *blocks.Block) {
			if _, done := f.chips[key]; b.Closed && !done {
				f.earlier[key] = true
			}
		})
	}
}

// sentLocked reports whether the console sent prompt to this pane.
func (f *follower) sentLocked(prompt string) bool {
	p := strings.TrimSpace(prompt)
	for _, s := range f.prompts {
		if strings.TrimSpace(s) == p {
			return true
		}
	}
	return false
}

func (f *follower) headSigLocked() string {
	a := f.agent
	return a.Label() + "|" + a.Status + "|" + a.Cwd + "|" + f.sourceLocked()
}

// applyBlocks applies every closed, valid block not applied before. A
// block no page heard (publisher.ErrNotDelivered: the tab is hidden) stays
// unchipped and is sent again on a later round; the round stops there, as
// every other patch would wait the same.
func (f *follower) applyBlocks(ctx context.Context) error {
	var stopped error
	for _, t := range f.turns {
		f.eachBlock(t, func(key string, b *blocks.Block) {
			if stopped != nil || !b.Closed || f.earlier[key] {
				return
			}
			if _, done := f.chips[key]; done {
				return
			}
			chip := render.Chip{Label: b.Summary(), Raw: b.Raw}
			if b.Err != "" {
				chip.Reason = b.Err
			} else if err := f.apply(ctx, b); errors.Is(err, publisher.ErrNotDelivered) {
				stopped = err
				return
			} else if err != nil {
				// A refusal is final. A transport error (the Broker
				// connection renewing at the Authorization Lease, a
				// timeout) is tried again on the next rounds, up to a cap.
				if !finalRefusal(err) && f.tries[key] < 8 {
					f.tries[key]++
					return
				}
				if sharedRegion(err) {
					chip.Off = true
				} else {
					chip.Reason = reason(err)
				}
			} else {
				chip.OK = true
			}
			f.chips[key] = chip
		})
	}
	return stopped
}

// finalRefusal reports whether a caller patch error is core's answer about
// the patch itself (refused, or not allowed), which no retry changes.
// publisher.Caller wraps transport failures as CallerError too, with the
// codes unavailable and deadline_exceeded; those are not final.
func finalRefusal(err error) bool {
	var ce *publisher.CallerError
	if !errors.As(err, &ce) {
		return false
	}
	return ce.Code == invalid || ce.Code == denied
}

// sharedRegion reports core's refusal of a session patch into a shared
// Patch Region (RT Station's shared-region rule): the block is not wrong,
// the page has no personal region for it.
func sharedRegion(err error) bool {
	return strings.Contains(reason(err), "is shared; it takes page patches")
}

func reason(err error) string {
	var ce *publisher.CallerError
	if errors.As(err, &ce) {
		return ce.Message
	}
	return err.Error()
}

func (f *follower) apply(ctx context.Context, b *blocks.Block) error {
	if b.Kind == blocks.Signals {
		if b.Region != "" {
			return errors.New("a patch-signals block takes no region")
		}
		return f.s.caller.PatchSignals(ctx, []byte(b.Body), false)
	}
	opts := []publisher.PatchOption{publisher.WithMode(b.EffectiveMode())}
	if b.Region != "" {
		opts = append(opts, publisher.WithRegion(b.Region))
	}
	return f.s.caller.PatchElements(ctx, b.Selector, b.Body, opts...)
}

// viewsLocked builds the shown turns.
func (f *follower) viewsLocked() []render.TurnView {
	turns := f.turns
	if len(turns) > ShownTurns {
		turns = turns[len(turns)-ShownTurns:]
	}
	var out []render.TurnView
	for _, t := range turns {
		out = append(out, f.viewLocked(t))
	}
	if f.expectText != "" {
		out = append(out, render.TurnView{ID: fmt.Sprintf("expect-%d", f.expectN), Prompt: f.expectText})
		if len(out) > ShownTurns {
			out = out[len(out)-ShownTurns:]
		}
	}
	return out
}

func (f *follower) viewLocked(t transcript.Turn) render.TurnView {
	v := render.TurnView{ID: t.ID, Prompt: t.Prompt, Done: t.Done}
	idx := 0
	for _, p := range t.Parts {
		if p.Tools > 0 {
			v.Items = append(v.Items, render.Item{Tools: p.Tools})
			continue
		}
		for _, seg := range blocks.Parse(p.Text, idx) {
			if seg.Block == nil {
				if strings.TrimSpace(seg.Text) != "" {
					v.Items = append(v.Items, render.Item{Text: seg.Text})
				}
				continue
			}
			b := seg.Block
			idx = b.Index + 1
			key := blockKey(f.pane, t.ID, b.Index)
			switch {
			case !b.Closed:
				v.Items = append(v.Items, render.Item{Pending: b.Kind})
			case f.earlier[key]:
				v.Items = append(v.Items, render.Item{Chip: &render.Chip{Earlier: true, Label: b.Summary(), Raw: b.Raw}})
			default:
				chip, ok := f.chips[key]
				if !ok {
					chip = render.Chip{Waiting: true, Label: b.Summary()}
				}
				v.Items = append(v.Items, render.Item{Chip: &chip})
			}
		}
	}
	return v
}

func sameIDs(views []render.TurnView, ids []string) bool {
	if len(views) != len(ids) {
		return false
	}
	for i, v := range views {
		if v.ID != ids[i] {
			return false
		}
	}
	return true
}

// renderAll re-renders the whole thread view.
func (f *follower) renderAll(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readLocked()
	return f.renderAllLocked(ctx)
}

func (f *follower) renderAllLocked(ctx context.Context) error {
	views := f.viewsLocked()
	note := ""
	if f.file == nil {
		note = "The console shows replies to messages you send from here (it reads this agent's screen)."
	}
	body := render.Thread(f.c.head(f.agent, f.sourceLocked()), views, note) + emptyConfirm()
	dialogHTML, shown := f.dialogHTMLLocked(ctx)
	body += dialogHTML
	if err := f.c.region(ctx, f.s, render.View("thread", body)); err != nil {
		return err
	}
	// The whole view was replaced: its confirm slot is empty again.
	f.s.mu.Lock()
	f.s.confirm = false
	f.s.mu.Unlock()
	f.dialog = shown
	f.whole = false
	f.headSig = f.headSigLocked()
	f.shown = f.shown[:0]
	f.sent = map[string]string{}
	for _, v := range views {
		f.shown = append(f.shown, v.ID)
		f.sent[render.AnswerID(v.ID)] = render.Answer(v)
	}
	return nil
}

// dialogLocked shows or hides the dialog card as the status changes.
func (f *follower) dialogLocked(ctx context.Context) error {
	blocked := f.agent.Status == "blocked"
	if blocked == f.dialog {
		return nil
	}
	html, shown := f.dialogHTMLLocked(ctx)
	if err := f.s.caller.PatchElements(ctx, "", html, publisher.WithMode("outer")); err != nil {
		return err
	}
	f.dialog = shown
	return nil
}

func (f *follower) dialogHTMLLocked(ctx context.Context) (string, bool) {
	if f.agent.Status != "blocked" {
		return render.NoDialog(), false
	}
	screen, err := f.c.herdr.Read(ctx, f.pane, 40)
	if err != nil {
		screen = "(cannot read the screen: " + err.Error() + ")"
	}
	text, keys := DialogText(screen)
	return render.Dialog(f.pane, f.agent.Label(), text, keys), true
}

var optionLine = regexp.MustCompile(`^\s*(?:[❯>›]\s*)?([1-9])[.)]\s+\S`)

// DialogText cuts a blocked agent's screen to its dialog (the last 18
// non-empty lines, chrome kept: a dialog is drawn in it) and lists the
// keys it offers: its numbered options, then the arrows, Enter and Esc.
func DialogText(screen string) (string, []string) {
	var lines []string
	for _, l := range strings.Split(screen, "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, strings.TrimRight(l, " "))
		}
	}
	if len(lines) > 18 {
		lines = lines[len(lines)-18:]
	}
	var keys []string
	seen := map[string]bool{}
	for _, l := range lines {
		if m := optionLine.FindStringSubmatch(l); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			keys = append(keys, m[1])
		}
	}
	keys = append(keys, "up", "down", "enter", "esc")
	return strings.Join(lines, "\n"), keys
}
