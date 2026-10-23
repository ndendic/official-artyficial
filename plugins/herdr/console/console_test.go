package console

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ndendic/data-start/sdk/go/publisher"

	"herdr-console/herdr"
)

// patch is one recorded Caller patch.
type patch struct {
	session  string
	selector string
	html     string
	signals  string
}

// fakeCallers hands out a fake Caller per ticket; a ticket "S:n" names
// browser session S.
type fakeCallers struct {
	mu      sync.Mutex
	patches []patch
	// refuse makes an elements patch whose HTML contains the key fail
	// with the value as the CallerError message.
	refuse map[string]string
	// flaky makes an elements patch whose HTML contains the key fail with
	// unavailable (a transport error) that many times.
	flaky map[string]int
	// expires is what every Caller's Expires answers.
	expires time.Time
	// absent makes any patch fail with publisher.ErrNotDelivered (no page
	// listens: the tab is hidden) while it is true.
	absent bool
	// deny makes any patch fail with permission_denied (an expired or
	// foreign ticket) while it is true.
	deny bool
}

type fakeCaller struct {
	f      *fakeCallers
	mu     sync.Mutex
	ticket string
}

func (f *fakeCallers) new(ticket string) Patcher { return &fakeCaller{f: f, ticket: ticket} }

func (c *fakeCaller) session() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, _, _ := strings.Cut(c.ticket, ":")
	return s
}

func (c *fakeCaller) PatchElements(_ context.Context, selector, html string, _ ...publisher.PatchOption) error {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	if c.f.deny {
		return &publisher.CallerError{Code: "permission_denied", Message: "the Caller Ticket expired"}
	}
	if c.f.absent {
		return publisher.ErrNotDelivered
	}
	for k, msg := range c.f.refuse {
		if strings.Contains(html, k) {
			return &publisher.CallerError{Code: "invalid_argument", Message: msg}
		}
	}
	for k, n := range c.f.flaky {
		if n > 0 && strings.Contains(html, k) {
			c.f.flaky[k] = n - 1
			return &publisher.CallerError{Code: "unavailable", Message: "core.ui.caller.patch: nats: connection closed"}
		}
	}
	c.f.patches = append(c.f.patches, patch{session: c.session(), selector: selector, html: html})
	return nil
}

func (c *fakeCaller) PatchSignals(_ context.Context, signals json.RawMessage, _ bool) error {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	if c.f.deny {
		return &publisher.CallerError{Code: "permission_denied", Message: "the Caller Ticket expired"}
	}
	if c.f.absent {
		return publisher.ErrNotDelivered
	}
	c.f.patches = append(c.f.patches, patch{session: c.session(), signals: string(signals)})
	return nil
}

func (c *fakeCaller) Renew(t string) {
	c.mu.Lock()
	c.ticket = t
	c.mu.Unlock()
}
func (c *fakeCaller) SessionID() string { return c.session() }
func (c *fakeCaller) Expires() time.Time {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	return c.f.expires
}

func (f *fakeCallers) all() []patch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]patch(nil), f.patches...)
}

func (f *fakeCallers) html() string {
	var b strings.Builder
	for _, p := range f.all() {
		b.WriteString(p.html)
		b.WriteString(p.signals)
		b.WriteString("\n")
	}
	return b.String()
}

func (f *fakeCallers) reset() {
	f.mu.Lock()
	f.patches = nil
	f.mu.Unlock()
}

var crew = []string{"reviewer", "docs-site", "api"}

func setup(t *testing.T, agents ...herdr.Agent) (*Console, *herdr.Fake, *fakeCallers) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := herdr.NewFake(agents...)
	fc := &fakeCallers{}
	c := New(ctx, Config{Crew: crew, ConsoleWorkspace: "Console", DataDir: t.TempDir(), ClaudeProjects: t.TempDir()}, h, nil, fc.new)
	c.Tick = 20 * time.Millisecond
	t.Cleanup(func() { c.Close(); cancel() })
	return c, h, fc
}

