package panel

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/width"
)

// barCells is how many cells a usage bar draws.
const barCells = 10

// runeWidth is the number of terminal columns r takes. East Asian wide and
// fullwidth characters take two, and combining and format characters take none.
// It is the rule internal/launcher uses, so the two screens agree on Chinese text.
func runeWidth(r rune) int {
	if unicode.In(r, unicode.Mn, unicode.Me, unicode.Cf) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// displayWidth is the number of terminal columns s takes.
func displayWidth(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// fit returns s cut or padded with spaces to exactly w columns. A cut ends in "…".
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := displayWidth(s)
	if sw <= w {
		return s + strings.Repeat(" ", w-sw)
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := runeWidth(r)
		if used+rw > w-1 {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String() + strings.Repeat(" ", w-used-1)
}

// rjust right-aligns s in w columns. A s wider than w is cut.
func rjust(s string, w int) string {
	if sw := displayWidth(s); sw < w {
		return strings.Repeat(" ", w-sw) + s
	}
	return fit(s, w)
}

// clean makes external text fit for one screen line. Control characters, newlines
// and escape bytes among them, and line or paragraph separators become spaces;
// format characters, which can reorder the text, are dropped; runs of spaces collapse.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.In(r, unicode.Cf):
			return -1
		case unicode.IsControl(r), unicode.In(r, unicode.Zl, unicode.Zp):
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// progressBar draws used, a percentage, as cells cells: ▰ for the used part and
// ▱ for the rest. Values outside 0 to 100 are clamped.
func progressBar(used float64, cells int) string {
	filled := 0
	switch {
	case math.IsNaN(used), used <= 0:
	case used >= 100:
		filled = cells
	default:
		filled = int(math.Round(used / 100 * float64(cells)))
	}
	return strings.Repeat("▰", filled) + strings.Repeat("▱", cells-filled)
}

// level ranks a usage percentage for its colour.
type level int

const (
	levelOK level = iota
	levelWarn
	levelBad
)

// usageLevel ranks a used percentage: under 70 is normal, 70 to 90 warns, and
// above 90 is bad.
func usageLevel(used float64) level {
	switch {
	case used > 90:
		return levelBad
	case used >= 70:
		return levelWarn
	}
	return levelOK
}

// formatDuration says a span in its two largest units: "<1m", "45m", "2h14m",
// "3d 4h".
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
	return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
}

// resetText says when a usage window starts again, given now. A reset time that
// is already behind now means the reading is older than that, so it says so. A
// zero time is unknown and gives "".
func resetText(now, at time.Time) string {
	if at.IsZero() {
		return ""
	}
	d := at.Sub(now)
	if d < 0 {
		return "reset passed"
	}
	return "resets in " + formatDuration(d)
}

// ago says how long before now a reading was taken, in one unit.
func ago(d time.Duration) string {
	switch {
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	}
	return fmt.Sprintf("%dh ago", int(d/time.Hour))
}

// formatCost is a dollar amount. A small non-zero amount reads "<$0.01" rather
// than "$0.00", which would suggest nothing was spent.
func formatCost(usd float64) string {
	if usd > 0 && usd < 0.005 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", usd)
}

// percentile returns the p-th percentile (0 to 100) of vals, interpolating
// linearly between the two nearest ranks, so 50 is the median. ok is false when
// vals is empty. vals is not changed.
func percentile(vals []float64, p float64) (v float64, ok bool) {
	if len(vals) == 0 {
		return 0, false
	}
	s := slices.Clone(vals)
	slices.Sort(s)
	p = min(max(p, 0), 100)
	h := float64(len(s)-1) * p / 100
	lo := int(math.Floor(h))
	hi := int(math.Ceil(h))
	return s[lo] + (h-float64(lo))*(s[hi]-s[lo]), true
}
