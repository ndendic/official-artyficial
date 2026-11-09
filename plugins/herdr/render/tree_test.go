package render

import (
	"encoding/json"
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden trees")

func treeJSON(t *testing.T, html string) string {
	t.Helper()
	tree, err := Tree(html)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(tree, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := "testdata/" + name + ".tree.json"
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no golden %s (run go test ./render -update): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from its golden:\n%s", name, got)
	}
}

func TestTreeShape(t *testing.T) {
	tree, err := Tree(`<!-- gone --><div class="x" hidden>a <b>b</b><!-- c --> &amp; d</div>tail`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(tree)
	want := `[{"t":"div","a":{"class":"x","hidden":""},"k":["a ",{"t":"b","k":["b"]}," \u0026 d"]},"tail"]`
	if string(raw) != want {
		t.Errorf("tree = %s\nwant   %s", raw, want)
	}
	if tree, err := Tree(""); err != nil || len(tree) != 0 {
		t.Errorf("empty html = %v, %v", tree, err)
	}
	// Table parts and svg parse as they would inside a div.
	got := treeJSON(t, `<svg viewBox="0 0 1 1"><path d="M0 0"/></svg>`)
	if !strings.Contains(got, `"viewBox"`) || !strings.Contains(got, `"path"`) {
		t.Errorf("svg lost: %s", got)
	}
	// Never nil: the reply holds [] not null.
	if tree, _ := Tree(""); tree == nil {
		t.Error("an empty tree is nil: it would marshal as null")
	}
}

func TestTreeGoldenPicker(t *testing.T) {
	html := Picker([]Group{
		{Title: "Crew", Rows: []Row{{Pane: "w1:p1", Name: "reviewer", Folder: "/home/x/proj", Kind: "claude", Status: "idle", Crew: true}}},
		{Title: "Opened from the console", Rows: []Row{{Pane: "w2:p1", Name: "pi-a", Folder: "/home/x/b", Kind: "pi", Status: "working", Opened: true, Selected: true}}},
	}, "")
	golden(t, "picker", treeJSON(t, html))
}

func TestTreeGoldenConfirm(t *testing.T) {
	golden(t, "confirm", treeJSON(t, Confirm("w1:p1", "reviewer", "claude", "ship it <now>", nil, true)))
	golden(t, "confirm-keys", treeJSON(t, Confirm("w1:p1", "reviewer", "claude", "", []string{"1"}, false)))
}

func TestTreeGoldenAgentsPanel(t *testing.T) {
	l := AgentsList{Groups: []AgentGroup{
		{ID: "w1", Title: "Crew", Rows: []AgentRow{
			{Pane: "w1:p1", Name: "reviewer", Folder: "/home/x/proj", Kind: "claude", Status: "idle", Crew: true},
			{Pane: "w1:p2", Name: "docs-site", Kind: "codex", Status: "working", Crew: true, Here: true},
		}},
		{ID: "w2", Title: "Console", Rows: []AgentRow{
			{Pane: "w2:p1", Name: "pi-a", Folder: "/home/x/b", Kind: "pi", Status: "blocked", Opened: true},
			{Pane: "w2:p2", Name: "shell <1>", Kind: "other", Status: "unknown"},
		}},
	}}
	golden(t, "agents", treeJSON(t, AgentsPanel(l, "")))
	golden(t, "agents-empty", treeJSON(t, AgentsPanel(AgentsList{}, "")))
}

func TestTreeDropsNamespacedAttributes(t *testing.T) {
	got := treeJSON(t, `<svg><image xlink:href="/x.png" width="3"></image></svg>`)
	if strings.Contains(got, "xlink") || strings.Contains(got, `"href"`) || !strings.Contains(got, `"width"`) {
		t.Errorf("a namespaced attribute reached the tree: %s", got)
	}
}

func answerOf(id string, lines int) string {
	return `<div class="hc-a" id="hc-a-` + id + `">` + strings.Repeat("<p>line</p>", lines) + `</div>`
}

func TestFitTreeLeavesAFittingTreeAlone(t *testing.T) {
	tree, _ := Tree(answerOf("1", 5))
	if got := FitTree(tree, MaxViewNodes); CountNodes(got) != CountNodes(tree) {
		t.Errorf("a tree that fits changed: %d nodes, was %d", CountNodes(got), CountNodes(tree))
	}
}

func TestFitTreeCutsTheLargestAnswerAndKeepsTheRest(t *testing.T) {
	tree, _ := Tree(`<div id="hc-thread">` + answerOf("small", 3) + answerOf("big", 3000) + `<div id="hc-confirm">keep me</div></div>`)
	before := CountNodes(tree)
	got := FitTree(tree, 500)
	if CountNodes(got) > 500 {
		t.Fatalf("%d nodes over 500", CountNodes(got))
	}
	if CountNodes(tree) != before {
		t.Error("the argument was changed")
	}
	flat := flatText(got)
	if !strings.Contains(flat, CutNote) || !strings.Contains(flat, "keep me") {
		t.Errorf("the note or the confirm bar is missing: %s", flat[:min(len(flat), 200)])
	}
	small := 0
	var walk func([]Node)
	walk = func(ns []Node) {
		for _, n := range ns {
			if el, ok := n.(Element); ok {
				if el.A["id"] == "hc-a-small" {
					small = CountNodes(el.K)
				}
				walk(el.K)
			}
		}
	}
	walk(got)
	if small != 6 {
		t.Errorf("the small answer was cut: %d nodes", small)
	}
}

// One long list is one element: the cut goes inside it, so the items go
// and the list stays.
func TestFitTreeCutsInsideOneLongList(t *testing.T) {
	tree, _ := Tree(`<div class="hc-a" id="hc-a-1"><ul>` + strings.Repeat("<li>item</li>", 2000) + `</ul></div>`)
	got := FitTree(tree, 800)
	if n := CountNodes(got); n > 800 || n < 100 {
		t.Errorf("%d nodes", n)
	}
	if !strings.Contains(flatText(got), CutNote) {
		t.Error("no note")
	}
}

func TestFitTreeBoundsDepthAndTotal(t *testing.T) {
	deep := strings.Repeat("<div>", 40) + "x" + strings.Repeat("</div>", 40)
	tree, _ := Tree(deep)
	got := FitTree(tree, MaxViewNodes)
	if d := depthOf(got); d > 24 {
		t.Errorf("depth %d", d)
	}
	flat, _ := Tree(strings.Repeat("<span>a</span>", 5000))
	if n := CountNodes(FitTree(flat, 1000)); n > 1000 {
		t.Errorf("%d nodes over 1000", n)
	}
}

func flatText(nodes []Node) string {
	var b strings.Builder
	for _, n := range nodes {
		switch v := n.(type) {
		case string:
			b.WriteString(v)
		case Element:
			b.WriteString(flatText(v.K))
		}
	}
	return b.String()
}

// The new-session form: the shell's contract test reads this golden to see
// every field the pill sends (kind, other_kind, cwd, name).
func TestTreeGoldenForm(t *testing.T) {
	out := treeJSON(t, Form([]string{"gemini", "opencode"}, []string{"/home/x/proj", "/home/x/b"}, FormValues{Kind: "more", OtherKind: "gemini", Cwd: "/home/x/proj", Name: "mine"}))
	for _, name := range []string{"kind", "other_kind", "cwd", "name"} {
		if !strings.Contains(out, `"name": "`+name+`"`) {
			t.Errorf("the form has no field named %q", name)
		}
	}
	golden(t, "form", out)
}

func TestFitViewTopLevelIsElementsOnly(t *testing.T) {
	tree, _ := Tree("\n<div>a</div>\n plain <p>b</p>\n")
	got := FitView(tree, MaxViewNodes)
	for _, v := range got {
		if _, ok := v.(Element); !ok {
			t.Fatalf("top-level %T %v in %v", v, v, got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("got %d nodes: %v", len(got), got)
	}
}

func TestFitViewBoundsBytes(t *testing.T) {
	big := strings.Repeat("x", 600<<10)
	tree, _ := Tree("<div class=\"hc-a\"><pre>" + big + "</pre><pre>" + big + "</pre></div>")
	got := FitView(tree, MaxViewNodes)
	if n := jsonSize(got); n > MaxViewBytes || n == 0 {
		t.Fatalf("size %d", n)
	}
	many := strings.Repeat("<p>"+strings.Repeat("y", 1500)+"</p>", 600)
	tree, _ = Tree("<div>" + many + "</div>")
	if n := jsonSize(FitView(tree, MaxViewNodes)); n > MaxViewBytes || n == 0 {
		t.Fatalf("many: size %d", n)
	}
}

// An oversized thread loses the end of its largest answer, not the newest
// turns, the confirm bar, the dialog card or the error line.
func TestFitViewByteCutKeepsTheTailOfTheThread(t *testing.T) {
	para := "<p>" + strings.Repeat("y", 1500) + "</p>"
	big := `<div class="hc-a" id="hc-a-big">` + strings.Repeat(para, 400) + `</div>`
	tree, _ := Tree(`<div id="hc-thread">` + answerOf("old", 3) + big + answerOf("newest", 3) + `</div>` +
		`<div id="hc-confirm">confirm-bar</div><div class="hc-dialog">dialog-card</div><p id="hc-error">error-line</p>`)
	got := FitView(tree, MaxViewNodes)
	if n := jsonSize(got); n > MaxViewBytes || n == 0 {
		t.Fatalf("size %d", n)
	}
	flat := flatText(got)
	for _, want := range []string{"confirm-bar", "dialog-card", "error-line", CutNote} {
		if !strings.Contains(flat, want) {
			t.Errorf("%q is gone from the cut view", want)
		}
	}
	ids := map[string]int{}
	var walk func([]Node)
	walk = func(ns []Node) {
		for _, n := range ns {
			if el, ok := n.(Element); ok {
				ids[el.A["id"]] = CountNodes(el.K)
				walk(el.K)
			}
		}
	}
	walk(got)
	if ids["hc-a-old"] != 6 || ids["hc-a-newest"] != 6 {
		t.Errorf("another turn was cut: %v", ids)
	}
	if ids["hc-a-big"] == 0 {
		t.Error("the big answer is gone")
	}
}

// With no answer to cut, the largest element is cut inside and says so.
func TestFitViewByteCutWithoutAnswerLeavesANote(t *testing.T) {
	para := "<p>" + strings.Repeat("y", 1500) + "</p>"
	tree, _ := Tree(`<div id="hc-a-x">` + strings.Repeat(para, 400) + `</div><div id="hc-confirm">confirm-bar</div>`)
	got := FitView(tree, MaxViewNodes)
	flat := flatText(got)
	if jsonSize(got) > MaxViewBytes || !strings.Contains(flat, CutNote) || !strings.Contains(flat, "confirm-bar") {
		t.Errorf("size %d, note %v, confirm %v", jsonSize(got), strings.Contains(flat, CutNote), strings.Contains(flat, "confirm-bar"))
	}
}

// Wrapping top-level text in a span counts against the node budget.
func TestFitViewWrapCountsAgainstTheBudget(t *testing.T) {
	// exactly max nodes, all top-level text: each one gains a span.
	const max = 4000
	tree := make([]Node, 0, max)
	for i := 0; i < max; i++ {
		tree = append(tree, "text")
	}
	if CountNodes(tree) != max {
		t.Fatal("setup")
	}
	got := FitView(tree, max)
	if n := CountNodes(got); n > max {
		t.Errorf("%d nodes over %d after wrapping", n, max)
	}
	for _, v := range got {
		if _, ok := v.(Element); !ok {
			t.Fatalf("top-level %T", v)
		}
	}
}

// A view with no answer is wrapped in one root (render.View); the byte cut goes
// inside its body, so the foot controls and the error slot stay.
func TestFitViewByteCutInsideTheViewWrapperKeepsTheFoot(t *testing.T) {
	var rows strings.Builder
	for i := 0; i < 300; i++ {
		rows.WriteString(`<div class="hc-row" data-x="` + strings.Repeat("z", 1000) + `">r</div>`)
	}
	tree, _ := Tree(`<div id="hc-view"><div class="hc-picker"><div class="hc-group">` + rows.String() +
		`</div><button class="hc-new">New session</button></div><div id="hc-error" hidden></div></div>`)
	got := FitView(tree, MaxViewNodes)
	if jsonSize(got) > MaxViewBytes {
		t.Fatalf("size %d", jsonSize(got))
	}
	hasErr := false
	var walk func([]Node)
	walk = func(ns []Node) {
		for _, n := range ns {
			if el, ok := n.(Element); ok {
				if el.A["id"] == "hc-error" {
					hasErr = true
				}
				walk(el.K)
			}
		}
	}
	walk(got)
	flat := flatText(got)
	if !hasErr || !strings.Contains(flat, "New session") || !strings.Contains(flat, CutNote) {
		t.Errorf("error slot %v, button %v, note %v", hasErr, strings.Contains(flat, "New session"), strings.Contains(flat, CutNote))
	}
}

// The note the byte cut adds does not push the view past the depth limit.
func TestFitViewByteCutNoteKeepsTheDepthLimit(t *testing.T) {
	var kids []Node
	for i := 0; i < 200; i++ {
		kids = append(kids, strings.Repeat("t", 1900))
	}
	var n Node = Element{T: "div", A: map[string]string{"class": "hc-a"}, K: kids}
	for i := 0; i < 21; i++ {
		n = Element{T: "div", K: []Node{n}}
	}
	got := FitView([]Node{n}, MaxViewNodes)
	if jsonSize(got) > MaxViewBytes || depthOf(got) > maxViewDepth {
		t.Errorf("size %d, depth %d", jsonSize(got), depthOf(got))
	}
}
