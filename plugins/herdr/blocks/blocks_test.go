package blocks

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	fence := "```"
	tests := []struct {
		name     string
		text     string
		offset   int
		blocks   int
		closed   bool
		kind     string
		selector string
		mode     string
		region   string
		body     string
		err      string // substring of Block.Err; "" means valid
		summary  string
		texts    []string
	}{
		{
			name:   "elements with selector and mode",
			text:   "Here:\n" + fence + "patch-elements selector=\"#aside\" mode=\"append\"\n<article id=\"x\">hi</article>\n" + fence + "\nDone.",
			blocks: 1, closed: true, kind: Elements, selector: "#aside", mode: "append",
			body:    `<article id="x">hi</article>`,
			summary: "patch-elements #aside · append",
			texts:   []string{"Here:\n", "Done."},
		},
		{
			name:   "signals",
			text:   fence + "patch-signals\n{\"theme\":\"dark\"}\n" + fence + "\n",
			blocks: 1, closed: true, kind: Signals, body: `{"theme":"dark"}`,
			summary: `patch-signals {"theme":"dark"}`,
		},
		{
			name:   "no selector, top-level id, outer",
			text:   fence + "patch-elements\n<article id=\"card\"><p>a</p></article>\n" + fence,
			blocks: 1, closed: true, kind: Elements, body: `<article id="card"><p>a</p></article>`,
			summary: "patch-elements #card · outer",
		},
		{
			name:   "no selector and two top-level elements",
			text:   fence + "patch-elements\n<p id=\"a\">1</p><p id=\"b\">2</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, err: "a selector is required",
		},
		{
			name:   "no selector, no id",
			text:   fence + "patch-elements\n<p>1</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, err: "no single top-level element with an id",
		},
		{
			name:   "no selector with append",
			text:   fence + "patch-elements mode=\"append\"\n<p id=\"a\">1</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, mode: "append", err: "a selector is required for mode append",
		},
		{
			name:   "unknown mode",
			text:   fence + "patch-elements selector=\"#a\" mode=\"sideways\"\n<p>1</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, selector: "#a", mode: "sideways", err: "unknown mode sideways",
		},
		{
			name:   "bad JSON",
			text:   fence + "patch-signals\n{theme: dark}\n" + fence,
			blocks: 1, closed: true, kind: Signals, err: "not valid JSON",
		},
		{
			name:   "signals must be an object",
			text:   fence + "patch-signals\n[1,2]\n" + fence,
			blocks: 1, closed: true, kind: Signals, err: "one JSON object",
		},
		{
			name:   "too big",
			text:   fence + "patch-elements selector=\"#a\"\n<p>" + strings.Repeat("x", MaxBody) + "</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, selector: "#a", err: "over the 64 KB limit",
		},
		{
			name:   "unknown attribute",
			text:   fence + "patch-elements target=\"#a\"\n<p id=\"a\">1</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, err: "unknown attribute target",
		},
		{
			name:   "selector on signals is unknown",
			text:   fence + "patch-signals selector=\"#a\"\n{}\n" + fence,
			blocks: 1, closed: true, kind: Signals, err: "unknown attribute selector",
		},
		{
			name:   "unreadable attributes",
			text:   fence + "patch-elements selector=#a\n<p id=\"a\">1</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, err: "cannot read the attributes",
		},
		{
			name:   "region",
			text:   fence + "patch-elements selector=\"#x\" mode=\"inner\" region=\"aside\"\n<b>1</b>\n" + fence,
			blocks: 1, closed: true, kind: Elements, selector: "#x", mode: "inner", region: "aside",
			summary: "patch-elements #x · inner · region aside",
		},
		{
			name:   "region root with append and no selector",
			text:   fence + "patch-elements region=\"main\" mode=\"append\"\n<div id=\"p\">1</div>\n" + fence,
			blocks: 1, closed: true, kind: Elements, mode: "append", region: "main",
			summary: "patch-elements (root) · append · region main",
		},
		{
			name:   "region root refuses before",
			text:   fence + "patch-elements region=\"main\" mode=\"before\"\n<div>1</div>\n" + fence,
			blocks: 1, closed: true, kind: Elements, mode: "before", region: "main", err: "does not work on the root of region main",
		},
		{
			name:   "bad region name",
			text:   fence + "patch-elements region=\"Bad Name\"\n<p id=\"a\">1</p>\n" + fence,
			blocks: 1, closed: true, kind: Elements, region: "Bad Name", err: "not a Patch Region name",
		},
		{
			name:   "signals take no region",
			text:   fence + "patch-signals region=\"main\"\n{}\n" + fence,
			blocks: 1, closed: true, kind: Signals, region: "main", err: "takes no region",
		},
		{
			name:   "open block is not closed and not validated",
			text:   "Wait…\n" + fence + "patch-elements selector=\"#a\" mode=\"nope\"\n<p>half",
			blocks: 1, closed: false, kind: Elements, selector: "#a", mode: "nope",
			texts: []string{"Wait…\n"},
		},
		{
			name:   "a patch fence inside an ordinary code block is text",
			text:   fence + "markdown\n" + fence + "patch-signals\n{}\n" + fence + "\nafter",
			blocks: 0,
			texts:  []string{fence + "markdown\n" + fence + "patch-signals\n{}\n" + fence + "\nafter"},
		},
		{
			name:   "offset numbers blocks",
			text:   fence + "patch-signals\n{}\n" + fence,
			offset: 3, blocks: 1, closed: true, kind: Signals, body: "{}",
		},
		{
			name:   "remove needs no html",
			text:   fence + "patch-elements selector=\"#a\" mode=\"remove\"\n" + fence,
			blocks: 1, closed: true, kind: Elements, selector: "#a", mode: "remove",
		},
		{
			name:   "plain text only",
			text:   "Nothing to patch.",
			blocks: 0, texts: []string{"Nothing to patch."},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			segs := Parse(tc.text, tc.offset)
			var got []*Block
			var texts []string
			for _, s := range segs {
				if s.Block != nil {
					got = append(got, s.Block)
				} else {
					texts = append(texts, s.Text)
				}
			}
			if len(got) != tc.blocks {
				t.Fatalf("blocks = %d, want %d (%+v)", len(got), tc.blocks, segs)
			}
			if tc.texts != nil && strings.Join(texts, "|") != strings.Join(tc.texts, "|") {
				t.Errorf("texts = %q, want %q", texts, tc.texts)
			}
			if tc.blocks == 0 {
				return
			}
			b := got[0]
			if b.Index != tc.offset {
				t.Errorf("index = %d, want %d", b.Index, tc.offset)
			}
			if b.Closed != tc.closed || b.Kind != tc.kind || b.Selector != tc.selector || b.Mode != tc.mode || b.Region != tc.region {
				t.Errorf("block = %+v", b)
			}
			if tc.body != "" && b.Body != tc.body {
				t.Errorf("body = %q, want %q", b.Body, tc.body)
			}
			if tc.err == "" && b.Err != "" {
				t.Errorf("err = %q, want none", b.Err)
			}
			if tc.err != "" && !strings.Contains(b.Err, tc.err) {
				t.Errorf("err = %q, want it to contain %q", b.Err, tc.err)
			}
			if tc.summary != "" && b.Summary() != tc.summary {
				t.Errorf("summary = %q, want %q", b.Summary(), tc.summary)
			}
			if b.Closed && !strings.HasPrefix(b.Raw, fence+b.Kind) || b.Closed && !strings.HasSuffix(b.Raw, fence) {
				t.Errorf("raw = %q", b.Raw)
			}
		})
	}
}

