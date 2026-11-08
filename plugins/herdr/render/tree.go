package render

import (
	"strings"

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
	for CountNodes(nodes) > max {
		idx, size := largestAnswer(nodes)
		if idx < 0 || size <= 3 {
			break
		}
		excess := CountNodes(nodes) - max
		next := cutAnswer(nodes, idx, excess)
		if CountNodes(next) >= CountNodes(nodes) {
			break
		}
		nodes = next
	}
	budget := max
	return prune(nodes, &budget, 0)
}

func isAnswer(el Element) bool {
	for _, c := range strings.Fields(el.A["class"]) {
		if c == "hc-a" {
			return true
		}
	}
	return false
}

// largestAnswer returns the preorder index (among hc-a elements) and node
// count of the biggest answer, or -1.
func largestAnswer(nodes []Node) (int, int) {
	best, bestN, i := -1, 0, 0
	var walk func(ns []Node)
	walk = func(ns []Node) {
		for _, v := range ns {
			el, ok := v.(Element)
			if !ok {
				continue
			}
			if isAnswer(el) {
				if n := CountNodes(el.K); n > bestN {
					best, bestN = i, n
				}
				i++
			}
			walk(el.K)
		}
	}
	walk(nodes)
	return best, bestN
}

func sizeOf(n Node) int {
	if el, ok := n.(Element); ok {
		return 1 + CountNodes(el.K)
	}
	return 1
}

// cutAnswer rebuilds nodes with the idx-th answer cut by about excess
// nodes from its end, and CutNote added.
func cutAnswer(nodes []Node, idx, excess int) []Node {
	i := 0
	var walk func(ns []Node) []Node
	walk = func(ns []Node) []Node {
		out := make([]Node, len(ns))
		for j, v := range ns {
			el, ok := v.(Element)
			if !ok {
				out[j] = v
				continue
			}
			if isAnswer(el) {
				cur := i
				i++
				if cur == idx {
					k, _ := cutTail(el.K, excess+3) // the note is 3 nodes
					el.K = append(k, Element{T: "p", A: map[string]string{"class": "hc-wait"}, K: []Node{CutNote}})
					out[j] = el
					continue
				}
			}
			el.K = walk(el.K)
			out[j] = el
		}
		return out
	}
	return walk(nodes)
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

// prune keeps the first *budget nodes in document order and nothing deeper
// than maxViewDepth.
func prune(nodes []Node, budget *int, depth int) []Node {
	out := []Node{}
	for _, v := range nodes {
		if *budget <= 0 {
			break
		}
		el, ok := v.(Element)
		if !ok {
			*budget--
			out = append(out, v)
			continue
		}
		if depth >= maxViewDepth {
			continue
		}
		*budget--
		el.K = prune(el.K, budget, depth+1)
		if len(el.K) == 0 {
			el.K = nil
		}
		out = append(out, el)
	}
	return out
}
