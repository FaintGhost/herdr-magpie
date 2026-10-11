package launcher

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// defaultDays is the session window when Deps.Days is not positive. It matches
// magpie.Sessions.
const defaultDays = 14

// openPane is the herdr pane that holds a session, and that pane's workspace.
type openPane struct {
	paneID      string
	workspaceID string
}

// item is one magpie session and the pane that has it open, if any.
type item struct {
	session magpie.Session
	open    *openPane
}

// buildItems pairs sessions with the agent panes that hold them and returns the
// result newest first. A session matches a pane when their ids agree ignoring
// case. When two panes hold one session, the first one listed wins.
func buildItems(sessions []magpie.Session, agents []herdr.Agent) []item {
	panes := make(map[string]openPane, len(agents))
	for _, a := range agents {
		k := herdr.SessionKey(a.Session)
		if k == "" {
			continue
		}
		if _, seen := panes[k]; !seen {
			panes[k] = openPane{paneID: a.PaneID, workspaceID: a.WorkspaceID}
		}
	}
	items := make([]item, 0, len(sessions))
	for _, s := range sessions {
		it := item{session: s}
		if p, ok := panes[strings.ToLower(s.ID)]; ok {
			it.open = new(p)
		}
		items = append(items, it)
	}
	slices.SortFunc(items, byRecency)
	return items
}

// byRecency orders items newest first. Ties fall back to agent and id, so the
// order is total and stable across loads.
func byRecency(a, b item) int {
	return cmp.Or(
		b.session.Last.Compare(a.session.Last),
		strings.Compare(a.session.Agent, b.session.Agent),
		strings.Compare(a.session.ID, b.session.ID),
	)
}

// filterItems keeps the items whose agent, title or directory contains every
// whitespace-separated term of query, ignoring case. An empty query keeps all.
func filterItems(items []item, query, home string) []item {
	terms := slices.Collect(strings.FieldsSeq(strings.ToLower(query)))
	if len(terms) == 0 {
		return items
	}
	var out []item
	for _, it := range items {
		if containsAll(strings.ToLower(searchText(it.session, home)), terms) {
			out = append(out, it)
		}
	}
	return out
}

// containsAll reports whether s contains every term.
func containsAll(s string, terms []string) bool {
	for _, t := range terms {
		if !strings.Contains(s, t) {
			return false
		}
	}
	return true
}

// searchText is what filter terms are matched against: the agent, the title, and
// the directory both as stored and with the home directory shown as ~.
func searchText(s magpie.Session, home string) string {
	return strings.Join([]string{s.Agent, s.Title, s.CWD, shortPath(s.CWD, home)}, "\n")
}

// model is the launcher's state: the sessions, the filter and the cursor. It does
// no I/O, so the key handling and the layout can be tested directly.
type model struct {
	home   string
	days   int
	width  int
	height int
	rows   int // list rows on screen

	loaded   bool          // sessions and agents have both answered
	agents   []herdr.Agent // herdr agent panes, for choosing a workspace
	agentsOK bool          // agents loaded; without them a new tab could duplicate an open session
	items    []item        // every session, newest first
	query    string        // the filter as typed
	shown    []item        // items matching query
	cursor   int           // index into shown
	top      int           // index into shown of the first row on screen
	message  string        // notice or error for the bottom line
}

// newModel returns an empty model that shows sessions from the last days days.
func newModel(home string, days int) *model {
	return &model{home: home, days: days}
}

// setLoaded installs the loaded sessions and agents. agentsOK is false when the
// agent list could not be read.
func (m *model) setLoaded(sessions []magpie.Session, agents []herdr.Agent, agentsOK bool) {
	m.loaded = true
	m.agents = agents
	m.agentsOK = agentsOK
	m.items = buildItems(sessions, agents)
	m.refilter()
}

// refilter applies the query and puts the cursor on the first match.
func (m *model) refilter() {
	m.shown = filterItems(m.items, m.query, m.home)
	m.cursor = 0
	m.top = 0
}

// resize records the terminal size and keeps the cursor on screen.
func (m *model) resize(width, height int) {
	m.width, m.height = width, height
	m.rows = max(height-2, 0)
	m.scroll()
}

// move shifts the cursor by n rows, stopping at the first and last match.
func (m *model) move(n int) {
	m.cursor = min(max(m.cursor+n, 0), max(len(m.shown)-1, 0))
	m.scroll()
}

// scroll moves the list window so that the cursor is on screen.
func (m *model) scroll() {
	if m.rows <= 0 {
		return
	}
	m.top = min(m.top, m.cursor)
	if m.cursor >= m.top+m.rows {
		m.top = m.cursor - m.rows + 1
	}
	m.top = max(0, min(m.top, len(m.shown)-m.rows))
}

// selected returns the highlighted item.
func (m *model) selected() (item, bool) {
	if m.cursor >= len(m.shown) {
		return item{}, false
	}
	return m.shown[m.cursor], true
}

// openCount is the number of sessions that have a pane open.
func (m *model) openCount() int {
	n := 0
	for _, it := range m.items {
		if it.open != nil {
			n++
		}
	}
	return n
}

// markOpen records that session id now has pane open, in the full list and in the
// filtered one. The launcher calls it after a launch that leaves it on screen, so
// that Enter on the session focuses the pane instead of resuming it again.
func (m *model) markOpen(id string, pane openPane) {
	for _, items := range [][]item{m.items, m.shown} {
		for i := range items {
			if items[i].session.ID == id {
				items[i].open = new(pane)
			}
		}
	}
}

// action is what a keypress asks the loop to do next.
type action int

const (
	actNone action = iota
	actQuit        // leave without opening anything
	actPick        // open the selected session
)

// press applies one key to the model. A notice lasts until the next key.
func (m *model) press(k key) action {
	if k.kind != keyEnter {
		m.message = ""
	}
	switch k.kind {
	case keyEsc, keyCtrlC:
		return actQuit
	case keyEnter:
		return actPick
	case keyUp:
		m.move(-1)
	case keyDown:
		m.move(1)
	case keyPageUp:
		m.move(-max(m.rows, 1))
	case keyPageDown:
		m.move(max(m.rows, 1))
	case keyBackspace:
		if m.query != "" {
			_, size := utf8.DecodeLastRuneInString(m.query)
			m.query = m.query[:len(m.query)-size]
			m.refilter()
		}
	case keyRune:
		m.query += string(k.r)
		m.refilter()
	}
	return actNone
}
