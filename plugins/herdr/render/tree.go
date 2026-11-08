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