func TestParseKeepsRunningIndex(t *testing.T) {
	fence := "```"
	text := "a\n" + fence + "patch-signals\n{}\n" + fence + "\nb\n" + fence + "patch-elements selector=\"#x\"\n<p>1</p>\n" + fence + "\nc"
	var idx []int
	for _, s := range Parse(text, 0) {
		if s.Block != nil {
			idx = append(idx, s.Block.Index)
		}
	}
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Fatalf("indexes = %v", idx)
	}
	if Count(text) != 2 {
		t.Fatalf("count = %d", Count(text))
	}
}

func TestStreamingGrowthKeepsKeysStable(t *testing.T) {
	fence := "```"
	full := "x\n" + fence + "patch-signals\n{\"a\":1}\n" + fence + "\ny"
	// Every prefix parses without panic, and a block is closed only once
	// the closing fence is present.
	for i := 0; i <= len(full); i++ {
		segs := Parse(full[:i], 0)
		for _, s := range segs {
			if s.Block != nil && s.Block.Closed && !strings.Contains(full[:i], "}\n"+fence) {
				t.Fatalf("closed too early at %d", i)
			}
		}
	}
}

// A block from a transcript carries inert markup only, never touches the
// console, and never sets the console's signals.
func TestBlocksRefuseWhatRunsOrReachesTheConsole(t *testing.T) {
	fence := "```"
	el := func(attrs, body string) string {
		return fence + "patch-elements " + attrs + "\n" + body + "\n" + fence + "\n"
	}
	sig := func(body string) string { return fence + "patch-signals\n" + body + "\n" + fence + "\n" }
	refused := map[string]string{
		"data-init confirm bypass": el(`selector="#aside" mode="append"`, `<div id="x" data-init="window.pill.agent.call({action:'send',pane:'w1:p1',text:'x',confirm:true})"></div>`),
		"data-on:click":            el(`selector="#aside" mode="append"`, `<button id="x" data-on:click="x()">x</button>`),
		"data-effect":              el(`selector="#aside" mode="append"`, `<div id="x" data-effect="x()"></div>`),
		"data-text":                el(`selector="#aside" mode="append"`, `<div id="x" data-text="x()"></div>`),
		"data-pill-call":           el(`selector="#aside" mode="append"`, `<button id="x" data-pill-call='{"action":"send","confirm":true}'>x</button>`),
		"data-pill-form":           el(`selector="#aside" mode="append"`, `<div id="x" data-pill-form="start"><button>x</button></div>`),
		"onclick":                  el(`selector="#aside" mode="append"`, `<button id="x" onclick="x()">x</button>`),
		"script":                   el(`selector="#aside" mode="append"`, `<div id="x"><script>x()</script></div>`),
		"iframe srcdoc":            el(`selector="#aside" mode="append"`, `<iframe id="x" srcdoc="<b>x</b>"></iframe>`),
		"javascript href":          el(`selector="#aside" mode="append"`, `<a id="x" href="javascript:x()">x</a>`),
		"form":                     el(`selector="#aside" mode="append"`, `<form id="x"><input name="a"></form>`),
		"console region":           el(`region="console" mode="inner"`, `<div>x</div>`),
		"console selector":         el(`selector="#pill-console" mode="inner"`, `<div>x</div>`),
		"console inner id":         el(`selector="#hc-confirm" mode="outer"`, `<div id="hc-confirm">x</div>`),
		"console by top-level id":  el(``, `<div id="hc-confirm">fake</div>`),
		"pillAgent signal":         sig(`{"pillAgent":{"pane":"w1:p1","name":"mine"}}`),
		"hc signal":                sig(`{"hcView":"thread"}`),
	}
	for name, text := range refused {
		segs := Parse(text, 0)
		if len(segs) == 0 || segs[0].Block == nil || segs[0].Block.Err == "" {
			t.Errorf("%s: not refused", name)
		}
	}
	for name, text := range map[string]string{
		"plain article":      el(`selector="#aside" mode="append"`, `<article id="x" class="rt-card" data-variant="ghost" data-state="done"><h3>Hi</h3><a href="https://example.com">x</a></article>`),
		"svg":                el(`selector="#aside" mode="append"`, `<svg id="x" viewBox="0 0 1 1"><path d="M0 0"/></svg>`),
		"id with hc- inside": el(`selector="#aside" mode="append"`, `<p id="patch-hc-note">x</p>`),
		"layer region":       el(`region="layer" mode="append"`, `<div class="rt-toast">x</div>`),
		"theme signal":       sig(`{"theme":"dark","demo":{"x":1}}`),
	} {
		segs := Parse(text, 0)
		if len(segs) == 0 || segs[0].Block == nil || segs[0].Block.Err != "" {
			t.Errorf("%s: refused: %+v", name, segs)
		}
	}
}
