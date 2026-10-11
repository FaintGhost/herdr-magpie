package panel

import (
	"context"
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/launcher"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// sessionsState is the Sessions tab's own state. The rows come from the snapshot
// and the pane list; the launcher's rules decide which rows show and in what order.
type sessionsState struct {
	query     string // the filter, as typed
	filtering bool   // typing goes to the filter; see key
	cursor    int    // the selected row
}

// openedMsg is the result of an OpenSession call. focus says the session was one a
// pane already held, so the call only focused that pane.
type openedMsg struct {
	session magpie.Session
	focus   bool
	notice  string
	err     error
}

// sessionItems are every session, newest first, each with the pane that holds it.
func (m *Model) sessionItems() []launcher.Item {
	return launcher.BuildItems(m.snap.Sessions.Data, m.panes.list)
}

// sessionRows are the sessions the tab shows: sessionItems that match the filter.
func (m *Model) sessionRows() []launcher.Item {
	return launcher.FilterItems(m.sessionItems(), m.sess.query, m.home)
}

// moveSessionCursor moves the selected row by d, staying within the rows.
func (m *Model) moveSessionCursor(d int) {
	n := len(m.sessionRows())
	m.sess.cursor = min(max(m.sess.cursor+d, 0), max(n-1, 0))
}

// selectedSession is the index of the selected row, kept in range for n rows.
func (m *Model) selectedSession(n int) int {
	return min(max(m.sess.cursor, 0), max(n-1, 0))
}

// keySessionFilter handles a key while the filter takes the keyboard. Every
// printable key is text, so digits and q type into the filter. Esc leaves the
// filter and keeps the text; Enter leaves it and opens the selected session.
func (m *Model) keySessionFilter(k string) tea.Cmd {
	s := &m.sess
	switch k {
	case "esc":
		s.filtering = false
	case "enter":
		s.filtering = false
		m.startOpen()
	case "up":
		m.moveSessionCursor(-1)
	case "down":
		m.moveSessionCursor(1)
	case "backspace":
		if s.query != "" {
			s.query = dropLastRune(s.query)
			s.cursor = 0
		}
	case "ctrl+u":
		s.query, s.cursor = "", 0
	default:
		if r, ok := typedRune(k); ok {
			s.query += string(r)
			s.cursor = 0
		}
	}
	return nil
}

// startOpen asks to open the selected row's session.
func (m *Model) startOpen() {
	rows := m.sessionRows()
	if len(rows) == 0 {
		return
	}
	m.askOpen(rows[m.selectedSession(len(rows))])
}

// askOpen puts "Open <agent>: <title>?" on the confirm line. Enter opens the session;
// Esc drops it. A session that no pane holds is refused while the pane list is
// unknown, since opening it could resume a session that a pane already runs.
func (m *Model) askOpen(it launcher.Item) {
	if m.writing {
		m.setNotice(noticeInfo, "wait for the change in progress to finish")
		return
	}
	if it.Open() == nil && !m.panesKnown() {
		m.setNotice(noticeBad, m.panesUnknownWhy())
		return
	}
	s, focus := it.Session(), it.Open() != nil
	m.ask = &confirmLine{
		text: fmt.Sprintf("Open %s: %s? Enter/Esc", launcher.AgentName(s), launcher.DisplayTitle(s, m.home)),
		yes:  func() tea.Cmd { return m.beginOpen(s, focus) },
	}
}

// panesKnown reports whether the pane list was read and the last read succeeded.
func (m *Model) panesKnown() bool {
	return m.panes.err == "" && !m.panes.at.IsZero()
}

// panesUnknownWhy says why the pane list cannot be used.
func (m *Model) panesUnknownWhy() string {
	switch {
	case m.herdr == nil:
		return "herdr is not configured, so the session is not opened"
	case m.panes.err != "":
		return "herdr agents unavailable, so the session is not opened in a new tab"
	}
	return "herdr agents still loading, so the session is not opened yet"
}

// beginOpen starts OpenSession, unless a write is already in flight.
func (m *Model) beginOpen(s magpie.Session, focus bool) tea.Cmd {
	verb := "opening "
	if focus {
		verb = "focusing "
	}
	if !m.beginWrite(verb + launcher.AgentName(s) + ": " + launcher.DisplayTitle(s, m.home) + "…") {
		return nil
	}
	return openCmd(m.ctx, m.act, s, focus)
}

// openCmd runs OpenSession off the event loop, bounded by writeTimeout.
func openCmd(ctx context.Context, act Actions, s magpie.Session, focus bool) tea.Cmd {
	return func() tea.Msg {
		notice, err := runWrite(ctx, func(ctx context.Context) (string, error) {
			if act == nil {
				return "", notConfigured("opening")
			}
			return act.OpenSession(ctx, s)
		})
		return openedMsg{session: s, focus: focus, notice: notice, err: err}
	}
}

// landOpen records the result of OpenSession. The pane list is read again, so the
// open marks match the pane that now runs the session.
func (m *Model) landOpen(msg openedMsg) tea.Cmd {
	m.writing = false
	label := launcher.AgentName(msg.session) + ": " + launcher.DisplayTitle(msg.session, m.home)
	switch {
	case msg.err != nil:
		m.setNotice(noticeBad, "open failed: "+msg.err.Error())
	case msg.notice != "":
		m.setNotice(noticeInfo, clean(msg.notice))
	case msg.focus:
		m.setNotice(noticeOK, "focused "+label)
	default:
		m.setNotice(noticeOK, "opened "+label)
	}
	return m.refreshPanes()
}

// rowWindow is the rows on screen: room of them, ending at the selected row, so that
// the selection is always visible.
func rowWindow(selected, n, room int) (start, end int) {
	if n <= 0 || room <= 0 {
		return 0, 0
	}
	start = max(selected-room+1, 0)
	return start, min(start+room, n)
}

// sessionsLines draws the Sessions tab in h lines: the filter and the counts, the
// failures of the sessions or the pane list, then the rows.
func (m *Model) sessionsLines(w, h int) []string {
	st := m.st
	all := m.sessionItems()
	rows := launcher.FilterItems(all, m.sess.query, m.home)
	open := 0
	for _, it := range all {
		if it.Open() != nil {
			open++
		}
	}
	out := []string{m.sessionsHeader(w, len(rows), len(all), open)}
	if e := m.feeds[feedSessions].err; e != "" {
		out = append(out, line(w, seg{"sessions: " + clean(e), st.warn}))
	}
	if e := m.panes.err; e != "" {
		out = append(out, line(w, seg{"herdr agents: " + clean(e), st.warn}))
	}
	room := h - len(out)
	switch {
	case room <= 0:
		return out
	case len(all) == 0 && m.loading(feedSessions):
		return append(out, line(w, seg{"loading…", st.dim}))
	case len(all) == 0:
		return append(out, line(w, seg{"no sessions reported; press r to refresh", st.dim}))
	case len(rows) == 0:
		return append(out, line(w, seg{"no session matches " + strconv.Quote(clean(m.sess.query)), st.dim}))
	}
	cur := m.selectedSession(len(rows))
	start, end := rowWindow(cur, len(rows), room)
	lay := launcher.NewLayout(w-2, rows, m.home)
	now := m.now()
	for i := start; i < end; i++ {
		gutter, style := "  ", st.base
		if i == cur {
			gutter, style = "▸ ", st.active
		}
		out = append(out, line(w, seg{gutter, st.base}, seg{lay.Row(rows[i], now), style}))
	}
	return out
}

// sessionsHeader is the Sessions tab's first line: the filter on the left, with the
// caret while typing, and the counts on the right.
func (m *Model) sessionsHeader(w, shown, total, open int) string {
	st := m.st
	var left string
	switch {
	case m.sess.filtering:
		left = "/" + clean(m.sess.query) + "▏"
	case m.sess.query != "":
		left = "filter: " + clean(m.sess.query)
	default:
		left = "press / to filter"
	}
	right := fmt.Sprintf("%d of %d · %d open", shown, total, open)
	avail := w - displayWidth(right) - 2
	if avail < 0 {
		return line(w, seg{right, st.dim})
	}
	return line(w, seg{launcher.TailFit(left, avail), st.bold}, seg{"  " + right, st.dim})
}
