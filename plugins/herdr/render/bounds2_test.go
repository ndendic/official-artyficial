package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func bigThread() string {
	return `<div class="hc-thread" id="hc-thread"><div class="hc-q">q</div><div class="hc-a" id="hc-a-1">` +
		strings.Repeat("<p>"+strings.Repeat("w", 200)+"</p>", 1500) + `</div></div>`
}

func findEl(ns []Node, id string) (Element, bool) {
	for _, n := range ns {
		if el, ok := n.(Element); ok {
			if el.A["id"] == id {
				return el, true
			}
			if e, ok := findEl(el.K, id); ok {
				return e, true
			}
		}
	}
	return Element{}, false
}

func pillCalls(ns []Node, out *[]string) {
	for _, n := range ns {
		if el, ok := n.(Element); ok {
			if c, ok := el.A["data-pill-call"]; ok {
				*out = append(*out, c)
			}
			pillCalls(el.K, out)
		}
	}
}

func TestFitViewConfirmCallStaysWhole(t *testing.T) {
	conf := Confirm("p1", "crew-a", "claude", strings.Repeat("please do this. ", 200), nil, true)
	tree, err := Tree(View("thread", bigThread()+conf+NoDialog()))
	if err != nil {
		t.Fatal(err)
	}
	got := FitView(tree, MaxViewNodes)
	wantFoot(t, "confirm", got, true)
	c, _ := findEl(got, ConfirmID)
	var calls []string
	pillCalls([]Node{c}, &calls)
	if len(calls) < 2 {
		t.Fatalf("calls %d", len(calls))
	}
	for _, s := range calls {
		if !json.Valid([]byte(s)) {
			t.Errorf("call cut: %.80s", s)
		}
	}
}

func TestFitViewNeverCutsAPillCallOutsideTheFoot(t *testing.T) {
	big := `{"a":"` + strings.Repeat("x", 3000) + `"}`
	body := `<div class="hc-head"><button data-pill-call='` + big + `'>b</button></div>` + bigThread()
	tree, _ := Tree(View("thread", body+footMarkup()))
	got := FitView(tree, MaxViewNodes)
	var calls []string
	pillCalls(got, &calls)
	for _, s := range calls {
		if strings.HasPrefix(s, "{") && !json.Valid([]byte(s)) {
			t.Errorf("call cut: %.60s", s)
		}
	}
}

func TestFitViewDialogCardKeepsItsOptions(t *testing.T) {
	screen := strings.Repeat("│ "+strings.Repeat("·", 150)+" │\n", 15) + "❯ 1. Yes\n  2. No, and tell Claude what to do"
	tree, _ := Tree(View("thread", bigThread()+`<div id="hc-confirm" hidden></div>`+Dialog("p1", "a", screen, []string{"1", "2"})))
	got := FitView(tree, MaxViewNodes)
	wantFoot(t, "dialog", got, true)
	d, _ := findEl(got, DialogID)
	if !strings.Contains(flatText([]Node{d}), "2. No") {
		t.Error("dialog options cut")
	}
}

func TestFitViewHugeClassKeepsAnswerStatus(t *testing.T) {
	cls := strings.Repeat("junk-class ", 400) + "hc-a"
	body := `<div class="hc-thread" id="hc-thread"><div class="` + cls + `">` + strings.Repeat("<p>"+strings.Repeat("w", 200)+"</p>", 1500) + `</div></div>`
	tree, _ := Tree(View("thread", body+footMarkup()))
	got := FitView(tree, MaxViewNodes)
	wantFoot(t, "class", got, true)
}

func TestFitTreeManySmallAnswersIsFast(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<div class="hc-thread" id="hc-thread">`)
	for i := 0; i < 3000; i++ {
		b.WriteString(`<div class="hc-q">q</div><div class="hc-a"><p>a</p><p>b</p><p>c</p><p>d</p></div>`)
	}
	b.WriteString(`</div>`)
	tree, _ := Tree(View("thread", b.String()+footMarkup()))
	start := time.Now()
	got := FitView(tree, MaxViewNodes)
	took := time.Since(start)
	wantFoot(t, "3000 small", got, true)
	if took > 500*time.Millisecond && !raceEnabled {
		t.Errorf("took %v", took)
	}
}

func TestFitViewTinyAnswersDropsNewestWithANote(t *testing.T) {
	var b strings.Builder
	b.WriteString(`<div class="hc-thread" id="hc-thread">`)
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, `<div class="hc-q">q%d</div><div class="hc-a"><p>a%d</p></div>`, i, i)
	}
	b.WriteString(`</div>`)
	tree, _ := Tree(View("thread", b.String()+footMarkup()))
	got := FitView(tree, MaxViewNodes)
	wantFoot(t, "5000 tiny", got, true)
}

func TestFitViewFootLargerThanMaxKeepsEveryFootElement(t *testing.T) {
	card := `<div class="hc-dialog" id="hc-dialog">` + strings.Repeat("<i>x</i>", 1400) + `</div>`
	body := bigThread() + `<div id="hc-confirm" hidden></div>` + card + `<div id="hc-error" hidden></div>`
	tree, _ := Tree(View("thread", body))
	got := FitView(tree, 3500)
	for _, id := range []string{ConfirmID, DialogID, ErrorID} {
		if !hasID(got, id) {
			t.Errorf("#%s lost", id)
		}
	}
	// the foot alone passes the budget
	small := FitTree(tree, 1000)
	for _, id := range []string{ConfirmID, DialogID, ErrorID} {
		if !hasID(small, id) {
			t.Errorf("max 1000: #%s lost", id)
		}
	}
}