var (
	reviewer = herdr.Agent{Pane: "w1:p1", Workspace: "w1", Title: "Reviewer", Kind: "claude", Status: "idle", Cwd: "/tmp"}
	docsSite = herdr.Agent{Pane: "w1:p2", Workspace: "w1", Name: "docs-site", Kind: "claude", Status: "done", Cwd: "/tmp"}
	stranger = herdr.Agent{Pane: "w9:p1", Workspace: "w9", Name: "scratch", Kind: "codex", Status: "idle", Cwd: "/tmp"}
)

func call(t *testing.T, c *Console, ticket string, req Request) (map[string]any, *Fault) {
	t.Helper()
	return c.Handle(context.Background(), ticket, req)
}

func TestSendToCrewNeedsConfirm(t *testing.T) {
	for _, a := range []herdr.Agent{reviewer, docsSite, stranger} {
		t.Run(a.Label(), func(t *testing.T) {
			c, h, fc := setup(t, a)
			res, f := call(t, c, "A:1", Request{Action: "send", Pane: a.Pane, Text: "status?"})
			if f != nil || res["sent"] != false || res["view"] != "confirm" {
				t.Fatalf("first send = %v, %v", res, f)
			}
			if len(h.Prompts) != 0 {
				t.Fatalf("a send without confirm reached the pane: %+v", h.Prompts)
			}
			if !strings.Contains(fc.html(), `class="hc-confirm"`) {
				t.Fatal("no confirm bar was rendered")
			}
			// confirm:true for another text does not deliver.
			res, _ = call(t, c, "A:2", Request{Action: "send", Pane: a.Pane, Text: "something else", Confirm: true})
			if res["sent"] != false || len(h.Prompts) != 0 {
				t.Fatalf("a confirm for another text delivered: %v %+v", res, h.Prompts)
			}
			// The pending confirm is now "something else"; another browser
			// session cannot confirm it.
			res, _ = call(t, c, "B:1", Request{Action: "send", Pane: a.Pane, Text: "something else", Confirm: true})
			if res["sent"] != false || len(h.Prompts) != 0 {
				t.Fatalf("another session confirmed: %v %+v", res, h.Prompts)
			}
			// The bar's own confirm delivers.
			res, f = call(t, c, "A:3", Request{Action: "send", Pane: a.Pane, Text: "something else", Confirm: true})
			if f != nil || res["sent"] != true || len(h.Prompts) != 1 || h.Prompts[0].Text != "something else" {
				t.Fatalf("confirmed send = %v %v %+v", res, f, h.Prompts)
			}
			// A confirm is used once.
			res, _ = call(t, c, "A:4", Request{Action: "send", Pane: a.Pane, Text: "something else", Confirm: true})
			if res["sent"] != false || len(h.Prompts) != 1 {
				t.Fatalf("a confirm was used twice: %v %+v", res, h.Prompts)
			}
		})
	}
}

func TestConfirmWithoutPendingDoesNotDeliver(t *testing.T) {
	c, h, _ := setup(t, reviewer)
	res, _ := call(t, c, "A:1", Request{Action: "send", Pane: reviewer.Pane, Text: "hi", Confirm: true})
	if res["sent"] != false || len(h.Prompts) != 0 {
		t.Fatalf("confirm:true alone delivered: %v %+v", res, h.Prompts)
	}
}

func TestDismissCancelsConfirm(t *testing.T) {
	c, h, _ := setup(t, docsSite)
	call(t, c, "A:1", Request{Action: "send", Pane: docsSite.Pane, Text: "hi"})
	call(t, c, "A:2", Request{Action: "dismiss"})
	res, _ := call(t, c, "A:3", Request{Action: "send", Pane: docsSite.Pane, Text: "hi", Confirm: true})
	if res["sent"] != false || len(h.Prompts) != 0 {
		t.Fatalf("a dismissed confirm delivered: %v %+v", res, h.Prompts)
	}
}

