package panel

import (
	"context"
	"fmt"
	"strconv"
	"time"

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

// sessionCols is the width of each column of the Sessions tab, 0 where a column is left
// out. The agent's column starts with the open mark. The columns come in priority order:
// the directory is the first to go on a narrow screen, then the title.
type sessionCols struct {
	agent, age, cost, title, dir int
}

// sessionColumns sizes the Sessions tab's columns in cw cells, for the rows it lists. The
// title is the flexible column, and the directory follows it.
func sessionColumns(rows []launcher.Item, home string, cw int) sessionCols {
	var marked, titles, costs, dirs []string
	for _, it := range rows {
		s := it.Session()
		marked = append(marked, "  "+launcher.AgentName(s))
		titles = append(titles, launcher.DisplayTitle(s, home))
		costs = append(costs, formatCost(s.Cost))
		dirs = append(dirs, launcher.ShortPath(clean(s.CWD), home))
	}
	// The title keeps its least width in order, but takes the room left after the
	// directory, which has its own width first.
	title := sizedCol("TITLE", titles, 8, 48, true)
	title.want = title.min
	widths := fitColumns(cw, tableGap, []column{
		sizedCol("  AGENT", marked, 7, 16, false), // the open mark, then the agent
		{min: 4, want: 4},                         // "now", "5m", "3h", "2d"
		sizedCol("COST", costs, 4, 9, false),
		title,
		sizedCol("DIR", dirs, 8, 36, false),
	})
	return sessionCols{agent: widths[0], age: widths[1], cost: widths[2], title: widths[3], dir: widths[4]}
}

// sessionHeader is the Sessions tab's column titles, dimmed.
func sessionHeader(cols sessionCols, st styles) []cell {
	return []cell{
		{text: "  AGENT", style: st.dim, width: cols.agent},
		{text: "AGE", style: st.dim, width: cols.age},
		{text: "COST", style: st.dim, width: cols.cost, right: true},
		{text: "TITLE", style: st.dim, width: cols.title},
		{text: "DIR", style: st.dim, width: cols.dir},
	}
}

// sessionLine is one session's row: its open mark, agent, age, cost, title and directory.
// The selected row is bold, and the directory is cut from its front.
func sessionLine(it launcher.Item, selected bool, now time.Time, home string, bw int, cols sessionCols, st styles) string {
	s := it.Session()
	base := st.base
	if selected {
		base = st.bold
	}
	mark := "  "
	if it.Open() != nil {
		mark = "● "
	}
	return markLine(bw, selected, st, []cell{
		{text: mark + launcher.AgentName(s), style: base, width: cols.agent},
		{text: launcher.RelTime(now, s.Last), style: base, width: cols.age},
		{text: formatCost(s.Cost), style: base, width: cols.cost, right: true},
		{text: launcher.DisplayTitle(s, home), style: base, width: cols.title},
		{text: launcher.TailFit(launcher.ShortPath(clean(s.CWD), home), cols.dir), style: st.dim, width: cols.dir},
	})
}

// sessionsLines draws the Sessions tab in h lines: the filter and the counts, the
// failures of the sessions or the pane list, the column titles, then the rows.
func (m *Model) sessionsLines(w, h int) []string {
	st := m.st
	bw, cw := splitWidth(w)
	all := m.sessionItems()
	rows := launcher.FilterItems(all, m.sess.query, m.home)
	open := 0
	for _, it := range all {
		if it.Open() != nil {
			open++
		}
	}
	out := []string{m.sessionsHeader(bw, len(rows), len(all), open)}
	if e := m.feeds[feedSessions].err; e != "" {
		out = append(out, noteLine(bw, st.warn, "sessions: "+clean(e)))
	}
	if e := m.panes.err; e != "" {
		out = append(out, noteLine(bw, st.warn, "herdr agents: "+clean(e)))
	}
	room := h - len(out)
	switch {
	case room <= 0:
		return out
	case len(all) == 0 && m.loading(feedSessions):
		return append(out, noteLine(bw, st.dim, "loading…"))
	case len(all) == 0:
		return append(out, noteLine(bw, st.dim, "no sessions reported; press r to refresh"))
	case len(rows) == 0:
		return append(out, noteLine(bw, st.dim, "no session matches "+strconv.Quote(clean(m.sess.query))))
	}
	cols := sessionColumns(rows, m.home, cw)
	out = append(out, markLine(bw, false, st, sessionHeader(cols, st)))
	room--
	cur := m.selectedSession(len(rows))
	start, end := rowWindow(cur, len(rows), room)
	now := m.now()
	for i := start; i < end; i++ {
		out = append(out, sessionLine(rows[i], i == cur, now, m.home, bw, cols, st))
	}
	return out
}

// sessionsHeader is the Sessions tab's first line, in w columns: the filter on the left,
// with the caret while typing, and the counts after it.
func (m *Model) sessionsHeader(w, shown, total, open int) string {
	st := m.st
	var left string
	switch {
	case m.sess.filtering:
		left = "/" + clean(m.sess.query) + "▏"
	case m.sess.query != "":
		left = "/ " + clean(m.sess.query)
	default:
		left = "/ filter"
	}
	count := fmt.Sprintf("%d sessions", total)
	if shown != total {
		count = fmt.Sprintf("%d of %d sessions", shown, total)
	}
	right := fmt.Sprintf(" · %s · %d open", count, open)
	avail := max(w-gutterW-displayWidth(right), 0)
	if displayWidth(left) > avail {
		left = launcher.TailFit(left, avail) // keeps the typed end of a long filter
	}
	return line(w, seg{"  ", st.base}, seg{left, st.bold}, seg{right, st.dim})
}
