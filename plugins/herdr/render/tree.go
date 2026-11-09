package render

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Node is one node of a view tree, the shape the shell's pill-views.js
// builds from: either a string (a text node) or an element
// {"t": tag, "a": {attr: value}, "k": [children]}. A Node is a string or
// an Element, so it marshals as either.
type Node any

// Element is the object form of a Node. Empty attributes and children are
// left out of the JSON.
type Element struct {
	T string            `json:"t"`
	A map[string]string `json:"a,omitempty"`
	K []Node            `json:"k,omitempty"`
}

// Tree parses an HTML fragment (as it would sit inside a div) into nodes.
// Text becomes strings, comments are dropped, tag and attribute names stay
// as parsed (lower case, and the parser's own SVG case fixes such as
// viewBox). Nothing here sanitizes: the shell's allowlist does.
func Tree(markup string) ([]Node, error) {
	nodes, err := html.ParseFragment(strings.NewReader(markup), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return nil, err
	}
	out := []Node{}
	for _, n := range nodes {
		if v, ok := convert(n); ok {
			out = append(out, v)
		}
	}
	return out, nil
}

func convert(n *html.Node) (Node, bool) {
	switch n.Type {
	case html.TextNode:
		return n.Data, true
	case html.ElementNode:
		el := Element{T: n.Data}
		for _, a := range n.Attr {
			if a.Namespace != "" {
				continue
			}
			if el.A == nil {
				el.A = map[string]string{}
			}
			el.A[a.Key] = a.Val
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if v, ok := convert(c); ok {
				el.K = append(el.K, v)
			}
		}
		return el, true
	}
	return nil, false // comments, doctype
}

// MaxViewNodes is the most nodes one view may hold. The shell's pill-views.js
// refuses a whole view over 4000 nodes (and deeper than 24 levels); this
// stays under both, so a long answer is cut short instead of refused.
const MaxViewNodes = 3500

// maxViewDepth is the deepest element nesting FitTree keeps.
const maxViewDepth = 22

// CutNote is the line that replaces what FitTree cut from an answer.
const CutNote = "… cut: this answer is longer than the page draws. Read the rest in herdr."

// CountNodes counts every node (text and element) of a tree, as the shell
// counts them against its budget.
func CountNodes(nodes []Node) int {
	n := 0
	for _, v := range nodes {
		n++
		if el, ok := v.(Element); ok {
			n += CountNodes(el.K)
		}
	}
	return n
}

func depthOf(nodes []Node) int {
	d := 0
	for _, v := range nodes {
		if el, ok := v.(Element); ok {
			if x := 1 + depthOf(el.K); x > d {
				d = x
			}
		}
	}
	return d
}

// FitTree makes nodes fit max nodes and the depth limit. It cuts the end of
// the largest answer (an element with class hc-a) first, leaving CutNote in
// its place, so the other turns, the confirm bar and the dialog card stay
// whole. When that is not enough, or the tree is nested too deep, whatever
// is last in document order goes. A tree that fits is returned as it is;
// the argument is never changed.
func FitTree(nodes []Node, max int) []Node {
	if CountNodes(nodes) <= max && depthOf(nodes) <= maxViewDepth {
		return nodes
	}
	// every pass plans the cut of all answers at once, so a long thread of
	// small answers costs a few passes, not one per answer
	for {
		total := CountNodes(nodes)
		if total <= max {
			break
		}
		refs := answerSizes(nodes, func(ks []Node) int { return CountNodes(ks) })
		plan := planCuts(refs, total-max, 3, 3) // the note is 3 nodes
		if plan == nil {
			plan = map[int]int{}
			for _, r := range refs {
				if r.size > 3 {
					plan[r.idx] = r.size + 3
				}
			}
		}
		if len(plan) == 0 {
			break
		}
		next := cutAnswersBy(nodes, plan, func(ks []Node, w int) []Node { k, _ := cutTail(ks, w); return k })
		if CountNodes(next) >= total {
			break
		}
		nodes = next
	}
	return pruneFit(nodes, max)
}

