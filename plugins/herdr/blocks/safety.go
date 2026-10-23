package blocks

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A patch block comes from a transcript, which any agent writes, including
// one steered by text it read. So a block carries inert markup only: core
// refuses script, Datastar expressions and the like (core.ui.caller.patch),
// and this file refuses, before core is asked, what core lets through but
// the console must keep for itself: data-pill-* controls (they call the
// console, and could confirm a send to a crew session), the console's own
// elements and its signals. data-* attributes are allowed only from a short
// list of plain words a stylesheet reads.

// inertData are the data-* attributes a block may carry.
var inertData = map[string]bool{
	"data-variant": true, "data-size": true, "data-state": true, "data-status": true,
	"data-kind": true, "data-tone": true, "data-side": true, "data-align": true,
	"data-orientation": true, "data-label": true, "data-value": true, "data-id": true,
	"data-testid": true,
}

// refusedElements run, load or embed code, restyle the page, or hide markup
// from a parser.
var refusedElements = map[string]bool{
	"script": true, "iframe": true, "frame": true, "frameset": true, "object": true,
	"embed": true, "applet": true, "portal": true, "base": true, "meta": true,
	"link": true, "style": true, "noscript": true, "noembed": true, "noframes": true,
	"xmp": true, "plaintext": true, "form": true,
}

// consoleSelector reports whether a selector reaches the console's own
// markup: #pill-console, the pill, or the console's hc- ids and classes.
func consoleSelector(sel string) bool {
	s := strings.ToLower(sel)
	return strings.Contains(s, "pill") || strings.Contains(s, "hc-") || strings.Contains(s, "hc_")
}

// consoleID reports whether an id is in the console's namespace (hc-*,
// pill*).
func consoleID(id string) bool {
	s := strings.ToLower(strings.TrimSpace(id))
	return strings.HasPrefix(s, "hc-") || strings.HasPrefix(s, "hc_") || strings.HasPrefix(s, "pill")
}

// protectedSignal reports whether a signal belongs to the console.
func protectedSignal(key string) bool {
	k := strings.ToLower(key)
	return strings.HasPrefix(k, "pill") || strings.HasPrefix(k, "hc")
}

// unsafeMarkup names the first thing in body a block may not carry, or "".
func unsafeMarkup(body string) string {
	ctx := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragmentWithOptions(strings.NewReader(body), ctx, html.ParseOptionEnableScripting(false))
	if err != nil {
		return "the HTML cannot be parsed"
	}
	var walk func(n *html.Node) string
	walk = func(n *html.Node) string {
		if n.Type == html.ElementNode {
			if why := unsafeElement(n); why != "" {
				return why
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if why := walk(c); why != "" {
				return why
			}
		}
		return ""
	}
	for _, n := range nodes {
		if why := walk(n); why != "" {
			return why
		}
	}
	return ""
}

func unsafeElement(n *html.Node) string {
	name := strings.ToLower(n.Data)
	if refusedElements[name] {
		return fmt.Sprintf("a block may not carry <%s>", name)
	}
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		switch {
		case strings.HasPrefix(key, "on"):
			return fmt.Sprintf("a block may not carry the attribute %s (it runs code)", key)
		case strings.HasPrefix(key, "data-") && !inertData[key]:
			return fmt.Sprintf("a block may not carry the attribute %s: only plain data attributes (%s) pass", key, "data-variant, data-state, data-status, …")
		case key == "srcdoc" || key == "action" || key == "formaction":
			return fmt.Sprintf("a block may not carry the attribute %s", key)
		case (key == "id" || key == "for" || key == "form") && consoleID(a.Val):
			return fmt.Sprintf("a block may not use the console's own id %q", a.Val)
		}
		v := strings.ToLower(strings.Map(func(r rune) rune {
			if r <= 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, a.Val))
		if strings.Contains(v, "javascript:") || strings.Contains(v, "vbscript:") {
			return fmt.Sprintf("a block may not carry a script URL (in %s)", key)
		}
	}
	return ""
}
