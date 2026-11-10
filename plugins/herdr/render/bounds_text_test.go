package render

import (
	"strings"
	"testing"
	"time"
)

// A foot whose bulk sits in spared attributes cannot be cut; FitView must
// still return (it used to halve a view that never shrank, forever).
func TestFitViewFootAloneOverMaxReturns(t *testing.T) {
	conf := Confirm("p1", "crew-a", "claude", strings.Repeat("<", 40<<10), nil, true)
	tree, err := Tree(View("thread", bigThread()+conf+NoDialog()))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan []Node, 1)
	go func() { done <- FitView(tree, MaxViewNodes) }()
	select {
	case got := <-done:
		// the foot alone is over the byte bound: it is kept whole, as the
		// README says, and the view is not emptied
		if !hasID(got, ConfirmID) || !hasID(got, ErrorID) {
			t.Errorf("foot lost")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("FitView does not return")
	}
}

func textOf(ns []Node) string {
	var b strings.Builder
	for _, n := range ns {
		switch x := n.(type) {
		case string:
			b.WriteString(x)
		case Element:
			b.WriteString(textOf(x.K))
		}
	}
	return b.String()
}

// A huge dialog keeps text, not only the note.
func TestFitViewHugeDialogKeepsAPrefix(t *testing.T) {
	screen := "Do you want to proceed?\n" + strings.Repeat("line of the dialog screen\n", 20000)
	tree, _ := Tree(View("thread", bigThread()+`<div id="hc-confirm" hidden></div>`+Dialog("p1", "crew-a", screen, []string{"enter", "esc"})+`<div id="hc-error" hidden></div>`))
	got := FitView(tree, MaxViewNodes)
	card, ok := findEl(got, DialogID)
	if !ok {
		t.Fatal("dialog lost")
	}
	if txt := textOf(card.K); !strings.Contains(txt, "is waiting on a dialog") {
		t.Errorf("dialog lost its intro: %.120q", txt)
	}
	if jsonSize(got) > MaxViewBytes {
		t.Errorf("over max: %d", jsonSize(got))
	}
}

// Many small answers keep a share of their text each, not only the note.
func TestFitViewSmallAnswersKeepText(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<div class="hc-thread" id="hc-thread">`)
	for i := 0; i < 300; i++ {
		b.WriteString(`<div class="hc-a" id="hc-a-` + strings.Repeat("x", 1) + `">` + strings.Repeat("word ", 400) + `</div>`)
	}
	b.WriteString(`</div>`)
	tree, _ := Tree(View("thread", b.String()))
	got := FitView(tree, MaxViewNodes)
	if jsonSize(got) > MaxViewBytes {
		t.Fatalf("over max: %d", jsonSize(got))
	}
	if !strings.Contains(textOf(got), "word word") {
		t.Errorf("no answer kept any text")
	}
}

// The prompt is at the bottom of a dialog screen: a cut keeps the end.
func TestFitViewHugeDialogKeepsTheTail(t *testing.T) {
	screen := strings.Repeat("line of the dialog screen\n", 20000) + "Do you want to proceed? (y/n)"
	tree, _ := Tree(View("thread", bigThread()+`<div id="hc-confirm" hidden></div>`+Dialog("p1", "crew-a", screen, []string{"enter", "esc"})+`<div id="hc-error" hidden></div>`))
	got := FitView(tree, MaxViewNodes)
	card, ok := findEl(got, DialogID)
	if !ok {
		t.Fatal("dialog lost")
	}
	txt := textOf(card.K)
	if !strings.Contains(txt, "Do you want to proceed? (y/n)") || !strings.Contains(txt, "…") {
		t.Errorf("dialog lost its end: %.80q", txt[len(txt)-80:])
	}
	if jsonSize(got) > MaxViewBytes {
		t.Errorf("over max: %d", jsonSize(got))
	}
}
