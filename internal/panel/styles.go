package panel

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// styles are the attributes the panel draws with. They all come from one
// renderer, so an Ascii profile turns every one of them off. The layout never
// depends on them: the active tab is bracketed and the selected card is marked
// with ▸ when the terminal shows no colour.
type styles struct {
	plain  bool           // the profile emits no SGR at all
	base   lipgloss.Style // no attributes
	dim    lipgloss.Style
	bold   lipgloss.Style
	active lipgloss.Style // the active tab: reverse video
	ok     lipgloss.Style
	warn   lipgloss.Style
	bad    lipgloss.Style
}

// newRenderer returns a renderer for out. With color false (NO_COLOR is set) it
// forces the Ascii profile.
func newRenderer(out io.Writer, color bool) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(out)
	if !color {
		r.SetColorProfile(termenv.Ascii)
	}
	return r
}

// newStyles builds the panel's styles from r.
func newStyles(r *lipgloss.Renderer) styles {
	base := r.NewStyle()
	return styles{
		plain:  r.ColorProfile() == termenv.Ascii,
		base:   base,
		dim:    base.Faint(true),
		bold:   base.Bold(true),
		active: base.Bold(true).Reverse(true),
		ok:     base.Foreground(lipgloss.Color("2")),
		warn:   base.Foreground(lipgloss.Color("3")),
		bad:    base.Foreground(lipgloss.Color("1")),
	}
}

// levelStyle is the style for a usage level: normal text, yellow or red.
func (s styles) levelStyle(l level) lipgloss.Style {
	switch l {
	case levelWarn:
		return s.warn
	case levelBad:
		return s.bad
	}
	return s.base
}

// noticeStyle is the style for the status line's text.
func (s styles) noticeStyle(k noticeKind) lipgloss.Style {
	switch k {
	case noticeOK:
		return s.ok
	case noticeBad:
		return s.bad
	}
	return s.base
}

// seg is a run of text drawn in one style.
type seg struct {
	text  string
	style lipgloss.Style
}

// line lays segs out in exactly w columns. Text past w is cut with "…" and the
// rest is padded with spaces. Widths are measured on the plain text before the
// styles apply, so no escape sequence is ever cut.
func line(w int, segs ...seg) string {
	var b []byte
	used := 0
	for _, s := range segs {
		if used >= w {
			break
		}
		sw := displayWidth(s.text)
		if used+sw <= w {
			b = append(b, s.style.Render(s.text)...)
			used += sw
			continue
		}
		b = append(b, s.style.Render(fit(s.text, w-used))...)
		used = w
	}
	if used < w {
		b = append(b, fit("", w-used)...)
	}
	return string(b)
}
