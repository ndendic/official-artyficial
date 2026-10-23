package transcript

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
)

// Screen builds turns from an agent's screen, for agents whose transcript
// the console cannot read. It only knows the turns the console sent: the
// reply is the text that follows the prompt on the screen, with the agent
// UI's chrome (boxes, rules, the input line, status hints) left out.
type Screen struct {
	mu    sync.Mutex
	turns []Turn
	n     int
}

// Sent records a prompt the console sent, and the screen just before.
func (s *Screen) Sent(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	s.turns = append(s.turns, Turn{ID: fmt.Sprintf("screen-%d", s.n), Prompt: prompt})
	if len(s.turns) > MaxTurns {
		s.turns = s.turns[len(s.turns)-MaxTurns:]
	}
}

// Update reads screen text into the last turn. settled marks the turn done.
func (s *Screen) Update(screen string, settled bool) []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.turns) == 0 {
		return nil
	}
	t := &s.turns[len(s.turns)-1]
	if !t.Done {
		reply := AfterPrompt(screen, t.Prompt)
		if reply == "" && len(t.Parts) > 0 {
			// A long reply pushed the prompt off the lines read: go on
			// from the tail of what was read before.
			reply = Continue(screen, t.Parts[0].Text)
		}
		if reply != "" {
			t.Parts = []Part{{Text: reply}}
		}
		t.Done = settled
	}
	return append([]Turn(nil), s.turns...)
}

// Turns returns the known turns.
func (s *Screen) Turns() []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Turn(nil), s.turns...)
}

// AfterPrompt returns the screen text after the last line that holds the
// prompt's first line, with chrome lines removed; "" when the prompt is
// not on the screen.
func AfterPrompt(screen, prompt string) string {
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(prompt), "\n", 2)[0])
	if r := []rune(first); len(r) > 60 {
		first = string(r[:60]) // whole runes: a cut č or emoji never matches
	}
	lines := strings.Split(screen, "\n")
	at := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if first != "" && strings.Contains(lines[i], first) {
			at = i
			break
		}
	}
	if at < 0 {
		return ""
	}
	// Skip the rest of a multi-line prompt.
	rest := lines[at+1:]
	promptLines := strings.Split(strings.TrimSpace(prompt), "\n")
	for _, pl := range promptLines[1:] {
		if len(rest) > 0 && strings.Contains(rest[0], strings.TrimSpace(pl)) {
			rest = rest[1:]
		}
	}
	return replyLines(rest)
}

// replyLines is the reply text in screen lines: chrome left out, up to the
// input box.
func replyLines(rest []string) string {
	var out []string
	for _, l := range rest {
		if IsChrome(l) {
			// The input box and hints close the reply.
			if isInputLine(l) {
				break
			}
			continue
		}
		out = append(out, strings.TrimRight(stripMarker(l), " "))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Continue extends prev, a reply read earlier, with what the screen shows
// after it, when the prompt line is no longer on the screen. It anchors on
// the last distinctive line of prev (12 characters or more) that is on the
// screen; it returns "" when none is, so the caller keeps prev.
func Continue(screen, prev string) string {
	prevLines := strings.Split(prev, "\n")
	lines := strings.Split(screen, "\n")
	for k := len(prevLines) - 1; k >= 0 && k >= len(prevLines)-40; k-- {
		anchor := strings.TrimSpace(prevLines[k])
		if len([]rune(anchor)) < 12 {
			continue
		}
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.TrimSpace(stripMarker(lines[i])) != anchor {
				continue
			}
			head := strings.TrimRight(strings.Join(prevLines[:k+1], "\n"), "\n")
			if tail := replyLines(lines[i+1:]); tail != "" {
				return head + "\n" + tail
			}
			return head
		}
	}
	return ""
}

// IsChrome reports whether a screen line is agent UI rather than reply
// text: box drawing, rules, the input prompt, spinners and key hints.
func IsChrome(l string) bool {
	t := strings.TrimSpace(l)
	if t == "" {
		return false
	}
	if isInputLine(l) {
		return true
	}
	// Status lines: spinners, effort and mode badges.
	if first := []rune(t)[0]; strings.ContainsRune("✻✶✳✢✽✺◐◑◒◓⏵⧉", first) {
		return true
	}
	drawing := 0
	letters := 0
	for _, r := range t {
		switch {
		case r >= 0x2500 && r <= 0x259F: // box drawing and blocks
			drawing++
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			letters++
		}
	}
	if drawing > 0 && drawing*2 >= len([]rune(t))/2 || letters == 0 && drawing > 0 {
		return true
	}
	for _, hint := range []string{"esc to interrupt", "shift+tab to cycle", "? for shortcuts", "ctrl+c to exit", "auto mode on", "bypass permissions"} {
		if strings.Contains(strings.ToLower(t), hint) {
			return true
		}
	}
	return false
}

func isInputLine(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, "❯") || strings.HasPrefix(t, "›") || strings.HasPrefix(t, "│ >") || strings.HasPrefix(t, "> ") && len(t) < 3
}

// stripMarker drops the reply bullet some agents print ("● ", "⏺ ").
func stripMarker(l string) string {
	t := strings.TrimLeft(l, " ")
	for _, m := range []string{"● ", "⏺ ", "• "} {
		if strings.HasPrefix(t, m) {
			return strings.TrimPrefix(t, m)
		}
	}
	return l
}
