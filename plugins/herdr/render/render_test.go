package render

import (
	"regexp"
	"strings"
	"testing"
)

var (
	scriptTag = regexp.MustCompile(`(?i)<script`)
	onAttr    = regexp.MustCompile(`(?i)<[^>]*\son[a-z]+\s*=`)
	classes   = regexp.MustCompile(`class="([^"]*)"`)
	quoted    = regexp.MustCompile(`"[^"]*"`)
)

// agreed are the class names the shell styles (part C).
var agreed = map[string]bool{"hc-picker": true, "hc-group": true, "hc-row": true, "hc-ico": true, "hc-name": true, "hc-folder": true, "hc-status": true, "hc-dot": true, "hc-crew": true, "hc-new": true, "hc-form": true, "hc-seg": true, "hc-thread": true, "hc-q": true, "hc-a": true, "hc-tools": true, "hc-chip": true, "hc-confirm": true, "hc-dialog": true, "hc-error": true, "hc-head": true, "hc-search": true, "hc-find": true, "hc-list": true, "hc-empty": true, "hc-wait": true}

func TestRenderedHTMLIsSafeAndUsesAgreedClasses(t *testing.T) {
	evil := `<script>alert(1)</script><img src=x onerror=alert(1)> **b** "q"`
	outputs := map[string]string{
		"picker": Picker([]Group{{Title: evil, Rows: []Row{{Pane: "w1:p1", Name: evil, Folder: evil, Kind: "claude", Status: "idle", Crew: true, Opened: true, Selected: true}}}}, ""),
		"form":   Form([]string{"gemini"}, []string{evil}, FormValues{Cwd: evil, Name: evil}),
		"thread": Thread(Head{Name: evil, Kind: "codex", Status: "working", Folder: evil}, []TurnView{{ID: "t1", Prompt: evil, Items: []Item{
			{Text: evil + "\n\n```\n" + evil + "\n```\n- " + evil},
			{Tools: 3},
			{Chip: &Chip{OK: true, Label: evil}},
			{Chip: &Chip{Label: evil, Reason: evil, Raw: evil}},
			{Chip: &Chip{Earlier: true, Label: evil}},
			{Pending: "patch-elements"},
		}}}, ""),
		"confirm": Confirm("w1:p1", evil, "pi", evil, nil, true),
		"keys":    Confirm("w1:p1", evil, "pi", "", []string{"1"}, false),
		"dialog":  Dialog("w1:p1", evil, evil, []string{"1", "enter", "esc"}),
		"error":   View("error", Error(evil)),
		"start":   Starting(evil, "gemini", evil),
	}
	for name, html := range outputs {
		if scriptTag.MatchString(html) {
			t.Errorf("%s has a script tag: %s", name, html)
		}
		// Attribute values are quoted and escaped: strip them, then look
		// for an attribute named on*.
		if onAttr.MatchString(quoted.ReplaceAllString(html, `""`)) {
			t.Errorf("%s has an on* attribute: %s", name, html)
		}
		for _, m := range classes.FindAllStringSubmatch(html, -1) {
			for _, c := range strings.Fields(m[1]) {
				if !agreed[c] {
					t.Errorf("%s uses class %q, which the shell does not style", name, c)
				}
			}
		}
	}
	if !strings.Contains(outputs["thread"], "<b>b</b>") || !strings.Contains(outputs["thread"], "<pre><code>") || !strings.Contains(outputs["thread"], "<li>") {
		t.Errorf("markdown did not render: %s", outputs["thread"])
	}
	if !strings.Contains(outputs["thread"], `data-ok`) || !strings.Contains(outputs["thread"], `data-err`) {
		t.Error("chips lack data-ok / data-err")
	}
	if !strings.Contains(outputs["form"], `data-pill-form="start"`) {
		t.Error("the form lacks data-pill-form")
	}
	if !strings.Contains(outputs["picker"], `aria-selected="true"`) {
		t.Error("the selected row is not marked")
	}
}

func TestShortPath(t *testing.T) {
	HomeDir = "/home/n"
	for in, want := range map[string]string{
		"/home/n/src": "~/src",
		"/home/n/src/team/docs-site/content/guides/x": "…/guides/x",
		"/tmp": "/tmp",
	} {
		if got := ShortPath(in); got != want {
			t.Errorf("ShortPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarkdownLinks(t *testing.T) {
	got := Markdown(`[x](https://a.b/c?d=1&e=2) and [y](javascript:alert(1))`)
	if !strings.Contains(got, `href="https://a.b/c?d=1&amp;e=2"`) || strings.Contains(got, `href="javascript`) {
		t.Fatalf("links = %s", got)
	}
}

func TestPickerHasSearchRowAndNewOutsideTheList(t *testing.T) {
	html := Picker([]Group{{Title: "Projects", Rows: []Row{{Pane: "w1:p1", Name: "docs-site", Folder: "/home/n/src/team/docs-site", Kind: "claude", Status: "idle", Crew: true}}}}, "doc")
	search := strings.Index(html, `class="hc-search"`)
	list := strings.Index(html, `class="hc-list"`)
	listEnd := strings.LastIndex(html, `</div><button type="button" class="hc-new"`)
	if search < 0 || list < 0 || search > list {
		t.Fatalf("the search row is not above the list: %s", html)
	}
	if listEnd < 0 {
		t.Fatalf("New session is not after (outside) the list: %s", html)
	}
	for _, want := range []string{`placeholder="Find a session or folder"`, `value="doc"`, `type="search"`, `data-search="docs-site /home/n/src/team/docs-site`} {
		if !strings.Contains(html, want) {
			t.Errorf("picker misses %s", want)
		}
	}
	// The row's parts are direct children: name, crew mark and folder are
	// siblings, not nested in the name.
	if !strings.Contains(html, `<span class="hc-name">docs-site</span><span class="hc-crew">crew</span><span class="hc-folder"`) {
		t.Errorf("row parts are not siblings: %s", html)
	}
	if empty := Picker(nil, "zz"); !strings.Contains(empty, `<p class="hc-empty">No session matches zz.</p>`) || !strings.Contains(empty, `class="hc-new"`) {
		t.Errorf("empty picker = %s", empty)
	}
}

func TestToolLineHasOneCaretAndFoldsARun(t *testing.T) {
	a := Answer(TurnView{ID: "t", Done: true, Items: []Item{{Tools: 1}, {Tools: 2}, {Text: "ok"}, {Tools: 1}}})
	// The caret is the shell's ::before; the markup carries none.
	if strings.Contains(a, "▸") {
		t.Fatalf("the tool line carries its own caret: %s", a)
	}
	if strings.Count(a, `class="hc-tools"`) != 2 || !strings.Contains(a, ">ran 3 tools<") || !strings.Contains(a, ">ran 1 tool<") {
		t.Fatalf("tool lines = %s", a)
	}
	if w := Answer(TurnView{ID: "t"}); !strings.Contains(w, `class="hc-wait"`) || strings.Contains(w, "hc-tools") {
		t.Fatalf("the waiting answer looks like a tool line: %s", w)
	}
}

func TestCardsOffIsOneLineATurn(t *testing.T) {
	off := &Chip{Off: true, Label: "patch-elements #aside · append"}
	html := Answer(TurnView{Items: []Item{{Chip: off}, {Text: "between"}, {Chip: off}}})
	if n := strings.Count(html, "Cards for the page are off"); n != 1 {
		t.Fatalf("got %d quiet lines, want 1", n)
	}
	if strings.Contains(html, "data-err") {
		t.Fatal("an off chip renders as an error")
	}
}
