package launcher

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// hints is the bottom line when there is no notice. It is ASCII only, so that
// its width is never in doubt.
const hints = "Up/Down move   PgUp/PgDn page   Enter open   Esc quit"

// Columns of a row, in terminal columns. The title and directory columns take
// whatever the fixed columns leave.
const (
	gap     = "  "
	gapW    = len(gap)
	markW   = 2 // "● " or two spaces
	agentW  = 8
	ageW    = 4 // up to "999d"
	costW   = 9 // up to "$99999.99"
	fixedW  = markW + agentW + gapW + ageW + gapW + costW + gapW
	minRest = 20 // below this many columns, the directory column is dropped
)

// sanitize makes session text safe to draw. Control characters, Esc and newlines
// among them, become spaces, and invisible format characters, which can reorder
// the text on screen, are dropped.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.In(r, unicode.Cf):
			return -1
		case unicode.IsControl(r), unicode.In(r, unicode.Zl, unicode.Zp):
			return ' '
		}
		return r
	}, s)
}

// relTime says how long ago t was: now, then Nm, Nh or Nd. A zero time is "-".
func relTime(now, t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

// formatCost renders a session cost in dollars.
func formatCost(c float64) string {
	switch {
	case c <= 0:
		return "$0.00"
	case c < 0.005:
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", c)
}

// displayTitle is the session title, or the last part of its directory when the
// session has no title.
func displayTitle(s magpie.Session, home string) string {
	t := strings.TrimSpace(sanitize(s.Title))
	if t == "" {
		t = "(untitled)"
		if d := normalizeDir(sanitize(s.CWD), home); d != "" {
			t = filepath.Base(d)
		}
	}
	return t
}

// agentName is the agent column text; "-" when magpie did not name one.
func agentName(s magpie.Session) string {
	return cmp.Or(strings.TrimSpace(sanitize(s.Agent)), "-")
}

// cwdText is the directory column text.
func cwdText(s magpie.Session, home string) string {
	return shortPath(sanitize(s.CWD), home)
}

// layout sizes the title and directory columns for one list.
type layout struct {
	home   string
	width  int
	titleW int
	cwdW   int // 0 when the directory column is dropped
}

// newLayout sizes the columns for rows drawn at width. The directory column takes
// at most two fifths of the space left after the fixed columns, and no more than
// the longest directory in items needs.
func newLayout(width int, items []item, home string) layout {
	rest := max(width-fixedW, 0)
	longest := 0
	for _, it := range items {
		longest = max(longest, displayWidth(cwdText(it.session, home)))
	}
	cwdW := 0
	if rest >= minRest {
		cwdW = min(longest, rest*2/5)
	}
	titleW := rest
	if cwdW > 0 {
		titleW = rest - gapW - cwdW
	}
	return layout{home: home, width: width, titleW: titleW, cwdW: cwdW}
}

// row returns one list row, exactly l.width columns wide. The mark is ● for an
// open session and blank otherwise.
func (l layout) row(it item, now time.Time) string {
	s := it.session
	mark := strings.Repeat(" ", markW)
	if it.open != nil {
		mark = "● "
	}
	cols := []string{
		mark,
		fit(agentName(s), agentW),
		gap,
		rjust(relTime(now, s.Last), ageW),
		gap,
		rjust(formatCost(s.Cost), costW),
		gap,
		fit(displayTitle(s, l.home), l.titleW),
	}
	if l.cwdW > 0 {
		cols = append(cols, gap, tailFit(cwdText(s, l.home), l.cwdW))
	}
	return fit(strings.Join(cols, ""), l.width)
}

// lineKind selects how a line is drawn.
type lineKind int

const (
	kindPlain lineKind = iota
	kindHeader
	kindSelected
	kindNotice // an error or notice; drawn in red when colour is allowed
	kindHint
)

// line is one screen row. Its text is exactly as wide as the screen.
type line struct {
	text string
	kind lineKind
}

// frame lays the screen out top to bottom: a header, the list rows, and a bottom
// line that holds the notice when there is one and the key hints otherwise. Every
// line is exactly m.width columns wide.
func (m *model) frame(now time.Time) []line {
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		return nil
	}
	out := make([]line, h)
	for i := range out {
		out[i] = line{text: fit("", w)}
	}
	out[0] = line{text: m.header(w), kind: kindHeader}
	if h == 1 {
		return out
	}
	out[h-1] = m.footer(w)
	rows := h - 2
	if rows == 0 {
		return out
	}
	switch {
	case !m.loaded:
		out[1] = line{text: fit("loading…", w)}
	case len(m.shown) == 0:
		out[1] = line{text: fit(m.emptyText(), w)}
	default:
		lay := newLayout(w, m.shown, m.home)
		for i := range rows {
			idx := m.top + i
			if idx >= len(m.shown) {
				break
			}
			kind := kindPlain
			if idx == m.cursor {
				kind = kindSelected
			}
			out[1+i] = line{text: lay.row(m.shown[idx], now), kind: kind}
		}
	}
	return out
}

// header is the top line: the title, the filter and the counts. When the filter
// does not fit, it keeps its end, which is where typing happens.
func (m *model) header(w int) string {
	const title = "magpie sessions"
	right := "loading"
	if m.loaded {
		right = fmt.Sprintf("%d of %d, %d open", len(m.shown), len(m.items), m.openCount())
	}
	avail := w - displayWidth(title) - displayWidth(right) - 4
	if avail < 8 {
		return fit(title+"  "+right, w)
	}
	return fit(title+"  "+tailFit("filter: "+sanitize(m.query), avail)+"  "+right, w)
}

// footer is the bottom line: the notice when there is one, else the key hints.
func (m *model) footer(w int) line {
	if m.message != "" {
		return line{text: fit(sanitize(m.message), w), kind: kindNotice}
	}
	return line{text: fit(hints, w), kind: kindHint}
}

// emptyText is the list placeholder when nothing is shown.
func (m *model) emptyText() string {
	if len(m.items) == 0 {
		return fmt.Sprintf("no sessions in the last %d days", m.days)
	}
	return fmt.Sprintf("no session matches %q", m.query)
}
