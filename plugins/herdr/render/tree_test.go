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