// pruneFit is prune with the foot spared: the confirm bar, the dialog card,
// the error slot and the elements that hold them are kept whole and only
// charged up front, so what goes is the rest, last in document order first.
func pruneFit(nodes []Node, max int) []Node {
	if CountNodes(nodes) <= max && depthOf(nodes) <= maxViewDepth {
		return nodes
	}
	st := &pruneState{}
	spare := hasFoot(nodes)
	reserve := 0
	if spare {
		reserve = CountNodes(keepFoot(nodes))
	}
	// the note that tells the reader the end is gone costs 3 nodes; a foot
	// larger than max is still kept whole and the rest goes
	st.budget = max - reserve - 3
	if st.budget < 0 {
		st.budget = 0
	}
	out := pruneKeep(nodes, st, 0, spare)
	if st.dropped && !st.noted {
		out = append(out, cutNote())
	}
	return out
}

// pruneState is what pruneKeep carries: the node budget left, whether it
// dropped anything for want of budget, and whether the note is placed.
type pruneState struct {
	budget  int
	dropped bool
	noted   bool
}

func pruneKeep(nodes []Node, st *pruneState, depth int, spare bool) []Node {
	out := []Node{}
	for _, v := range nodes {
		el, ok := v.(Element)
		if spare && ok && (isFoot(el) || hasFoot(el.K)) {
			if st.dropped && !st.noted && depth < maxViewDepth {
				// the note goes where the cut was, before the foot
				out = append(out, cutNote())
				st.noted = true
			}
			if isFoot(el) {
				out = append(out, el)
			} else {
				el.K = pruneKeep(el.K, st, depth+1, spare)
				out = append(out, el)
			}
			continue
		}
		if st.budget <= 0 {
			st.dropped = true
			continue
		}
		if !ok {
			st.budget--
			out = append(out, v)
			continue
		}
		if depth >= maxViewDepth {
			continue
		}
		st.budget--
		el.K = pruneKeep(el.K, st, depth+1, false)
		if len(el.K) == 0 {
			el.K = nil
		}
		out = append(out, el)
	}
	return out
}

func isAnswer(el Element) bool {
	for _, c := range strings.Fields(el.A["class"]) {
		if c == "hc-a" {
			return true
		}
	}
	return false
}

func sizeOf(n Node) int {
	if el, ok := n.(Element); ok {
		return 1 + CountNodes(el.K)
	}
	return 1
}

// cutTail removes about want nodes from the end of ks: whole trailing
// nodes while they fit the want, else the tail inside the last element
// (a long list loses its last items, not the list). It returns what is
// left and how many nodes it removed.
func cutTail(ks []Node, want int) ([]Node, int) {
	out := append([]Node(nil), ks...)
	removed := 0
	for len(out) > 0 && removed < want {
		last := out[len(out)-1]
		sz := sizeOf(last)
		if sz <= want-removed {
			out = out[:len(out)-1]
			removed += sz
			continue
		}
		if el, ok := last.(Element); ok && len(el.K) > 0 {
			k, r := cutTail(el.K, want-removed)
			el.K = k
			out[len(out)-1] = el
			removed += r
		}
		break
	}
	return out, removed
}

// MaxViewBytes is the most JSON one view may hold. Core refuses a reply over
// 1 MiB whole, and one reply can carry several views, so a view stays well
// under it: text nodes are unbounded in a node count.
const MaxViewBytes = 224 << 10

// maxTextRunes is the longest text node FitView keeps when a view is over
// MaxViewBytes.
const maxTextRunes = 2000

// FitView is FitTree plus what core's result schema and payload cap need: a
// top-level node is an element (whitespace between elements is dropped, other
// top-level text goes into a span, and that span counts against max), and the
// view's JSON is at most MaxViewBytes. Over that, long text is cut first;
// then the end of the largest answer (hc-a), else of the largest element, goes
// and CutNote says so, as FitTree does for nodes, so the other turns, the
// confirm bar, the dialog card and the error line stay whole. The argument is
// never changed.
func FitView(nodes []Node, max int) []Node {
	limit := max
	wrapped := make([]Node, 0, len(nodes))
	for _, v := range nodes {
		if s, ok := v.(string); ok {
			if strings.TrimSpace(s) == "" {
				continue
			}
			v = Element{T: "span", K: []Node{s}}
		}
		wrapped = append(wrapped, v)
	}
	out := FitTree(wrapped, max)
	for size := jsonSize(out); size > MaxViewBytes; size = jsonSize(out) {
		if next := cutText(out, maxTextRunes, false); jsonSize(next) < size {
			out = next
			continue
		}
		if next := capAttrs(out); jsonSize(next) < size {
			out = next
			continue
		}
		if next := cutBytes(out, size-MaxViewBytes); jsonSize(next) < size {
			out = next
			continue
		}
		if hasFoot(out) {
			// never halve the whole view while a foot exists
			if next := keepFoot(out); jsonSize(next) < size {
				out = next
				continue
			}
		}
		before := CountNodes(out)
		max = before / 2
		if max < 1 {
			return []Node{}
		}
		out = pruneFit(out, max)
		if CountNodes(out) >= before {
			// the bulk sits where no pass reaches (spared attributes of the
			// foot): halving cannot shrink it, so keep the foot and stop
			if hasFoot(out) {
				out = keepFoot(out)
			}
			break
		}
	}
	// the note can add nodes to a view that was at its limit
	if CountNodes(out) > limit || depthOf(out) > maxViewDepth {
		out = FitTree(out, limit)
	}
	return out
}