func startSession(t *testing.T, c *Console, h *herdr.Fake, ticket string) string {
	t.Helper()
	dir := t.TempDir()
	res, f := call(t, c, ticket, Request{Action: "start", Kind: "claude", Cwd: dir})
	if f != nil {
		t.Fatalf("start: %v", f)
	}
	name := res["name"].(string)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, o := range c.Opened().All() {
			if o.Name == name {
				if _, err := h.Get(context.Background(), o.Pane); err == nil {
					return o.Pane
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the started session never appeared")
	return ""
}

func TestStartOpensInConsoleWorkspaceAndSendsWithoutConfirm(t *testing.T) {
	c, h, fc := setup(t, reviewer)
	pane := startSession(t, c, h, "A:1")
	spaces, _ := h.Workspaces(context.Background())
	if len(spaces) != 1 || spaces[0].Label != "Console" {
		t.Fatalf("workspaces = %+v", spaces)
	}
	if !strings.Contains(fc.html(), "Starting") {
		t.Fatal("no starting line")
	}
	res, f := call(t, c, "A:2", Request{Action: "send", Pane: pane, Text: "reply with the word ready"})
	if f != nil || res["sent"] != true || len(h.Prompts) != 1 {
		t.Fatalf("send to an opened session = %v %v %+v", res, f, h.Prompts)
	}
	// The registry survives a restart of the Plugin.
	again := LoadRegistry(filepath.Join(c.cfg.DataDir, "opened.json"))
	if !again.Has(pane) {
		t.Fatal("the opened session was not persisted")
	}
}

func TestStartRefusesCrewNamesAndBadFolders(t *testing.T) {
	c, h, _ := setup(t)
	if _, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: t.TempDir(), Name: "api"}); f == nil || !strings.Contains(f.Message, "crew name") {
		t.Fatalf("a crew name was accepted: %v", f)
	}
	if _, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: "/nope/not/here"}); f == nil || !strings.Contains(f.Message, "does not exist") {
		t.Fatalf("a missing folder was accepted: %v", f)
	}
	if _, f := call(t, c, "A:1", Request{Action: "start", Kind: "vim", Cwd: t.TempDir()}); f == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if _, f := call(t, c, "A:1", Request{Action: "start", Kind: "claude", Cwd: "relative/dir"}); f == nil {
		t.Fatal("a relative folder was accepted")
	}
	if len(h.Started) != 0 {
		t.Fatalf("started = %v", h.Started)
	}
}

func TestCloseOnlyOpened(t *testing.T) {
	c, h, _ := setup(t, reviewer, stranger)
	for _, a := range []herdr.Agent{reviewer, stranger} {
		if _, f := call(t, c, "A:1", Request{Action: "close", Pane: a.Pane}); f == nil || f.Code != denied {
			t.Fatalf("close %s = %v", a.Pane, f)
		}
	}
	if len(h.Closed) != 0 {
		t.Fatalf("closed = %v", h.Closed)
	}
	pane := startSession(t, c, h, "A:1")
	// The user splits the console's tab and runs a shell beside it.
	mine, _ := h.Get(context.Background(), pane)
	beside := herdr.Agent{Pane: mine.Workspace + ":p99", Tab: mine.Tab, Workspace: mine.Workspace, Name: "shell", Kind: "shell", Status: "idle"}
	h.Put(beside)
	if _, f := call(t, c, "A:2", Request{Action: "close", Pane: pane}); f != nil {
		t.Fatalf("close opened = %v", f)
	}
	if len(h.Closed) != 1 || h.Closed[0] != pane || c.Opened().Has(pane) {
		t.Fatalf("closed = %v, still opened = %v", h.Closed, c.Opened().Has(pane))
	}
	if _, err := h.Get(context.Background(), beside.Pane); err != nil {
		t.Fatal("close removed another pane of the same tab")
	}
}

// A stale entry whose pane id now holds another agent is not closed.
func TestCloseRefusesAReusedPane(t *testing.T) {
	c, h, _ := setup(t)
	pane := startSession(t, c, h, "A:1")
	got, _ := h.Get(context.Background(), pane)
	got.Name = "someone-else"
	h.Put(got)
	if _, f := call(t, c, "A:1", Request{Action: "close", Pane: pane}); f == nil || f.Code != denied {
		t.Fatalf("close of a reused pane = %v", f)
	}
	if len(h.Closed) != 0 {
		t.Fatalf("closed = %v", h.Closed)
	}
}

