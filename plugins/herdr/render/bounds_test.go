package render

import (
	"strings"
	"testing"
	"time"
)

func hasID(ns []Node, id string) bool {
	for _, n := range ns {
		if el, ok := n.(Element); ok {
			if el.A["id"] == id || hasID(el.K, id) {
				return true
			}
		}
	}
	return false
}

func boundsOK(t *testing.T, name string, got []Node) {
	t.Helper()
	if s := jsonSize(got); s > MaxViewBytes {
		t.Errorf("%s: size %d > %d", name, s, MaxViewBytes)
	}
	if c := CountNodes(got); c > MaxViewNodes {
		t.Errorf("%s: nodes %d", name, c)
	}
	if d := depthOf(got); d > maxViewDepth {
		t.Errorf("%s: depth %d", name, d)
	}
	for _, v := range got {
		if _, ok := v.(Element); !ok {
			t.Errorf("%s: top-level %T", name, v)
		}
	}
}

func footMarkup() string {
	return `<div class="hc-confirm" id="hc-confirm" role="alertdialog"><span>confirm-bar</span><button data-pill-call="x">Send</button></div><div class="hc-dialog" id="hc-dialog"><p>dialog-card</p></div>`
}

func threadWith(ans string) []Node {
	body := `<div class="hc-thread" id="hc-thread"><div class="hc-q">q</div><div class="hc-a" id="hc-a-1">` + ans + `</div></div>` + footMarkup()
	tree, err := Tree(View("thread", body))
	if err != nil {
		panic(err)
	}
	return tree
}

func wantFoot(t *testing.T, name string, got []Node, note bool) {
	t.Helper()
	boundsOK(t, name, got)
	for _, id := range []string{ConfirmID, "hc-dialog", ErrorID} {
		if !hasID(got, id) {
			t.Errorf("%s: #%s lost", name, id)
		}
	}
	if note && !strings.Contains(flatText(got), CutNote) {
		t.Errorf("%s: CutNote lost", name)
	}
}

func TestFitViewHugeHrefInAnswerKeepsTheFoot(t *testing.T) {
	for _, tc := range []struct{ name, ans string }{
		{"last", `<p>hello <a href="https://a/` + strings.Repeat("A", 400<<10) + `">x</a></p>`},
		{"middle", `<p>see <a href="https://a/` + strings.Repeat("x", 300<<10) + `">link</a> and more text</p>`},
		{"other attr", `<p title="` + strings.Repeat("é", 300<<10) + `">text</p>`},
		{"plain text", `<p>` + strings.Repeat("y", 400<<10) + `</p>`},
	} {
		got := FitView(threadWith(tc.ans), MaxViewNodes)
		wantFoot(t, tc.name, got, false)
		if !strings.Contains(flatText(got), "q") {
			t.Errorf("%s: question lost", tc.name)
		}
	}
}

func TestCapAttrsDropsLinksAndCutsTheRest(t *testing.T) {
	big := strings.Repeat("é", maxAttrBytes) // 2 bytes a rune
	in := []Node{Element{T: "a", A: map[string]string{"href": big, "title": big, "id": "k"}, K: []Node{"x"}}}
	got := capAttrs(in)[0].(Element)
	if _, ok := got.A["href"]; ok {
		t.Error("oversize href kept")
	}
	if l := len(got.A["title"]); l == 0 || l > maxAttrBytes {
		t.Errorf("title len %d", l)
	}
	if got.A["id"] != "k" {
		t.Error("small attribute changed")
	}
	if len(in[0].(Element).A["href"]) == 0 {
		t.Error("argument changed")
	}
}

func TestFitViewHugeAttrOutsideAnswersKeepsTheFoot(t *testing.T) {
	body := `<div class="hc-head" title="` + strings.Repeat("é", 200<<10) + `">h</div><div class="hc-thread" id="hc-thread"><div class="hc-a" id="hc-a-1"><p>small</p></div></div>` + footMarkup()
	tree, _ := Tree(View("thread", body))
	wantFoot(t, "head attr", FitView(tree, MaxViewNodes), false)
}

// when the answers cannot cover the excess even cut to the note, the foot
// stays and the answers' content becomes the note
func TestFitViewOversizeFootSiblingsKeepTheFoot(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<div class="hc-thread" id="hc-thread">`)
	b.WriteString(`<div class="hc-a">a</div>`)
	for i := 0; i < 150; i++ {
		b.WriteString(`<div class="hc-q">` + strings.Repeat("q", 1999) + `</div>`)
	}
	b.WriteString(`</div>`)
	tree, _ := Tree(View("thread", b.String()+footMarkup()))
	if jsonSize(tree) <= MaxViewBytes {
		t.Fatal("setup: not over the limit")
	}
	wantFoot(t, "no answers to cut", FitView(tree, MaxViewNodes), false)
}

func TestFitViewAnswersCoverNothingBecomeTheNote(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<div class="hc-thread" id="hc-thread">`)
	for i := 0; i < 3; i++ {
		b.WriteString(`<div class="hc-a">` + strings.Repeat("a", 1999) + `</div>`)
	}
	b.WriteString(`<ul>`)
	for i := 0; i < 3500/2; i++ {
		b.WriteString(`<li>` + strings.Repeat("q", 150) + `</li>`)
	}
	b.WriteString(`</ul></div>`)
	tree, _ := Tree(View("thread", b.String()+footMarkup()))
	if jsonSize(tree) <= MaxViewBytes {
		t.Fatal("setup: not over the limit")
	}
	wantFoot(t, "answers too small", FitView(tree, MaxViewNodes), false)
}