// maxAttrBytes is the longest attribute value FitView keeps when a view is
// over MaxViewBytes.
const maxAttrBytes = 2 << 10

// footIDs are the elements a cut must never lose: the confirm bar, the dialog
// card and the error slot.
var footIDs = map[string]bool{"hc-confirm": true, "hc-dialog": true, "hc-error": true}

func isFoot(el Element) bool { return footIDs[el.A["id"]] }

func hasFoot(nodes []Node) bool {
	for _, v := range nodes {
		if el, ok := v.(Element); ok && (isFoot(el) || hasFoot(el.K)) {
			return true
		}
	}
	return false
}

// urlAttr says whether an attribute holds a link: capAttrs drops those when
// they are over the cap (the shell lets a link through only as http, https,
// relative or #, so a dropped one is safe), and truncates any other.
func urlAttr(k string) bool {
	k = strings.ToLower(k)
	return strings.Contains(k, "href") || strings.Contains(k, "src") ||
		k == "action" || k == "formaction" || k == "poster" || k == "cite" || k == "data" || k == "ping"
}

// pillAttr says whether an attribute is a call or a form of the shell: cut
// mid-string it is no longer valid JSON, so it is kept whole or dropped.
func pillAttr(k string) bool { return k == "data-pill-call" || k == "data-pill-form" }

// capClass keeps the hc- tokens of a class list first (they carry the roles
// the view is cut by), then any other that fits the cap.
func capClass(val string) string {
	var keep, rest []string
	for _, c := range strings.Fields(val) {
		if strings.HasPrefix(c, "hc-") {
			keep = append(keep, c)
		} else {
			rest = append(rest, c)
		}
	}
	out, n := []string{}, 0
	for _, c := range append(keep, rest...) {
		if n+len(c)+1 > maxAttrBytes {
			continue
		}
		out = append(out, c)
		n += len(c) + 1
	}
	return strings.Join(out, " ")
}

// capAttrs returns nodes with every attribute value over maxAttrBytes dropped
// (links, calls, forms, ids) or cut to that size at a rune boundary. A class
// list keeps its hc- tokens. The foot (confirm bar, dialog card, error slot)
// is spared except for oversize links: its calls are the buttons' own.
func capAttrs(nodes []Node) []Node { return capAttrsIn(nodes, false) }

func capAttrsIn(nodes []Node, foot bool) []Node {
	out := make([]Node, len(nodes))
	for i, v := range nodes {
		if el, ok := v.(Element); ok {
			inFoot := foot || isFoot(el)
			var a map[string]string
			for k, val := range el.A {
				if len(val) <= maxAttrBytes {
					continue
				}
				if inFoot && !urlAttr(k) {
					continue
				}
				if a == nil {
					a = make(map[string]string, len(el.A))
					for k2, v2 := range el.A {
						a[k2] = v2
					}
				}
				switch {
				case urlAttr(k) || pillAttr(k) || k == "id":
					delete(a, k)
				case k == "class":
					if c := capClass(val); c != "" {
						a[k] = c
					} else {
						delete(a, k)
					}
				default:
					c := maxAttrBytes
					for c > 0 && !utf8.RuneStart(val[c]) {
						c--
					}
					a[k] = val[:c]
				}
			}
			if a != nil {
				if len(a) == 0 {
					a = nil
				}
				el.A = a
			}
			el.K = capAttrsIn(el.K, inFoot)
			if len(el.K) == 0 {
				el.K = nil
			}
			v = el
		}
		out[i] = v
	}
	return out
}

func cutNote() Element {
	return Element{T: "p", A: map[string]string{"class": "hc-wait"}, K: []Node{CutNote}}
}

// answerRef is one outermost answer: its preorder index and the JSON size of
// its content, measured once.
type answerRef struct{ idx, size int }