func TestKeysOnlyWhenBlocked(t *testing.T) {
	c, h, _ := setup(t, stranger)
	pane := startSession(t, c, h, "A:1")
	if _, f := call(t, c, "A:2", Request{Action: "keys", Pane: pane, Keys: []string{"1"}}); f == nil || !strings.Contains(f.Message, "not waiting on a dialog") {
		t.Fatalf("keys to an idle session = %v", f)
	}
	a, _ := h.Get(context.Background(), pane)
	a.Status = "blocked"
	h.Put(a)
	if _, f := call(t, c, "A:3", Request{Action: "keys", Pane: pane, Keys: []string{"rm -rf"}}); f == nil {
		t.Fatal("a non-key was accepted")
	}
	if res, f := call(t, c, "A:4", Request{Action: "keys", Pane: pane, Keys: []string{"1"}}); f != nil || res["sent"] != true {
		t.Fatalf("keys to a blocked opened session = %v %v", res, f)
	}
	if len(h.Keys) != 1 || h.Keys[0].Text != "1" {
		t.Fatalf("keys = %+v", h.Keys)
	}
	// A blocked session the console did not open needs the confirm too.
	s := stranger
	s.Status = "blocked"
	h.Put(s)
	res, _ := call(t, c, "A:5", Request{Action: "keys", Pane: s.Pane, Keys: []string{"2"}})
	if res["sent"] != false || len(h.Keys) != 1 {
		t.Fatalf("keys to a session not opened skipped the confirm: %v %+v", res, h.Keys)
	}
	res, _ = call(t, c, "A:6", Request{Action: "keys", Pane: s.Pane, Keys: []string{"2"}, Confirm: true})
	if res["sent"] != true || len(h.Keys) != 2 {
		t.Fatalf("confirmed keys = %v %+v", res, h.Keys)
	}
	// Sending text to a blocked session is refused, confirm or not.
	if _, f := call(t, c, "A:7", Request{Action: "send", Pane: pane, Text: "hi"}); f == nil || !strings.Contains(f.Message, "dialog") {
		t.Fatalf("send to a blocked session = %v", f)
	}
}

func TestTeachOnceOnlyOpened(t *testing.T) {
	c, h, _ := setup(t, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "teach", Pane: stranger.Pane}); f == nil || f.Code != denied {
		t.Fatalf("teach a session not opened = %v", f)
	}
	pane := startSession(t, c, h, "A:1")
	if _, f := call(t, c, "A:2", Request{Action: "teach", Pane: pane}); f != nil {
		t.Fatalf("teach = %v", f)
	}
	if _, f := call(t, c, "A:3", Request{Action: "teach", Pane: pane}); f == nil {
		t.Fatal("taught twice")
	}
	if len(h.Prompts) != 1 || h.Prompts[0].Text != Primer {
		t.Fatalf("prompts = %+v", h.Prompts)
	}
}

func TestNoTicketPatchesNothing(t *testing.T) {
	c, h, fc := setup(t, stranger)
	res, f := call(t, c, "", Request{Action: "send", Pane: stranger.Pane, Text: "x", Confirm: true})
	if f != nil || res["ok"] != false || len(fc.all()) != 0 || len(h.Prompts) != 0 {
		t.Fatalf("no ticket = %v %v", res, f)
	}
}

func TestPickerGroupsAndCrewMark(t *testing.T) {
	c, _, fc := setup(t, reviewer, docsSite, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "open"}); f != nil {
		t.Fatal(f)
	}
	html := fc.html()
	for _, want := range []string{`class="hc-picker"`, `class="hc-crew"`, "Reviewer", "scratch", `data-pill-call="{&#34;action&#34;:&#34;select&#34;,&#34;pane&#34;:&#34;w1:p1&#34;}"`, `class="hc-new"`} {
		if !strings.Contains(html, want) {
			t.Errorf("picker misses %s", want)
		}
	}
	if strings.Count(html, `class="hc-crew"`) != 2 {
		t.Errorf("crew marks = %d, want 2", strings.Count(html, `class="hc-crew"`))
	}
	if strings.Contains(html, "<script") || strings.Contains(html, " onclick") {
		t.Error("the picker has script")
	}
}

