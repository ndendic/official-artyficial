package render

import (
	"strings"
	"testing"
)

// A pipe table in a reply is a table, not a paragraph of pipes.
func TestMarkdownTable(t *testing.T) {
	md := "Results:\n\n| Name | Count |\n|:-----|------:|\n| `a` | 1 |\n| **b** | 2 |\n\nDone."
	got := Markdown(md)
	want := `<p>Results:</p>` +
		`<table><thead><tr><th>Name</th><th>Count</th></tr></thead>` +
		`<tbody><tr><td><code>a</code></td><td>1</td></tr><tr><td><b>b</b></td><td>2</td></tr></tbody></table>` +
		`<p>Done.</p>`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestMarkdownTableShapes(t *testing.T) {
	for name, tc := range map[string]struct{ md, want string }{
		"no outer pipes": {
			"a | b\n--- | ---\n1 | 2",
			`<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>`,
		},
		"short and long rows fit the header": {
			"| a | b |\n|---|---|\n| 1 |\n| 1 | 2 | 3 |",
			`<table><thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>1</td><td></td></tr><tr><td>1</td><td>2</td></tr></tbody></table>`,
		},
		"an escaped pipe stays in its cell": {
			"| expr | means |\n|---|---|\n| `a \\| b` | or |",
			`<table><thead><tr><th>expr</th><th>means</th></tr></thead><tbody><tr><td><code>a | b</code></td><td>or</td></tr></tbody></table>`,
		},
		"header only": {
			"| a | b |\n|---|---|",
			`<table><thead><tr><th>a</th><th>b</th></tr></thead></table>`,
		},
		"a line without a pipe ends it": {
			"| a |\n|---|\n| 1 |\nafter",
			`<table><thead><tr><th>a</th></tr></thead><tbody><tr><td>1</td></tr></tbody></table><p>after</p>`,
		},
		"it interrupts a paragraph": {
			"text\n| a | b |\n|---|---|\n| 1 | 2 |",
			`<p>text</p><table><thead><tr><th>a</th><th>b</th></tr></thead><tbody><tr><td>1</td><td>2</td></tr></tbody></table>`,
		},
		"cells are escaped": {
			"| <b>x</b> |\n|---|\n| <img src=x onerror=alert(1)> |",
			`<table><thead><tr><th>&lt;b&gt;x&lt;/b&gt;</th></tr></thead><tbody><tr><td>&lt;img src=x onerror=alert(1)&gt;</td></tr></tbody></table>`,
		},
	} {
		if got := Markdown(tc.md); got != tc.want {
			t.Errorf("%s:\ngot  %s\nwant %s", name, got, tc.want)
		}
	}
}

// Text that only looks like a table stays what it was.
func TestMarkdownNotATable(t *testing.T) {
	for name, md := range map[string]string{
		"no delimiter row":         "| a | b |\n| 1 | 2 |",
		"cell counts differ":       "| a | b |\n|---|\n| 1 | 2 |",
		"a rule under one word":    "title\n---",
		"inside a fence":           "```\n| a | b |\n|---|---|\n```",
		"a delimiter with letters": "| a | b |\n|-x-|---|",
	} {
		if got := Markdown(md); strings.Contains(got, "<table>") {
			t.Errorf("%s: rendered a table: %s", name, got)
		}
	}
}

// A table still streaming (its header typed, the delimiter row not yet)
// is a paragraph until the delimiter arrives.
func TestMarkdownTableWhileStreaming(t *testing.T) {
	if got := Markdown("| a | b |"); strings.Contains(got, "<table>") {
		t.Fatalf("a header alone rendered a table: %s", got)
	}
	if got := Markdown("| a | b |\n|---|---|"); !strings.Contains(got, "<table>") {
		t.Fatalf("the delimiter row did not make a table: %s", got)
	}
}
