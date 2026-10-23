package render

import (
	"regexp"
	"strings"
	"testing"
)

// dataAttr finds data-* attribute names; datastarAttrs is core's list of
// the Datastar attributes a caller patch may not carry (it runs them as code).
var (
	dataAttr      = regexp.MustCompile(`\sdata-([a-zA-Z0-9:_.-]+)`)
	datastarAttrs = map[string]bool{"attr": true, "bind": true, "class": true, "computed": true, "effect": true, "indicator": true, "init": true, "json-signals": true, "on": true, "ref": true, "show": true, "signals": true, "style": true, "text": true, "preserve-attr": true, "animate": true, "custom-validity": true, "persist": true, "query-string": true, "replace-url": true, "scroll-into-view": true, "view-transition": true, "match-media": true, "chart": true, "cmd": true, "code": true, "demo": true, "feed": true, "observe-resize": true, "resize-pane": true, "snap-drag": true, "theme-switch": true, "toast": true}
)

// panelClasses are the classes the shell styles for the Agents panel.
var panelClasses = map[string]bool{"ha-panel": true, "ha-tools": true, "ha-find": true, "ha-new": true, "ha-group": true, "ha-group-h": true, "ha-count": true, "ha-row": true, "ha-ico": true, "ha-name": true, "ha-crew": true, "ha-here": true, "ha-dot": true, "ha-close": true, "ha-empty": true, "ha-error": true}

func panelFixture() AgentsList {
	return AgentsList{Groups: []AgentGroup{
		{ID: "w1", Title: "Projects", Rows: []AgentRow{
			{Pane: "w1:p1", Name: "reviewer", Folder: "/home/n/src/reviews", Kind: "claude", Status: "working", Crew: true},
			{Pane: "w1:p2", Name: "docs-site", Folder: "/home/n/x", Kind: "codex", Status: "blocked", Crew: true, Here: true},
		}},
		{ID: "w9", Title: "Console", Rows: []AgentRow{
			{Pane: "w9:p1", Name: "claude-notes", Folder: "/tmp/n", Kind: "pi", Status: "done", Opened: true},
		}},
		{ID: "w3", Title: "Empty"},
	}}
}

func TestAgentsPanelGroupsCountsCurrentAndClose(t *testing.T) {
	HomeDir = "/home/n"
	html := AgentsPanel(panelFixture(), "ex")
	for _, want := range []string{
		`<div class="ha-panel"><div class="ha-tools"><input class="ha-find" data-ha-find type="search"`,
		`value="ex"`,
		`<button type="button" class="ha-new" data-pill-call="{&#34;action&#34;:&#34;new&#34;}">＋ New</button>`,
		`<div id="ha-list" data-sessions="3">`,
		`<summary class="ha-group-h"><span>Projects</span><span class="ha-count">2</span></summary>`,
		`<summary class="ha-group-h"><span>Console</span><span class="ha-count">1</span></summary>`,
		`data-pill-call="{&#34;action&#34;:&#34;select&#34;,&#34;pane&#34;:&#34;w1:p2&#34;}"`,
		`<span class="ha-name">docs-site</span><span class="ha-crew">crew</span><span class="ha-here">· in the pill</span><span class="ha-dot" data-status="blocked"`,
		`<span class="ha-ico" data-kind="codex"`,
		`data-search="docs-site /home/n/x ~/x"`,
		`data-pill-call="{&#34;action&#34;:&#34;close&#34;,&#34;pane&#34;:&#34;w9:p1&#34;}"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("panel misses %s\n%s", want, html)
		}
	}
	// Groups in the given (herdr's) order; an empty group is left out.
	if a, c := strings.Index(html, ">Projects<"), strings.Index(html, ">Console<"); a < 0 || c < a {
		t.Error("groups out of order")
	}
	if strings.Contains(html, ">Empty<") {
		t.Error("an empty group is shown")
	}
	if n := strings.Count(html, `aria-current="true"`); n != 1 {
		t.Errorf("aria-current on %d rows, want 1", n)
	}
	if n := strings.Count(html, `class="ha-here"`); n != 1 {
		t.Errorf("in the pill on %d rows, want 1", n)
	}
	// Close only on the row the console opened.
	if n := strings.Count(html, `class="ha-close"`); n != 1 {
		t.Errorf("close on %d rows, want 1", n)
	}
	if strings.Contains(html, `&#34;close&#34;,&#34;pane&#34;:&#34;w1:`) {
		t.Error("a session the console did not open has a close button")
	}
	// The list alone is the tail of the panel: a live update replaces it.
	if !strings.Contains(html, AgentsListHTML(panelFixture())) {
		t.Error("the panel does not embed its list")
	}
}

func TestAgentsPanelIsSafe(t *testing.T) {
	evil := `<script>alert(1)</script><img src=x onerror=alert(1)> "q"`
	l := AgentsList{Error: evil, Groups: []AgentGroup{{ID: evil, Title: evil, Rows: []AgentRow{{Pane: evil, Name: evil, Folder: evil, Kind: evil, Status: evil, Crew: true, Opened: true, Here: true}}}}}
	for name, html := range map[string]string{"panel": AgentsPanel(l, evil), "empty": AgentsPanel(AgentsList{}, ""), "more": AgentsListHTML(AgentsList{More: 3})} {
		if scriptTag.MatchString(html) {
			t.Errorf("%s has a script tag: %s", name, html)
		}
		if onAttr.MatchString(quoted.ReplaceAllString(html, `""`)) {
			t.Errorf("%s has an on* attribute: %s", name, html)
		}
		// core refuses a caller patch that carries a Datastar attribute
		// (the same rule core applies to every caller patch)
		for _, m := range dataAttr.FindAllStringSubmatch(quoted.ReplaceAllString(html, `""`), -1) {
			if rest := m[1]; datastarAttrs[rest] || strings.HasPrefix(rest, "on-") || strings.HasPrefix(rest, "star-") || strings.ContainsAny(rest, ":") || strings.Contains(rest, "__") {
				t.Errorf("%s carries the Datastar attribute data-%s, which core refuses in a caller patch", name, rest)
			}
		}
		for _, m := range classes.FindAllStringSubmatch(html, -1) {
			for _, c := range strings.Fields(m[1]) {
				if !panelClasses[c] {
					t.Errorf("%s uses class %q, which the shell does not style", name, c)
				}
			}
		}
	}
	html := AgentsPanel(l, evil)
	if !strings.Contains(html, `data-status="unknown"`) || !strings.Contains(html, `data-kind="other"`) {
		t.Errorf("unknown status or kind leaked: %s", html)
	}
	if !strings.Contains(html, `<p class="ha-error" role="alert">&lt;script&gt;`) {
		t.Errorf("error line = %s", html)
	}
	if e := AgentsPanel(AgentsList{}, ""); !strings.Contains(e, `<p class="ha-empty">No herdr sessions are running.</p>`) || !strings.Contains(e, `data-sessions="0"`) {
		t.Errorf("empty panel = %s", e)
	}
	if m := AgentsListHTML(AgentsList{More: 3}); !strings.Contains(m, `data-sessions="3"`) || !strings.Contains(m, "3 more not shown") {
		t.Errorf("more = %s", m)
	}
}