func answerSizes(nodes []Node, size func([]Node) int) []answerRef {
	var out []answerRef
	var walk func(ns []Node)
	walk = func(ns []Node) {
		for _, v := range ns {
			el, ok := v.(Element)
			if !ok {
				continue
			}
			if isAnswer(el) {
				out = append(out, answerRef{len(out), size(el.K)})
				continue
			}
			walk(el.K)
		}
	}
	walk(nodes)
	return out
}

// planCuts spreads excess bytes over the largest answers: the biggest ones
// are cut down to a common level, so a thread of many answers loses a little
// from each big one, not one answer at a time. It returns the bytes to remove
// per answer index (0 = leave alone), or nil when the answers cannot cover
// excess even cut to the note.
func planCuts(refs []answerRef, excess, noteSz, slack int) map[int]int {
	cand := []answerRef{}
	for _, r := range refs {
		if r.size > noteSz+slack {
			cand = append(cand, r)
		}
	}
	sort.Slice(cand, func(i, j int) bool { return cand[i].size > cand[j].size })
	sum := 0
	for k := 1; k <= len(cand); k++ {
		sum += cand[k-1].size
		next := 0
		if k < len(cand) {
			next = cand[k].size
		}
		level := (sum - excess - k*noteSz) / k
		if level < next && k < len(cand) {
			continue
		}
		if level < 0 {
			break
		}
		plan := map[int]int{}
		for _, r := range cand[:k] {
			if w := r.size - level + noteSz; w > 0 {
				plan[r.idx] = w
			}
		}
		return plan
	}
	return nil
}

// cutBytes returns nodes with about excess bytes of JSON cut from the end of
// the largest answers (else of the largest element) and a CutNote added to
// each cut. Answers are measured once and cut by plan, not one per pass. When
// the answers cannot cover excess, their content becomes the note alone. The
// note sits after the cut element when inside it would pass the depth limit.
func cutBytes(nodes []Node, excess int) []Node {
	refs := answerSizes(nodes, jsonSize)
	noteSz := jsonSize([]Node{cutNote()}) + 1
	plan := planCuts(refs, excess, noteSz, 16)
	if len(refs) > 0 {
		if plan == nil {
			plan = map[int]int{}
			for _, r := range refs {
				if r.size > noteSz+16 {
					plan[r.idx] = r.size + noteSz
				}
			}
		}
		if len(plan) > 0 {
			return cutAnswersBy(nodes, plan, func(ks []Node, w int) []Node { k, _ := cutTailBytes(ks, w); return k })
		}
	}
	path := largestElementPath(nodes)
	if path == nil {
		return nodes
	}
	want := excess + noteSz
	var rebuild func(ns []Node, path []int, depth int) []Node
	rebuild = func(ns []Node, path []int, depth int) []Node {
		out := append([]Node(nil), ns...)
		el := out[path[0]].(Element)
		if len(path) > 1 {
			el.K = rebuild(el.K, path[1:], depth+1)
			out[path[0]] = el
			return out
		}
		k, _ := cutTailBytes(el.K, want)
		if len(k) == 0 {
			k = nil
		}
		el.K = k
		if depth+1 > maxViewDepth {
			// the note inside would pass the depth limit: it follows the element
			out[path[0]] = el
			return append(out[:path[0]+1], append([]Node{cutNote()}, out[path[0]+1:]...)...)
		}
		el.K = append(el.K, cutNote())
		out[path[0]] = el
		return out
	}
	return rebuild(nodes, path, 1)
}

// cutAnswersBy cuts plan[i] units (bytes or nodes, by cut) from the end of the i-th outermost answer.
func cutAnswersBy(nodes []Node, plan map[int]int, cut func([]Node, int) []Node) []Node {
	i := 0
	var walk func(ns []Node, depth int) []Node
	walk = func(ns []Node, depth int) []Node {
		out := make([]Node, 0, len(ns)+1)
		for _, v := range ns {
			el, ok := v.(Element)
			if !ok {
				out = append(out, v)
				continue
			}
			if isAnswer(el) {
				cur := i
				i++
				if w, cutIt := plan[cur]; cutIt {
					k := cut(el.K, w)
					if depth+1 > maxViewDepth {
						el.K = k
						out = append(out, el, cutNote())
					} else {
						el.K = append(k, cutNote())
						out = append(out, el)
					}
					continue
				}
				out = append(out, el)
				continue
			}
			el.K = walk(el.K, depth+1)
			if len(el.K) == 0 {
				el.K = nil
			}
			out = append(out, el)
		}
		return out
	}
	return walk(nodes, 1)
}

