package launcher

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// This file is the one width helper for the launcher and the panel. Widths are
// terminal cells as ansi.StringWidth counts them: East Asian wide and emoji take
// two, an emoji followed by the variation selector VS16 takes two too (🖥️ is 2),
// combining and format characters take none, and ambiguous characters such as
// ● … → take one. Every cut and every pad goes through these functions.

// displayWidth is the number of terminal cells s takes.
func displayWidth(s string) int {
	return ansi.StringWidth(s)
}

// fit returns s cut or padded with spaces to exactly w cells. A cut ends in "…", and
// a wide character that would straddle the cut is dropped.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if displayWidth(s) <= w {
		return s + strings.Repeat(" ", w-displayWidth(s))
	}
	t := ansi.Truncate(s, w-1, "") + "…"
	return t + strings.Repeat(" ", w-displayWidth(t))
}

// tailFit is fit for paths: when it cuts, it keeps the end of s.
func tailFit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	total := displayWidth(s)
	if total <= w {
		return s + strings.Repeat(" ", w-total)
	}
	// TruncateLeft drops whole characters, so a wide character at the cut can leave
	// the rest one cell too wide. Dropping one more cell then fixes it, and the first
	// cut that fits is the longest tail that does.
	for n := total - (w - 1); n <= total; n++ {
		tail := ansi.TruncateLeft(s, n, "")
		if displayWidth(tail) <= w-1 {
			t := "…" + tail
			return t + strings.Repeat(" ", w-displayWidth(t))
		}
	}
	return "…" + strings.Repeat(" ", w-1)
}

// rjust right-aligns s in w cells, cutting it when it is wider.
func rjust(s string, w int) string {
	if sw := displayWidth(s); sw < w {
		return strings.Repeat(" ", w-sw) + s
	}
	return fit(s, w)
}

// prefix returns the longest leading part of s that is at most w cells wide.
func prefix(s string, w int) string {
	return ansi.Truncate(s, w, "")
}
