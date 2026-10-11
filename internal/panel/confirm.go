package panel

import (
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

// confirmLine is a question on the status line, and the one component every write
// is confirmed with. Enter runs yes, Esc runs no, and s runs skip where it is set.
// q quits, as it does everywhere outside the text inputs. Only yes starts a write.
type confirmLine struct {
	text string // the whole question, keys included, such as "Apply profile work? Enter/Esc"
	hint string // the keys for the hint line; "" means "Enter confirm · Esc cancel"
	yes  func() tea.Cmd
	no   func() // nil when Esc only closes the line
	skip func() // nil when the question takes no s
}

// keyAsk answers the confirm line with k. The line closes before its action runs,
// so an action may open the next question.
func (m *Model) keyAsk(k string) tea.Cmd {
	a := m.ask
	switch k {
	case "q":
		return tea.Quit
	case "enter":
		m.ask = nil
		if a.yes != nil {
			return a.yes()
		}
	case "esc":
		m.ask = nil
		if a.no != nil {
			a.no()
		}
	case "s":
		if a.skip != nil {
			m.ask = nil
			a.skip()
		}
	}
	return nil
}

// typedRune returns the character a key names, when it is one that can be typed.
// Named keys such as "enter" or "ctrl+u" are not characters.
func typedRune(k string) (rune, bool) {
	r, size := utf8.DecodeRuneInString(k)
	if size == 0 || size != len(k) || !unicode.IsPrint(r) {
		return 0, false
	}
	return r, true
}

// dropLastRune removes the last character of s.
func dropLastRune(s string) string {
	_, size := utf8.DecodeLastRuneInString(s)
	return s[:len(s)-size]
}