// largestElementPath is the child-index path to the largest top-level
// element, descending into a child that holds most of it (a wrapper such as
// render.View's root keeps the foot controls and the error slot after its
// body: the cut goes inside the body). Nil when there is no element.
func largestElementPath(nodes []Node) []int {
	var best []int
	bestN := 0
	for i, v := range nodes {
		if _, ok := v.(Element); ok {
			if n := jsonSize(nodes[i : i+1]); n > bestN {
				best, bestN = []int{i}, n
			}
		}
	}
	for best != nil {
		el := elementAt(nodes, best)
		whole := jsonSize(el.K)
		next, nextN := -1, 0
		for i, v := range el.K {
			if c, ok := v.(Element); ok {
				if n := jsonSize([]Node{c}); n > nextN {
					next, nextN = i, n
				}
			}
		}
		if next < 0 || nextN*2 <= whole {
			break
		}
		best = append(best, next)
	}
	return best
}

func elementAt(nodes []Node, path []int) Element {
	var el Element
	for _, i := range path {
		el = nodes[i].(Element)
		nodes = el.K
	}
	return el
}

// keepFoot returns only the foot elements (confirm bar, dialog card, error
// slot) and the elements that hold them: the last resort before whole-view
// halving, which would drop the foot first.
func keepFoot(nodes []Node) []Node {
	out := []Node{}
	for _, v := range nodes {
		el, ok := v.(Element)
		if !ok {
			continue
		}
		if isFoot(el) {
			out = append(out, el)
		} else if hasFoot(el.K) {
			el.K = keepFoot(el.K)
			out = append(out, el)
		}
	}
	return out
}

// cutTailBytes is cutTail by JSON bytes: whole trailing nodes while they fit
// the want or while the element's children cannot cover it (its own tag and
// attributes are the bulk), else the tail inside the last element. It returns
// what is left and the bytes it removed.
func cutTailBytes(ks []Node, want int) ([]Node, int) {
	out := append([]Node(nil), ks...)
	removed := 0
	for len(out) > 0 && removed < want {
		last := out[len(out)-1]
		sz := jsonSize([]Node{last}) - 1 // the node and its comma
		if str, isStr := last.(string); isStr && sz > want-removed {
			// keep a prefix of the text: the end goes, the start stays
			if keep, left := textPrefix(str, sz-(want-removed)); keep != "" {
				out[len(out)-1] = keep
				removed += sz - left
				break
			}
		}
		el, isEl := last.(Element)
		if sz <= want-removed || !isEl || len(el.K) == 0 || jsonSize(el.K) < want-removed {
			out = out[:len(out)-1]
			removed += sz
			continue
		}
		k, r := cutTailBytes(el.K, want-removed)
		if len(k) == 0 {
			k = nil
		}
		el.K = k
		out[len(out)-1] = el
		removed += r
		break
	}
	return out, removed
}

// textPrefix is the longest prefix of s (cut on a rune, ended with an
// ellipsis) whose node and comma take at most room JSON bytes, and that
// size. It returns "" when not even one rune fits.
func textPrefix(s string, room int) (string, int) {
	r := []rune(s)
	size := func(n int) int { return jsonSize([]Node{string(r[:n]) + "…"}) - 1 }
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if size(mid) <= room {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	if lo == 0 {
		return "", 0
	}
	return string(r[:lo]) + "…", size(lo)
}

type countWriter int

func (c *countWriter) Write(p []byte) (int, error) { *c += countWriter(len(p)); return len(p), nil }

// jsonSize is len(json.Marshal(nodes)), counted without building the bytes.
func jsonSize(nodes []Node) int {
	var c countWriter
	if err := json.NewEncoder(&c).Encode(nodes); err != nil {
		return 0
	}
	return int(c) - 1 // Encode ends with a newline
}

// cutText returns nodes with every text node over n runes cut to n. The foot
// is spared: the dialog card's end is where its options are.
func cutText(nodes []Node, n int, foot bool) []Node {
	out := make([]Node, len(nodes))
	for i, v := range nodes {
		switch x := v.(type) {
		case string:
			if r := []rune(x); !foot && len(r) > n {
				v = string(r[:n]) + "…"
			}
		case Element:
			x.K = cutText(x.K, n, foot || isFoot(x))
			if len(x.K) == 0 {
				x.K = nil
			}
			v = x
		}
		out[i] = v
	}
	return out
}