func TestSelectSetsPillAgent(t *testing.T) {
	c, _, fc := setup(t, docsSite)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: docsSite.Pane}); f != nil {
		t.Fatal(f)
	}
	var found bool
	for _, p := range fc.all() {
		if p.signals != "" {
			var v struct {
				PillAgent map[string]any `json:"pillAgent"`
			}
			if json.Unmarshal([]byte(p.signals), &v) == nil && v.PillAgent["pane"] == docsSite.Pane && v.PillAgent["crew"] == true && v.PillAgent["kind"] == "claude" && v.PillAgent["status"] == "done" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("no pillAgent signal: %+v", fc.all())
	}
}

func TestUnknownAction(t *testing.T) {
	c, _, _ := setup(t)
	if _, f := call(t, c, "A:1", Request{Action: "rm"}); f == nil || f.Code != invalid {
		t.Fatalf("unknown action = %v", f)
	}
}

// The follower applies a patch block once, when its closing fence
// arrives, and never a block that was already closed when it started.
func TestFollowerAppliesBlocksOnce(t *testing.T) {
	c, h, fc := setup(t)
	fc.refuse = map[string]string{"<my-widget>": "the Accept Rule refuses the unknown element <my-widget>"}
	session := "abc-123"
	proj := filepath.Join(c.cfg.ClaudeProjects, "-tmp")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, session+".jsonl")
	fence := "```"
	line := func(v any) string { raw, _ := json.Marshal(v); return string(raw) + "\n" }
	user := func(id, text string) string {
		return line(map[string]any{"type": "user", "uuid": "u" + id, "promptId": id, "message": map[string]any{"role": "user", "content": text}})
	}
	text := func(s string) string {
		return line(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": s}}}})
	}
	old := user("p1", "old") + text("Before:\n"+fence+"patch-signals\n{\"old\":true}\n"+fence+"\n") + line(map[string]any{"type": "system", "subtype": "turn_duration"})
	if err := os.WriteFile(file, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	a := herdr.Agent{Pane: "w9:p5", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: session}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(fc.html(), `{"old":true}`+"\n") {
		t.Fatal("a block closed before the console followed was applied")
	}
	if !strings.Contains(fc.html(), "earlier, not applied") {
		t.Fatal("the earlier block has no chip")
	}
	// A new turn. The transcript gets whole entries; the tailer must not
	// read a half-written line, so the reply arrives in two writes.
	reply := text(fence + "patch-signals\n{\"theme\":\"dark\"}\n" + fence + "\n" + fence + "patch-elements selector=\"#aside\" mode=\"append\"\n<div><my-widget>x</my-widget></div>\n" + fence + "\n" + fence + "patch-elements mode=\"sideways\" selector=\"#a\"\n<p>1</p>\n" + fence + "\nDone.")
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(user("p2", "go") + reply[:len(reply)/2])
	_ = f.Close()
	time.Sleep(100 * time.Millisecond)
	if strings.Contains(fc.html(), `{"theme":"dark"}`) {
		t.Fatal("applied from a half-written entry")
	}
	f, _ = os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(reply[len(reply)/2:])
	_ = f.Close()
	time.Sleep(200 * time.Millisecond)
	count := 0
	for _, p := range fc.all() {
		if p.signals == `{"theme":"dark"}` {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the signals block was applied %d times", count)
	}
	html := fc.html()
	for _, want := range []string{"✓ patch-signals", "the Accept Rule refuses the unknown element &lt;my-widget&gt;", "unknown mode sideways", "<details>", "Done."} {
		if !strings.Contains(html, want) {
			t.Errorf("thread misses %q", want)
		}
	}
	// Re-reading (another select) never applies again.
	fc.reset()
	call(t, c, "A:2", Request{Action: "select", Pane: a.Pane})
	time.Sleep(100 * time.Millisecond)
	for _, p := range fc.all() {
		if p.signals == `{"theme":"dark"}` {
			t.Fatal("re-applied on re-read")
		}
	}
}

func TestDialogText(t *testing.T) {
	screen := "Do you want to proceed?\n❯ 1. Yes\n  2. Yes, and don't ask again\n  3. No\n\nEsc to cancel"
	text, keys := DialogText(screen)
	if strings.Join(keys, ",") != "1,2,3,up,down,enter,esc" || !strings.Contains(text, "proceed") {
		t.Fatalf("keys = %v, text = %q", keys, text)
	}
}

func TestDefaultName(t *testing.T) {
	if got := DefaultName("claude", "/home/x/src/Docs Site"); got != "claude-docs-site" {
		t.Fatalf("got %q", got)
	}
	if got := DefaultName("claude", "/x/"+strings.Repeat("a", 40)); len(got) > 32 || !herdr.NamePattern.MatchString(got) {
		t.Fatalf("got %q", got)
	}
}

func TestFollowRestoresPillAgent(t *testing.T) {
	c, _, fc := setup(t, docsSite)
	res, f := call(t, c, "A:1", Request{Action: "follow"})
	if f != nil || res["following"] != "" {
		t.Fatalf("follow with nothing selected = %v %v", res, f)
	}
	res, f = call(t, c, "A:2", Request{Action: "follow", Pane: docsSite.Pane})
	if f != nil || res["following"] != docsSite.Pane || !strings.Contains(fc.html(), `"pane":"w1:p2"`) {
		t.Fatalf("follow with a pane = %v %v", res, f)
	}
}

// transcriptFixture writes a Claude transcript for session in the fake
// projects folder and returns an append function and the entry builders.
func transcriptFixture(t *testing.T, c *Console, session string) (appendf func(string), user func(id, text string) string, text func(string) string) {
	t.Helper()
	proj := filepath.Join(c.cfg.ClaudeProjects, "-tmp")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, session+".jsonl")
	line := func(v any) string { raw, _ := json.Marshal(v); return string(raw) + "\n" }
	user = func(id, s string) string {
		return line(map[string]any{"type": "user", "uuid": "u" + id, "promptId": id, "message": map[string]any{"role": "user", "content": s}})
	}
	text = func(s string) string {
		return line(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": s}}}})
	}
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	appendf = func(s string) {
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(s)
		_ = f.Close()
	}
	return appendf, user, text
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A follower whose ticket expired (the tab was hidden) ends for good, and
// the next call for the same pane starts a new one, which applies the
// blocks written in between.
func TestFollowerRestartsAfterTicketExpiry(t *testing.T) {
	c, h, fc := setup(t)
	appendf, user, text := transcriptFixture(t, c, "exp-1")
	a := herdr.Agent{Pane: "w9:p6", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: "exp-1"}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	c.mu.Lock()
	s := c.sessions["A"]
	c.mu.Unlock()
	first := c.follower(s)
	fc.mu.Lock()
	fc.expires = time.Now().Add(-time.Minute)
	fc.mu.Unlock()
	waitFor(t, "the expired follower to end", func() bool { return first.ctx.Err() != nil })

	fence := "```"
	appendf(user("p1", "go") + text(fence+"patch-signals\n{\"late\":1}\n"+fence+"\n"))
	fc.mu.Lock()
	fc.expires = time.Time{}
	fc.mu.Unlock()
	if _, f := call(t, c, "A:2", Request{Action: "follow", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	if c.follower(s) == first {
		t.Fatal("the ended follower was kept")
	}
	waitFor(t, "the block written while the follower was down", func() bool { return strings.Contains(fc.html(), `{"late":1}`+"\n") })
}

// A transport error (unavailable) is retried; the block lands and its chip
// is green.
func TestTransportErrorIsRetried(t *testing.T) {
	c, h, fc := setup(t)
	fc.flaky = map[string]int{"retry-me": 2}
	appendf, user, text := transcriptFixture(t, c, "flaky-1")
	a := herdr.Agent{Pane: "w9:p7", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: "flaky-1"}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	fence := "```"
	appendf(user("p1", "go") + text(fence+"patch-elements selector=\"#aside\" mode=\"append\"\n<p id=\"retry-me\">x</p>\n"+fence+"\nDone."))
	waitFor(t, "the retried block", func() bool {
		for _, p := range fc.all() {
			if p.selector == "#aside" && strings.Contains(p.html, "retry-me") {
				return true
			}
		}
		return false
	})
	waitFor(t, "a green chip", func() bool { return strings.Contains(fc.html(), "✓ patch-elements</b> #aside") })
	if strings.Contains(fc.html(), "connection closed") {
		t.Fatal("a transport error became a final refusal")
	}
}

func (f *fakeCallers) setAbsent(v bool) {
	f.mu.Lock()
	f.absent = v
	f.mu.Unlock()
}

func appliedTo(fc *fakeCallers, selector, marker string) int {
	n := 0
	for _, p := range fc.all() {
		if p.selector == selector && strings.Contains(p.html, marker) {
			n++
		}
	}
	return n
}

// A block no page heard (the tab is hidden: delivered false) is not
// chipped and not given up after the transport-retry cap: it lands once
// the page listens again, and only once.
func TestUndeliveredBlockWaitsForThePage(t *testing.T) {
	c, h, fc := setup(t)
	appendf, user, text := transcriptFixture(t, c, "hidden-1")
	a := herdr.Agent{Pane: "w9:p8", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: "hidden-1"}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	fc.setAbsent(true)
	fence := "```"
	appendf(user("p1", "go") + text(fence+"patch-elements selector=\"#aside\" mode=\"append\"\n<p id=\"while-hidden\">x</p>\n"+fence+"\nDone."))
	// Far more rounds than the transport-retry cap (8) at a 20 ms tick.
	time.Sleep(400 * time.Millisecond)
	c.mu.Lock()
	s := c.sessions["A"]
	c.mu.Unlock()
	f := c.follower(s)
	f.mu.Lock()
	chips := len(f.chips)
	f.mu.Unlock()
	if chips != 0 {
		t.Fatalf("a block no page heard was chipped (%d chips)", chips)
	}
	fc.setAbsent(false)
	waitFor(t, "the block once the page listens", func() bool { return appliedTo(fc, "#aside", "while-hidden") == 1 })
	waitFor(t, "a green chip", func() bool { return strings.Contains(fc.html(), "✓ patch-elements</b> #aside") })
	time.Sleep(100 * time.Millisecond)
	if n := appliedTo(fc, "#aside", "while-hidden"); n != 1 {
		t.Fatalf("applied %d times", n)
	}
}

// A page shown again asks "follow" with resync: the thread is rendered
// whole and pillAgent sent again, even when nothing changed, and again on
// a later round when the page's Stream is not back yet.
func TestResyncRendersTheThreadWhole(t *testing.T) {
	c, _, fc := setup(t, stranger)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: stranger.Pane}); f != nil {
		t.Fatal(f)
	}
	waitFor(t, "the thread", func() bool { return regionRenders(fc, "thread") > 0 })
	time.Sleep(60 * time.Millisecond)
	fc.setAbsent(true)
	if _, f := call(t, c, "A:2", Request{Action: "follow", Pane: stranger.Pane, Resync: true}); f != nil {
		t.Fatalf("resync answered %v", f)
	}
	time.Sleep(100 * time.Millisecond)
	fc.reset()
	fc.setAbsent(false)
	waitFor(t, "the thread rendered whole", func() bool { return regionRenders(fc, "thread") == 1 })
	waitFor(t, "pillAgent again", func() bool { return strings.Contains(fc.html(), `"pillAgent"`) })
	time.Sleep(100 * time.Millisecond)
	if n := regionRenders(fc, "thread"); n != 1 {
		t.Fatalf("rendered %d times after one resync", n)
	}
}

// Picking another pane and coming back keeps what the console knew: a
// block that closed meanwhile in a turn the console sent is applied; one
// in a turn typed elsewhere is earlier; one applied before is not again.
func TestPaneSwitchKeepsTheConsolesBlocks(t *testing.T) {
	c, h, fc := setup(t, stranger)
	appendf, user, text := transcriptFixture(t, c, "switch-1")
	a := herdr.Agent{Pane: "w9:p9", Workspace: "w9", Name: "worker", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: "switch-1"}
	h.Put(a)
	fence := "```"
	block := func(id string) string {
		return fence + "patch-elements selector=\"#aside\" mode=\"append\"\n<p id=\"" + id + "\">x</p>\n" + fence + "\n"
	}
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	c.mu.Lock()
	s := c.sessions["A"]
	c.mu.Unlock()
	appendf(user("p0", "first") + text(block("before-switch")+"Done."))
	waitFor(t, "the first block", func() bool { return appliedTo(fc, "#aside", "before-switch") == 1 })
	c.follower(s).expect("add a note to #aside")

	if _, f := call(t, c, "A:2", Request{Action: "select", Pane: stranger.Pane}); f != nil {
		t.Fatal(f)
	}
	appendf(user("p1", "add a note to #aside") + text(block("asked-for")+"Done."))
	appendf(user("p2", "typed in the terminal") + text(block("typed-elsewhere")+"Done."))
	time.Sleep(60 * time.Millisecond)

	if _, f := call(t, c, "A:3", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	waitFor(t, "the block the console asked for", func() bool { return appliedTo(fc, "#aside", "asked-for") == 1 })
	time.Sleep(100 * time.Millisecond)
	if appliedTo(fc, "#aside", "typed-elsewhere") != 0 {
		t.Fatal("a block of a turn typed elsewhere was applied")
	}
	if appliedTo(fc, "#aside", "before-switch") != 1 {
		t.Fatal("a block applied before the switch was applied again")
	}
	f := c.follower(s)
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, v := range f.earlier {
		if v {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d blocks earlier, want the one typed elsewhere", n)
	}
}

// A block core refuses because its Patch Region is shared (RT Station's
// shared-region rule) is not the agent's fault: the turn shows one quiet
// line for all such blocks, and no red chip.
func TestSharedRegionRefusalIsOneQuietLine(t *testing.T) {
	c, h, fc := setup(t)
	shared := "Patch Region aside is shared; it takes page patches (folds) only. A session or user patch belongs in a personal Patch Region"
	fc.refuse = map[string]string{"card-a": shared, "card-b": shared}
	session := "shared-1"
	proj := filepath.Join(c.cfg.ClaudeProjects, "-tmp")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(proj, session+".jsonl")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	fence := "```"
	line := func(v any) string { raw, _ := json.Marshal(v); return string(raw) + "\n" }
	a := herdr.Agent{Pane: "w9:p6", Workspace: "w9", Name: "carder", Kind: "claude", Status: "idle", Cwd: "/tmp", SessionID: session}
	h.Put(a)
	if _, f := call(t, c, "A:1", Request{Action: "select", Pane: a.Pane}); f != nil {
		t.Fatal(f)
	}
	time.Sleep(100 * time.Millisecond)
	reply := fence + "patch-elements selector=\"#aside\" mode=\"append\"\n<article id=\"card-a\">a</article>\n" + fence + "\n" +
		fence + "patch-elements selector=\"#aside\" mode=\"append\"\n<article id=\"card-b\">b</article>\n" + fence + "\nDone."
	body := line(map[string]any{"type": "user", "uuid": "u1", "promptId": "p1", "message": map[string]any{"role": "user", "content": "cards"}}) +
		line(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": reply}}}})
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(body)
	_ = f.Close()
	time.Sleep(300 * time.Millisecond)
	var last string
	for _, p := range fc.all() {
		if strings.Contains(p.html, "Cards for the page are off") {
			last = p.html
		}
	}
	if last == "" {
		t.Fatal("no quiet line for the shared-region refusals")
	}
	if n := strings.Count(last, "Cards for the page are off"); n != 1 {
		t.Fatalf("the quiet line shows %d times in one turn", n)
	}
	if strings.Contains(last, "data-err") {
		t.Fatal("a shared-region refusal shows as a red chip")
	}
}
