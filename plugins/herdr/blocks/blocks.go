// Package blocks finds patch blocks in an agent's Markdown reply.
//
// A patch block is a fenced code block whose info string names a patch:
//
//	```patch-elements selector="#aside" mode="append"
//	<article id="x">…</article>
//	```
//	```patch-signals
//	{"theme":"dark"}
//	```
//
// The package is pure: it parses and validates, and never applies. A block
// is applied once its closing fence has arrived (Block.Closed), keyed by
// (pane, turn, Block.Index) so a re-read never applies it twice.
package blocks

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Kinds of patch block.
const (
	Elements = "patch-elements"
	Signals  = "patch-signals"
)

// MaxBody is the largest block body accepted, in bytes.
const MaxBody = 64 << 10

// Modes are the elements patch modes, as core.ui.caller.patch takes them.
var Modes = []string{"outer", "inner", "append", "prepend", "before", "after", "replace", "remove"}

// Block is one patch block.
type Block struct {
	// Index is the block's position among the patch blocks of the text
	// (0-based, counting from the Offset given to Parse).
	Index    int
	Kind     string // Elements or Signals
	Selector string
	Mode     string // empty in the source means outer
	Region   string
	Body     string
	// Raw is the block as written, fences included.
	Raw string
	// Closed is true once the closing fence has arrived.
	Closed bool
	// Err is why the block cannot be applied; empty when it can.
	Err string
}

// EffectiveMode is the mode the patch uses: outer when none was written.
func (b Block) EffectiveMode() string {
	if b.Mode == "" {
		return "outer"
	}
	return b.Mode
}

// Summary is the short label of a chip: "patch-elements #aside · append".
func (b Block) Summary() string {
	if b.Kind == Signals {
		body := strings.Join(strings.Fields(b.Body), " ")
		if len(body) > 60 {
			body = body[:57] + "…"
		}
		return Signals + " " + body
	}
	target := b.Selector
	if target == "" && b.Region == "" {
		if id := topLevelID(b.Body); id != "" {
			target = "#" + id
		}
	}
	s := Elements
	if target == "" && b.Region != "" {
		target = "(root)"
	}
	if target != "" {
		s += " " + target
	}
	s += " · " + b.EffectiveMode()
	if b.Region != "" {
		s += " · region " + b.Region
	}
	return s
}

// Segment is a run of plain text or one patch block, in reply order.
type Segment struct {
	Text  string
	Block *Block
}

var (
	openFence  = regexp.MustCompile("^ {0,3}```(patch-elements|patch-signals)(?:[ \t]+(.*))?$")
	otherFence = regexp.MustCompile("^ {0,3}(```+|~~~+)")
	closeFence = regexp.MustCompile("^ {0,3}```[ \t]*$")
	attrRe     = regexp.MustCompile(`^([a-z_]+)="([^"]*)"`)
	regionName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
)

// Parse splits text into plain-text segments and patch blocks. offset is
// the Index of the first block, so a turn made of several text parts keeps
// one running index. Parse validates every closed block (Block.Err).
func Parse(text string, offset int) []Segment {
	lines := strings.SplitAfter(text, "\n")
	var out []Segment
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			out = append(out, Segment{Text: plain.String()})
			plain.Reset()
		}
	}
	index := offset
	other := "" // the fence of a plain code block we are inside
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r\n")
		if other != "" {
			plain.WriteString(lines[i])
			if t := strings.TrimLeft(line, " "); strings.HasPrefix(t, other) && strings.TrimSpace(strings.TrimLeft(t, other[:1])) == "" {
				other = ""
			}
			continue
		}
		m := openFence.FindStringSubmatch(line)
		if m == nil {
			if f := otherFence.FindStringSubmatch(line); f != nil {
				other = f[1]
			}
			plain.WriteString(lines[i])
			continue
		}
		flush()
		b := &Block{Index: index, Kind: m[1]}
		index++
		var raw, body strings.Builder
		raw.WriteString(lines[i])
		attrErr := parseAttrs(b, m[2])
		j := i + 1
		for ; j < len(lines); j++ {
			l := strings.TrimRight(lines[j], "\r\n")
			if closeFence.MatchString(l) {
				raw.WriteString(lines[j])
				b.Closed = true
				break
			}
			raw.WriteString(lines[j])
			body.WriteString(lines[j])
		}
		b.Raw = strings.TrimRight(raw.String(), "\n")
		b.Body = strings.TrimSuffix(body.String(), "\n")
		if b.Closed {
			if attrErr != "" {
				b.Err = attrErr
			} else {
				b.Err = validate(*b)
			}
		}
		out = append(out, Segment{Block: b})
		i = j
	}
	flush()
	return out
}

// Count is the number of patch blocks in text, closed or not.
func Count(text string) int {
	n := 0
	for _, s := range Parse(text, 0) {
		if s.Block != nil {
			n++
		}
	}
	return n
}

