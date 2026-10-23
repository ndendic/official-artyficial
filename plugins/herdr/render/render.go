// Package render writes the console's HTML: the session picker, the
// new-session form, the thread with its patch chips, the confirm bar, the
// dialog card and errors. It emits exactly the .hc-* classes the shell
// styles, and data-pill-call / data-pill-form for every control. It never
// writes <script> or an on* attribute, and it escapes every piece of agent
// or user text.
package render

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html"
	"os"
	"regexp"
	"strings"
)

// Root is the element every full view replaces (inner) inside the Patch
// Region console.
const (
	RegionSelector = "#pill-console"
	ViewID         = "hc-view"
	ErrorID        = "hc-error"
	ConfirmID      = "hc-confirm"
	DialogID       = "hc-dialog"
	ThreadID       = "hc-thread"
)

func esc(s string) string { return html.EscapeString(s) }

// Call is the data-pill-call attribute for an action.
func Call(action string, fields ...any) string {
	m := map[string]any{"action": action}
	for i := 0; i+1 < len(fields); i += 2 {
		m[fields[i].(string)] = fields[i+1]
	}
	raw, _ := json.Marshal(m)
	return ` data-pill-call="` + esc(string(raw)) + `"`
}

// Key turns a turn id into a short, stable, id-safe token.
func Key(id string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return fmt.Sprintf("%012x", h.Sum64())[:12]
}

// Row is one session in the picker.
type Row struct {
	Pane, Name, Folder, Kind, Status string
	Crew, Opened, Selected, Taught   bool
}

// Group is one heading of the picker.
type Group struct {
	Title string
	Rows  []Row
}

// View wraps a view's body in the root element, with an empty error slot.
func View(name, body string) string {
	return `<div id="` + ViewID + `" data-view="` + esc(name) + `">` + body + emptyError() + `</div>`
}

func emptyError() string { return `<div id="` + ErrorID + `" class="hc-error" hidden></div>` }

// Error is the error line (outer on #hc-error).
func Error(msg string) string {
	return `<div id="` + ErrorID + `" class="hc-error" role="alert">` + esc(msg) + `</div>`
}

// ClearError is the empty error slot (outer on #hc-error).
func ClearError() string { return emptyError() }