func TestFitViewNoteSurvivesAtTheDepthLimit(t *testing.T) {
	var kids []Node
	for i := 0; i < 200; i++ {
		kids = append(kids, strings.Repeat("t", 1900))
	}
	var n Node = Element{T: "div", A: map[string]string{"class": "hc-a"}, K: kids}
	for i := 0; i < 21; i++ {
		n = Element{T: "div", K: []Node{n}}
	}
	got := FitView([]Node{n}, MaxViewNodes)
	boundsOK(t, "depth 22", got)
	if !strings.Contains(flatText(got), CutNote) {
		t.Error("CutNote pruned by the final guard")
	}
}

func TestFitViewNoteSurvivesAtTheDepthLimitWithoutAnswer(t *testing.T) {
	var kids []Node
	for i := 0; i < 200; i++ {
		kids = append(kids, strings.Repeat("t", 1900))
	}
	var n Node = Element{T: "div", K: kids}
	for i := 0; i < 21; i++ {
		n = Element{T: "div", K: []Node{n}}
	}
	got := FitView([]Node{n}, MaxViewNodes)
	boundsOK(t, "depth 22 no answer", got)
	if !strings.Contains(flatText(got), CutNote) {
		t.Error("CutNote pruned by the final guard")
	}
}

func manyAnswers(n, each int) []Node {
	var b strings.Builder
	b.WriteString(`<div class="hc-thread" id="hc-thread">`)
	for i := 0; i < n; i++ {
		b.WriteString(`<div class="hc-q">q</div><div class="hc-a"><p>` + strings.Repeat("ж<", each/3) + `</p><p>` + strings.Repeat("x", each/2) + `</p></div>`)
	}
	b.WriteString(`</div>`)
	tree, _ := Tree(View("thread", b.String()+footMarkup()))
	return tree
}

func TestFitViewManyAnswersIsFast(t *testing.T) {
	tree := manyAnswers(300, 7000) // about 2 MB
	if s := jsonSize(tree); s < 2<<20 {
		t.Fatalf("setup: %d bytes", s)
	}
	start := time.Now()
	got := FitView(tree, MaxViewNodes)
	took := time.Since(start)
	wantFoot(t, "300 answers", got, true)
	if took > 200*time.Millisecond && !raceEnabled {
		t.Errorf("took %v", took)
	}
	t.Logf("took %v", took)
}

func TestFitViewManySingleTextAnswersIsFast(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<div class="hc-thread" id="hc-thread">`)
	for i := 0; i < 1700; i++ {
		b.WriteString(`<div class="hc-a">` + strings.Repeat("\"", 1999) + `</div>`)
	}
	b.WriteString(`</div>`)
	tree, _ := Tree(View("thread", b.String()+footMarkup()))
	start := time.Now()
	got := FitView(tree, MaxViewNodes)
	took := time.Since(start)
	wantFoot(t, "1700 answers", got, true)
	if took > 500*time.Millisecond && !raceEnabled {
		t.Errorf("took %v", took)
	}
}

func BenchmarkFitView300Answers(b *testing.B) {
	tree := manyAnswers(300, 7000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FitView(tree, MaxViewNodes)
	}
}

// an element whose own attribute is the bulk: its children cannot cover the
// cut, so it goes whole instead of losing only its text
func TestCutTailBytesDropsAnElementWhoseChildrenCannotCoverTheCut(t *testing.T) {
	a := Element{T: "a", A: map[string]string{"href": "https://a/" + strings.Repeat("A", 50<<10)}, K: []Node{"x"}}
	got, removed := cutTailBytes([]Node{"hello ", a}, 1000)
	if len(got) != 1 || got[0] != "hello " {
		t.Errorf("got %v", got)
	}
	if removed < 50<<10 {
		t.Errorf("removed %d", removed)
	}
}

func TestKeepFootKeepsTheFootAndWhatHoldsIt(t *testing.T) {
	tree := threadWith("<p>a</p>")
	got := keepFoot(tree)
	wantFoot(t, "keepFoot", got, false)
	if hasID(got, "hc-thread") || strings.Contains(flatText(got), "q") {
		t.Errorf("kept more than the foot: %s", flatText(got))
	}
}
