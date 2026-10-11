package panel

import (
	"cmp"
	"strings"
	"time"
)

// Screen sizes the panel assumes when the terminal does not report one.
const (
	defaultWidth  = 80
	defaultHeight = 24
)

// View draws the panel: the tab bar, the age of the data, the body, the status
// line and the key hints. It is exactly the terminal's height, and every line is
// exactly its width, so no row keeps stale text.
func (m *Model) View() string {
	w, h := m.width, m.height
	if w <= 0 {
		w = defaultWidth
	}
	if h <= 0 {
		h = defaultHeight
	}
	head := min(2, h)
	foot := min(2, h-head)
	lines := []string{m.tabBar(w), m.ageLine(w)}[:head]
	lines = append(lines, m.body(w, h-head-foot)...)
	lines = append(lines, []string{m.statusLine(w), m.hintLine(w)}[2-foot:]...)
	return strings.Join(lines, "\n")
}

// tabBar draws the tab names with · between them. The active tab is in reverse
// video, or in brackets when the terminal shows no attributes.
func (m *Model) tabBar(w int) string {
	var segs []seg
	for t := range numTabs {
		if t > 0 {
			segs = append(segs, seg{" · ", m.st.dim})
		}
		name := t.String()
		switch {
		case t != m.tab:
			segs = append(segs, seg{name, m.st.base})
		case m.st.plain:
			segs = append(segs, seg{"[" + name + "]", m.st.bold})
		default:
			segs = append(segs, seg{" " + name + " ", m.st.active})
		}
	}
	return line(w, segs...)
}

// ageText says how old the data on this tab is, and whether a refresh is running:
// "cached 42s ago" before the first fetch lands, "updated 3s ago" after.
func (m *Model) ageText() string {
	now := m.now()
	var oldest time.Time
	cached, pending := false, false
	for _, f := range m.tabFeeds() {
		st := m.feeds[f]
		pending = pending || st.pending
		cached = cached || st.fromCache
		if at := m.snap.at(f); !at.IsZero() && (oldest.IsZero() || at.Before(oldest)) {
			oldest = at
		}
	}
	if m.tabUsesPanes() {
		pending = pending || m.panes.pending
		if at := m.panes.at; !at.IsZero() && (oldest.IsZero() || at.Before(oldest)) {
			oldest = at
		}
	}
	parts := []string{"no data yet"}
	if !oldest.IsZero() {
		verb := "updated "
		if cached {
			verb = "cached "
		}
		parts = []string{verb + ago(now.Sub(oldest))}
	}
	if pending {
		parts = append(parts, "refreshing…")
	}
	return strings.Join(parts, " · ")
}

// ageLine is the line under the tab bar.
func (m *Model) ageLine(w int) string {
	return line(w, seg{m.ageText(), m.st.dim})
}

// body draws the current tab in exactly n lines. The restart walk and the profile
// list cover the tab while they are open.
func (m *Model) body(w, n int) []string {
	var lines []string
	switch {
	case m.walk.active:
		lines = m.restartLines(w, n)
	case m.prof.open:
		lines = m.profileLines(w, n)
	case m.tab == tabAgents:
		lines = m.agentsLines(w, n)
	case m.tab == tabAllowances:
		lines = m.allowancesLines(w, n)
	case m.tab == tabUsage:
		lines = m.usageLines(w, n)
	case m.tab == tabRouting:
		lines = m.routingLines(w, n)
	case m.tab == tabSessions:
		lines = m.sessionsLines(w, n)
	}
	out := make([]string, n)
	for i := range out {
		if i < len(lines) {
			out[i] = lines[i]
		} else {
			out[i] = line(w)
		}
	}
	return out
}

// statusLine is the line above the hints. It holds the question on the confirm
// line, the name being typed for a profile, the progress of a write, the last
// result, or a cache error, and is blank otherwise.
func (m *Model) statusLine(w int) string {
	switch {
	case m.ask != nil:
		return line(w, seg{m.ask.text, m.st.warn.Bold(true)})
	case m.prof.open && m.prof.naming:
		return line(w, seg{"Save current setup as: " + clean(m.prof.name) + "▏", m.st.bold})
	case m.writing:
		return line(w, seg{cmp.Or(m.writeText, "working…"), m.st.warn})
	case m.notice != "":
		return line(w, seg{clean(m.notice), m.st.noticeStyle(m.noticeKind)})
	case m.saveErr != "":
		return line(w, seg{"cache not saved: " + clean(m.saveErr), m.st.bad})
	}
	return line(w)
}

// hintLine lists the keys that work in the current state.
func (m *Model) hintLine(w int) string {
	var text string
	switch {
	case m.ask != nil:
		text = cmp.Or(m.ask.hint, "Enter confirm · Esc cancel")
	case m.walk.active && m.walk.done:
		text = "Esc close · q quit"
	case m.prof.open && m.prof.naming:
		text = "type a name · Enter continue · Esc back"
	case m.prof.open:
		text = "↑↓ choose · Enter apply · S save current · Esc close"
	case m.flow == flowPick:
		text = "↑↓ choose · Enter pick · Esc cancel"
	case m.writing:
		text = "working… · Ctrl+C quit"
	case m.tab == tabSessions && m.sess.filtering:
		text = "typing filters · ↑↓ move · Enter open · Ctrl+U clear · Esc done"
	default:
		text = m.tabHints()
	}
	return line(w, seg{text, m.st.dim})
}

// tabHints is the hint line of the current tab, in its normal state.
func (m *Model) tabHints() string {
	const common = "p profiles · 1-5 tabs · Tab next · r refresh · q quit"
	switch m.tab {
	case tabAgents:
		return "↑↓ select · " + common
	case tabAllowances:
		return "↑↓ select · Enter switch · " + common
	case tabSessions:
		return "↑↓ select · Enter open · / filter · p profiles · 1-5 tabs · r refresh · q quit"
	}
	return common
}
