package render

import (
	"strconv"
	"strings"
)

// The Agents panel lives in the sidebar, in the personal Patch Region
// agents (element #agents-panel). A full render replaces the region's root
// (inner); a live update replaces only the list (#ha-list, outer), so the
// Find field, its text and its focus are never touched while a person
// types in it.
const (
	AgentsRegion   = "agents"
	AgentsSelector = "#agents-panel"
	AgentsListID   = "ha-list"
)

// AgentRow is one session in the Agents panel.
type AgentRow struct {
	Pane, Name, Folder, Kind, Status string
	// Crew marks a crew session; Opened one the console opened (it gets
	// the close button); Here the session attached to the pill.
	Crew, Opened, Here bool
}

// AgentGroup is one herdr workspace of the panel.
type AgentGroup struct {
	ID, Title string
	Rows      []AgentRow
}

// AgentsList is the panel's list: the groups, or the empty line, with an
// error line above them when herdr could not be read. More is how many
// sessions were left out to keep the patch small.
type AgentsList struct {
	Groups []AgentGroup
	Error  string
	More   int
}

// Sessions counts the rows of the list.
func (l AgentsList) Sessions() int {
	n := l.More
	for _, g := range l.Groups {
		n += len(g.Rows)
	}
	return n
}

// AgentsPanel renders the whole panel: the tools row (the Find field,
// which the shell filters by the rows' data-search, and "＋ New") above the
// list. filter is the Find field's text the shell had, if it sent one.
func AgentsPanel(l AgentsList, filter string) string {
	var b strings.Builder
	b.WriteString(`<div class="ha-panel">`)
	b.WriteString(`<div class="ha-tools">`)
	b.WriteString(`<input class="ha-find" data-ha-find type="search" autocomplete="off" spellcheck="false" placeholder="Find" aria-label="Find a session or folder" value="` + esc(filter) + `">`)
	b.WriteString(`<button type="button" class="ha-new"` + Call("new") + `>＋ New</button>`)
	b.WriteString(`</div>`)
	b.WriteString(AgentsListHTML(l))
	b.WriteString(`</div>`)
	return b.String()
}

// AgentsListHTML renders the list alone (outer on #ha-list). Its
// data-sessions is the number of live sessions, for the panel's header.
func AgentsListHTML(l AgentsList) string {
	var b strings.Builder
	b.WriteString(`<div id="` + AgentsListID + `" data-sessions="` + strconv.Itoa(l.Sessions()) + `">`)
	if l.Error != "" {
		b.WriteString(`<p class="ha-error" role="alert">` + esc(l.Error) + `</p>`)
	}
	empty := true
	for _, g := range l.Groups {
		if len(g.Rows) == 0 {
			continue
		}
		empty = false
		// A group opens and closes in the browser. A caller patch may not
		// carry Datastar attributes (data-preserve-attr among them), so every
		// render sends the group open and the shell puts back the groups the
		// reader folded after each re-render.
		b.WriteString(`<details class="ha-group" id="ha-g-` + Key(g.ID) + `" data-ws="` + esc(g.ID) + `" open>`)
		b.WriteString(`<summary class="ha-group-h"><span>` + esc(g.Title) + `</span><span class="ha-count">` + strconv.Itoa(len(g.Rows)) + `</span></summary>`)
		for _, r := range g.Rows {
			b.WriteString(agentRow(r))
		}
		b.WriteString(`</details>`)
	}
	if empty && l.Error == "" {
		b.WriteString(`<p class="ha-empty">No herdr sessions are running.</p>`)
	}
	if l.More > 0 {
		b.WriteString(`<p class="ha-empty">` + strconv.Itoa(l.More) + ` more not shown: find them in the picker (@ in the pill).</p>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// agentRow is one row: a focusable button (role, tabindex) that attaches
// the session to the pill, as the picker's rows are. Its parts are direct
// children: icon, name, crew mark, "· in the pill", status dot, close.
func agentRow(r AgentRow) string {
	var b strings.Builder
	cur := ""
	if r.Here {
		cur = ` aria-current="true"`
	}
	status := statusWord(r.Status)
	b.WriteString(`<div class="ha-row" role="button" tabindex="0"` + cur + ` data-pane="` + esc(r.Pane) + `" data-search="` + esc(strings.ToLower(r.Name+" "+r.Folder+" "+ShortPath(r.Folder))) + `" title="` + esc(ShortPath(r.Folder)) + `"` + Call("select", "pane", r.Pane) + `>`)
	b.WriteString(PanelIcon(r.Kind))
	b.WriteString(`<span class="ha-name">` + esc(r.Name) + `</span>`)
	if r.Crew {
		b.WriteString(`<span class="ha-crew">crew</span>`)
	}
	if r.Here {
		b.WriteString(`<span class="ha-here">· in the pill</span>`)
	}
	b.WriteString(`<span class="ha-dot" data-status="` + status + `" role="img" aria-label="` + status + `"></span>`)
	if r.Opened {
		b.WriteString(`<button type="button" class="ha-close" title="Close this session" aria-label="Close ` + esc(r.Name) + `"` + Call("close", "pane", r.Pane) + `>✕</button>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}