// Picker renders the session picker: a search row at the top ("Find a
// session or folder"; the shell filters the rows by their data-search as
// you type there or after an "@" in the pill), the rows grouped by herdr
// workspace in a list that scrolls, and "New session…" at the foot, outside
// the list, so it stays in view however long the list is.
func Picker(groups []Group, filter string) string {
	var b strings.Builder
	b.WriteString(`<div class="hc-picker" role="listbox" aria-label="herdr sessions">`)
	b.WriteString(`<label class="hc-search"><span aria-hidden="true">⌕</span><input class="hc-find" type="search" autocomplete="off" spellcheck="false" placeholder="Find a session or folder" aria-label="Find a session or folder" value="` + esc(filter) + `"></label>`)
	b.WriteString(`<div class="hc-list">`)
	empty := true
	for _, g := range groups {
		if len(g.Rows) == 0 {
			continue
		}
		empty = false
		b.WriteString(`<div class="hc-group">` + esc(g.Title) + `</div>`)
		for _, r := range g.Rows {
			b.WriteString(row(r))
		}
	}
	if empty {
		msg := "No herdr sessions are running."
		if filter != "" {
			msg = "No session matches " + filter + "."
		}
		b.WriteString(`<p class="hc-empty">` + esc(msg) + `</p>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<button type="button" class="hc-new"` + Call("new") + `>＋ New session…</button>`)
	b.WriteString(`</div>`)
	return b.String()
}

// row is one picker row. Its parts are direct children (icon, name, crew
// mark, folder, status, tools): the shell lays them on a grid.
func row(r Row) string {
	var b strings.Builder
	sel := ` aria-selected="false"`
	if r.Selected {
		sel = ` aria-selected="true"`
	}
	b.WriteString(`<div class="hc-row" role="option"` + sel + ` data-search="` + esc(strings.ToLower(r.Name+" "+r.Folder+" "+ShortPath(r.Folder))) + `"` + Call("select", "pane", r.Pane) + ` tabindex="0">`)
	b.WriteString(Icon(r.Kind))
	b.WriteString(`<span class="hc-name">` + esc(r.Name) + `</span>`)
	if r.Crew {
		b.WriteString(`<span class="hc-crew">crew</span>`)
	}
	b.WriteString(`<span class="hc-folder" title="` + esc(r.Folder) + `">` + esc(ShortPath(r.Folder)) + `</span>`)
	b.WriteString(`<span class="hc-status">` + Dot(r.Status) + esc(statusWord(r.Status)) + `</span>`)
	if r.Opened {
		b.WriteString(`<span class="hc-tools">`)
		if !r.Taught {
			b.WriteString(`<button type="button" title="Send the patch-block primer once"` + Call("teach", "pane", r.Pane) + `>Teach patch blocks</button>`)
		}
		b.WriteString(`<button type="button" title="Close this session"` + Call("close", "pane", r.Pane) + `>Close</button></span>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// FormValues are a new-session form's values, kept when it is shown again
// with an error.
type FormValues struct {
	Kind, OtherKind, Cwd, Name string
}

// Form renders the new-session form.
func Form(otherKinds, recent []string, v FormValues) string {
	if v.Kind == "" {
		v.Kind = "claude"
	}
	var b strings.Builder
	b.WriteString(`<form class="hc-form" data-pill-form="start" autocomplete="off">`)
	b.WriteString(`<fieldset class="hc-seg"><legend>Agent</legend>`)
	for _, k := range []struct{ kind, label string }{{"claude", "Claude"}, {"pi", "Pi"}, {"codex", "Codex"}, {"more", "More"}} {
		checked := ""
		if v.Kind == k.kind {
			checked = " checked"
		}
		icon := Icon(k.kind)
		b.WriteString(`<label><input type="radio" name="kind" value="` + k.kind + `"` + checked + `>` + icon + esc(k.label) + `</label>`)
	}
	b.WriteString(`<select name="other_kind" aria-label="Other agent">`)
	for _, k := range otherKinds {
		sel := ""
		if k == v.OtherKind {
			sel = " selected"
		}
		b.WriteString(`<option value="` + esc(k) + `"` + sel + `>` + esc(k) + `</option>`)
	}
	b.WriteString(`</select></fieldset>`)
	b.WriteString(`<label>Folder<input name="cwd" required list="hc-cwds" placeholder="~/src/…" value="` + esc(v.Cwd) + `"></label>`)
	b.WriteString(`<datalist id="hc-cwds">`)
	for _, r := range recent {
		b.WriteString(`<option value="` + esc(r) + `"></option>`)
	}
	b.WriteString(`</datalist>`)
	b.WriteString(`<label>Name (optional)<input name="name" pattern="[a-z][a-z0-9_\-]{0,31}" placeholder="kind-folder" value="` + esc(v.Name) + `"></label>`)
	b.WriteString(`<div><button type="button"` + Call("open") + `>Cancel</button><button type="submit">Start session</button></div>`)
	b.WriteString(`</form>`)
	return b.String()
}

// Head is the session line above the thread.
type Head struct {
	Pane, Name, Kind, Status, Folder string
	Crew                             bool
	Source                           string // "transcript" or "screen"
}

// HeadHTML renders the thread's head line.
func HeadHTML(h Head) string {
	var b strings.Builder
	b.WriteString(`<div class="hc-head" id="hc-head">` + Icon(h.Kind) + `<b class="hc-name">` + esc(h.Name) + `</b>`)
	if h.Crew {
		b.WriteString(`<span class="hc-crew">crew</span>`)
	}
	b.WriteString(`<span class="hc-status">` + Dot(h.Status) + esc(statusWord(h.Status)) + `</span>`)
	b.WriteString(`<span class="hc-folder">` + esc(ShortPath(h.Folder)) + `</span>`)
	if h.Source == "screen" {
		b.WriteString(`<span class="hc-folder" title="This agent's transcript is not readable; the reply comes from its screen">screen</span>`)
	}
	b.WriteString(`<button type="button"` + Call("open") + `>Sessions</button>`)
	b.WriteString(`</div>`)
	return b.String()
}

// Item is one piece of an answer, in order.
type Item struct {
	Text  string // Markdown
	Tools int
	Chip  *Chip
	// Pending is a patch block whose closing fence has not arrived.
	Pending string
}

// Chip is a patch block's outcome.
type Chip struct {
	OK      bool
	Earlier bool // from before the console followed the session: not applied
	Waiting bool // closed, being applied
	// Off: core refused it because its Patch Region is shared, and a
	// session patch belongs in a personal one. Shown once a turn, quietly:
	// the page has no personal region for cards yet, which is not an error
	// of the agent's.
	Off    bool
	Label  string
	Reason string
	Raw    string
}

// TurnView is one turn of the thread.
type TurnView struct {
	ID     string
	Prompt string
	Items  []Item
	Done   bool
}

// Thread renders the head and the turns.
func Thread(h Head, turns []TurnView, note string) string {
	var b strings.Builder
	b.WriteString(HeadHTML(h))
	b.WriteString(`<div class="hc-thread" id="` + ThreadID + `" aria-live="polite">`)
	if len(turns) == 0 {
		msg := "No conversation yet. Write a message in the pill."
		if note != "" {
			msg = note
		}
		b.WriteString(`<div class="hc-a" id="hc-empty"><p>` + esc(msg) + `</p></div>`)
	}
	for _, t := range turns {
		b.WriteString(Question(t))
		b.WriteString(Answer(t))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// Question is a turn's prompt bubble.
func Question(t TurnView) string {
	return `<div class="hc-q" id="hc-q-` + Key(t.ID) + `">` + esc(t.Prompt) + `</div>`
}

// AnswerID is the stable id of a turn's answer, for streaming it (outer).
func AnswerID(turnID string) string { return "hc-a-" + Key(turnID) }

// Answer renders one answer bubble with a stable id.
func Answer(t TurnView) string {
	var b strings.Builder
	state := "working"
	if t.Done {
		state = "done"
	}
	b.WriteString(`<div class="hc-a" id="` + AnswerID(t.ID) + `" data-state="` + state + `">`)
	if len(t.Items) == 0 && !t.Done {
		b.WriteString(`<p class="hc-wait">…</p>`)
	}
	offShown := false
	for i := 0; i < len(t.Items); i++ {
		it := t.Items[i]
		switch {
		case it.Chip != nil && it.Chip.Off:
			if !offShown {
				offShown = true
				b.WriteString(CardsOffLine)
			}
		case it.Tools > 0:
			// One quiet line for a run of tool calls. Its caret is the
			// shell's (.hc-tools::before), so the text carries none.
			n := it.Tools
			for i+1 < len(t.Items) && t.Items[i+1].Tools > 0 {
				i++
				n += t.Items[i].Tools
			}
			b.WriteString(ToolLine(n))
		case it.Chip != nil:
			b.WriteString(ChipHTML(*it.Chip))
		case it.Pending != "":
			b.WriteString(`<div class="hc-chip">` + esc(it.Pending) + ` · writing…</div>`)
		case it.Text != "":
			b.WriteString(Markdown(it.Text))
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

// CardsOffLine is the one quiet line a turn shows for its blocks that core
// refused because their region is shared (a card for the inspector).
const CardsOffLine = `<div class="hc-tools hc-off">Cards for the page are off for now: this page has no personal place for them yet.</div>`

// ToolLine is the folded line for n tool calls ("ran 3 tools").
func ToolLine(n int) string {
	word := "tools"
	if n == 1 {
		word = "tool"
	}
	return fmt.Sprintf(`<div class="hc-tools">ran %d %s</div>`, n, word)
}

// ChipHTML renders a patch chip.
func ChipHTML(c Chip) string {
	switch {
	case c.Earlier:
		return `<div class="hc-chip" title="From before the console followed this session: not applied">` + esc(c.Label) + ` · earlier, not applied</div>`
	case c.Waiting:
		return `<div class="hc-chip">` + esc(c.Label) + ` · applying…</div>`
	case c.OK:
		kind, rest := chipParts(c.Label)
		return `<div class="hc-chip" data-ok><b>✓ ` + esc(kind) + `</b>` + rest + `</div>`
	}
	kind, rest := chipParts(c.Label)
	return `<div class="hc-chip" data-err><b>✗ ` + esc(kind) + ` refused</b>` + rest + ` · ` + esc(c.Reason) +
		`<details><summary>block as sent</summary><pre>` + esc(c.Raw) + `</pre></details></div>`
}

// chipParts splits a block's summary ("patch-elements #aside · append")
// into its kind, set in bold as in the design, and the rest (escaped, with
// its leading space).
func chipParts(label string) (string, string) {
	kind, rest, ok := strings.Cut(label, " ")
	if !ok {
		return label, ""
	}
	return kind, " " + esc(rest)
}

// Confirm renders the red confirm bar for a send to a crew session or a
// session the console did not open.
func Confirm(pane, name, kind, text string, keys []string, crew bool) string {
	what := "a session the console did not open"
	if crew {
		what = "crew"
	}
	var b strings.Builder
	b.WriteString(`<div class="hc-confirm" id="` + ConfirmID + `" role="alertdialog">`)
	b.WriteString(Icon(kind))
	if len(keys) > 0 {
		b.WriteString(`<span>Press <b>` + esc(strings.Join(keys, " ")) + `</b> in <b>` + esc(name) + `</b> (` + esc(what) + `)?</span>`)
		b.WriteString(`<button type="button"` + Call("dismiss") + `>Cancel</button>`)
		b.WriteString(`<button type="button" data-variant="danger"` + Call("keys", "pane", pane, "keys", keys, "confirm", true) + `>Send ↵</button>`)
	} else {
		b.WriteString(`<span>Send to <b>` + esc(name) + `</b> (` + esc(what) + `)? <q>` + esc(clip(text, 160)) + `</q></span>`)
		b.WriteString(`<button type="button"` + Call("dismiss") + `>Cancel</button>`)
		b.WriteString(`<button type="button" data-variant="danger"` + Call("send", "pane", pane, "text", text, "confirm", true) + `>Send ↵</button>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// Dialog renders the card for an agent waiting on an approval or question
// dialog: its text and the keys it offers. The console never answers it.
func Dialog(pane, name, screen string, keys []string) string {
	var b strings.Builder
	b.WriteString(`<div class="hc-dialog" id="` + DialogID + `" role="group" aria-label="` + esc(name) + ` waits on a dialog">`)
	b.WriteString(`<p><b>` + esc(name) + `</b> is waiting on a dialog. Choose a key; the console never answers for you.</p>`)
	b.WriteString(`<pre>` + esc(screen) + `</pre><div>`)
	for _, k := range keys {
		b.WriteString(`<button type="button"` + Call("keys", "pane", pane, "keys", []string{k}) + `>` + esc(keyLabel(k)) + `</button>`)
	}
	b.WriteString(`</div></div>`)
	return b.String()
}

// NoDialog is the empty dialog slot (outer on #hc-dialog).
func NoDialog() string { return `<div id="` + DialogID + `" hidden></div>` }

func keyLabel(k string) string {
	switch k {
	case "enter":
		return "Enter"
	case "esc":
		return "Esc"
	case "tab":
		return "Tab"
	case "up":
		return "↑"
	case "down":
		return "↓"
	}
	return k
}

// Starting is the line shown while a new session starts.
func Starting(name, kind, cwd string) string {
	return `<div class="hc-thread" id="` + ThreadID + `"><div class="hc-a">` + Icon(kind) + `<p>Starting <b>` + esc(name) + `</b> in ` + esc(ShortPath(cwd)) + `…</p></div></div>`
}

// Dot is a status dot.
func Dot(status string) string {
	return `<span class="hc-dot" data-status="` + esc(statusWord(status)) + `"></span>`
}

func statusWord(s string) string {
	switch s {
	case "idle", "working", "blocked", "done":
		return s
	}
	return "unknown"
}

// HomeDir is the home directory ShortPath writes as ~ (the console sets it;
// a Plugin's environment has no HOME).
var HomeDir, _ = os.UserHomeDir()

// ShortPath writes a folder the way the picker shows it: ~ for home, and
// "…/" plus the last two elements when long.
func ShortPath(p string) string {
	if p == "" {
		return ""
	}
	if HomeDir != "" && strings.HasPrefix(p, HomeDir) {
		p = "~" + strings.TrimPrefix(p, HomeDir)
	}
	if len(p) <= 32 {
		return p
	}
	parts := strings.Split(strings.TrimRight(p, "/"), "/")
	if len(parts) > 2 {
		return "…/" + strings.Join(parts[len(parts)-2:], "/")
	}
	return p
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

var (
	inlineCode = regexp.MustCompile("`([^`\n]+)`")
	bold       = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	link       = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)
)

// Markdown renders a small, safe subset: paragraphs, fenced code, bullet
// lists, inline code, bold and http(s) links. Everything is escaped first.
func Markdown(md string) string {
	var b strings.Builder
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var para []string
	inList := false
	flushPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + inline(strings.Join(para, "\n")) + "</p>")
			para = nil
		}
	}
	closeList := func() {
		if inList {
			b.WriteString("</ul>")
			inList = false
		}
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			flushPara()
			closeList()
			fence := t[:3]
			var code []string
			j := i + 1
			for ; j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), fence); j++ {
				code = append(code, lines[j])
			}
			b.WriteString("<pre><code>" + esc(strings.Join(code, "\n")) + "</code></pre>")
			i = j
		case t == "":
			flushPara()
			closeList()
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			flushPara()
			if !inList {
				b.WriteString("<ul>")
				inList = true
			}
			b.WriteString("<li>" + inline(t[2:]) + "</li>")
		case strings.HasPrefix(t, "#"):
			flushPara()
			closeList()
			b.WriteString("<p><b>" + inline(strings.TrimSpace(strings.TrimLeft(t, "#"))) + "</b></p>")
		default:
			closeList()
			para = append(para, l)
		}
	}
	flushPara()
	closeList()
	return b.String()
}

func inline(s string) string {
	s = esc(s)
	s = inlineCode.ReplaceAllString(s, "<code>$1</code>")
	s = bold.ReplaceAllString(s, "<b>$1</b>")
	s = link.ReplaceAllString(s, `<a href="$2" rel="noopener noreferrer" target="_blank">$1</a>`)
	return strings.ReplaceAll(s, "\n", "<br>")
}