func parseAttrs(b *Block, s string) string {
	s = strings.TrimSpace(s)
	seen := map[string]bool{}
	for s != "" {
		m := attrRe.FindStringSubmatch(s)
		if m == nil {
			return fmt.Sprintf("cannot read the attributes %q: write name=\"value\" pairs", s)
		}
		key, value := m[1], m[2]
		if seen[key] {
			return "the attribute " + key + " is written twice"
		}
		seen[key] = true
		switch {
		case key == "region":
			b.Region = value
		case key == "selector" && b.Kind == Elements:
			b.Selector = value
		case key == "mode" && b.Kind == Elements:
			b.Mode = value
		default:
			return "unknown attribute " + key + " for " + b.Kind
		}
		s = strings.TrimSpace(s[len(m[0]):])
	}
	return ""
}

func validate(b Block) string {
	if len(b.Body) > MaxBody {
		return fmt.Sprintf("the block is %d KB, over the 64 KB limit", (len(b.Body)+1023)/1024)
	}
	if b.Region != "" && !regionName.MatchString(b.Region) {
		return "the region name " + b.Region + " is not a Patch Region name"
	}
	if b.Kind == Signals {
		if b.Region != "" {
			return "a patch-signals block takes no region"
		}
		var v any
		if err := json.Unmarshal([]byte(b.Body), &v); err != nil {
			return "the signals are not valid JSON: " + err.Error()
		}
		object, ok := v.(map[string]any)
		if !ok {
			return "the signals must be one JSON object"
		}
		for k := range object {
			if protectedSignal(k) {
				return "the signal " + k + " belongs to the console; a block may not set it"
			}
		}
		return ""
	}
	if b.Region == "console" || consoleSelector(b.Selector) {
		return "the console is not a place for a block: patch the page (#aside, #main, region layer …)"
	}
	if b.Mode != "" && !validMode(b.Mode) {
		return "unknown mode " + b.Mode + " (use one of " + strings.Join(Modes, ", ") + ")"
	}
	if strings.TrimSpace(b.Selector) == "" && b.Region != "" {
		// The root of a Patch Region: core takes inner, append or prepend
		// there (outer becomes append).
		switch b.EffectiveMode() {
		case "inner", "append", "prepend", "outer":
		default:
			return "mode " + b.Mode + " does not work on the root of region " + b.Region + ": use inner, append or prepend"
		}
	} else if strings.TrimSpace(b.Selector) == "" {
		if b.EffectiveMode() != "outer" {
			return "a selector is required for mode " + b.Mode
		}
		if topLevelID(b.Body) == "" {
			return "a selector is required: the HTML has no single top-level element with an id"
		}
	}
	if strings.TrimSpace(b.Body) == "" && b.EffectiveMode() != "remove" {
		return "the block has no HTML"
	}
	if strings.TrimSpace(b.Selector) == "" && b.Region == "" && consoleID(topLevelID(b.Body)) {
		return "the console is not a place for a block: patch the page (#aside, #main, region layer …)"
	}
	return unsafeMarkup(b.Body)
}

func validMode(m string) bool {
	for _, v := range Modes {
		if v == m {
			return true
		}
	}
	return false
}

var (
	firstTag = regexp.MustCompile(`(?s)^\s*<([a-zA-Z][a-zA-Z0-9-]*)([^>]*)>`)
	idAttr   = regexp.MustCompile(`(?i)(?:^|\s)id\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s"'>]+))`)
)

// topLevelID returns the id of the HTML's one top-level element, or "" when
// the HTML does not start with an element carrying an id, or holds more than
// one top-level element.
func topLevelID(html string) string {
	m := firstTag.FindStringSubmatch(html)
	if m == nil {
		return ""
	}
	idm := idAttr.FindStringSubmatch(m[2])
	if idm == nil {
		return ""
	}
	id := idm[1] + idm[2] + idm[3]
	if !singleRoot(html, m[1]) {
		return ""
	}
	return id
}

// singleRoot reports whether html is one element named tag (a cheap check:
// the text ends with its closing tag, or the element is void/self-closed
// and nothing follows).
func singleRoot(html, tag string) bool {
	t := strings.TrimSpace(html)
	closeTag := "</" + strings.ToLower(tag) + ">"
	if strings.HasSuffix(strings.ToLower(t), closeTag) {
		// depth walk over this tag name only
		low := strings.ToLower(t)
		open := "<" + strings.ToLower(tag)
		depth := 0
		for i := 0; i < len(low); {
			switch {
			case strings.HasPrefix(low[i:], closeTag):
				depth--
				i += len(closeTag)
				if depth == 0 && strings.TrimSpace(low[i:]) != "" {
					return false
				}
			case strings.HasPrefix(low[i:], open) && i+len(open) < len(low) && strings.ContainsRune(" \t\n>/", rune(low[i+len(open)])):
				depth++
				i += len(open)
			default:
				i++
			}
		}
		return depth == 0
	}
	end := strings.Index(t, ">")
	return end == len(t)-1
}
