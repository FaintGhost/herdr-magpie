package panel

import (
	"context"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// tickEvery is how often the panel redraws, so that ages and countdowns move.
const tickEvery = 10 * time.Second

// feed is one Source method, and so one part of the panel's data.
type feed int

const (
	feedAgents feed = iota
	feedQuotas
	feedAccounts
	feedProfiles
	feedRequests
	feedAPIUsage
	feedSessions
	numFeeds
)

// allFeeds lists every feed, in order.
func allFeeds() []feed {
	out := make([]feed, 0, numFeeds)
	for f := range numFeeds {
		out = append(out, f)
	}
	return out
}

// feedState is a feed's fetch state. Its data is in the snapshot.
type feedState struct {
	pending   bool   // a fetch is in flight
	fromCache bool   // the data is the cache's, and no fetch has finished since
	err       string // the last fetch's error; "" when it succeeded
}

// paneFeed is the herdr agent list. It is read live and never cached: the Agents
// and Sessions tabs and the restart plan all need the panes as they are now.
type paneFeed struct {
	feedState
	list []herdr.Agent
	at   time.Time // when list was last read; zero when never
}

// noticeKind colours the status line.
type noticeKind int

const (
	noticeInfo noticeKind = iota
	noticeOK
	noticeBad
)

// Model is the panel's bubbletea model. NewModel builds it and Run drives it.
type Model struct {
	dryRun bool // Deps.DryRun
	ctx    context.Context
	src    Source
	act    Actions
	herdr  HerdrView
	cache  CacheStore
	now    func() time.Time
	home   string // the user's home directory, for "~" in paths and filters
	st     styles
	live   bool // set by Run: ticks keep the ages and countdowns current

	width, height int
	tab           tab

	snap  snapshot
	feeds [numFeeds]feedState
	panes paneFeed

	cursor      int    // the selected card on Allowances
	agentCursor int    // the selected agent on Agents
	focusAgent  string // the herdr agent label the panel was opened from
	focusDone   bool   // focusAgent has been looked for in the agent list

	ask  *confirmLine // the question on the status line, when one is open
	flow flowState    // the account picker of a switch
	pick pickState
	prof profileState
	sess sessionsState
	walk restartWalk
	plan *restartPlan // a restart waiting for a fresh pane list

	writing   bool   // a write is in flight; nothing else may write until it lands
	writeText string // what that write is doing, for the status line

	notice     string
	noticeKind noticeKind
	saveErr    string
}

// NewModel builds the panel's model. It reads the cached snapshot at once, so the
// first View already has data. Init starts the refreshes. ctx bounds every fetch
// and every write.
func NewModel(ctx context.Context, d Deps) *Model {
	out := d.Out
	if out == nil {
		out = io.Discard
	}
	return newModel(ctx, d, newRenderer(out, os.Getenv("NO_COLOR") == ""))
}

// newModel is NewModel with the renderer given, so tests can choose a colour profile.
func newModel(ctx context.Context, d Deps, r *lipgloss.Renderer) *Model {
	start, _ := parseTab(d.StartTab)
	home, _ := os.UserHomeDir()
	m := &Model{
		ctx:        ctx,
		src:        d.Source,
		act:        d.Actions,
		herdr:      d.Herdr,
		cache:      d.Cache,
		dryRun:     d.DryRun,
		now:        d.Now,
		home:       home,
		st:         newStyles(r),
		tab:        start,
		focusAgent: d.FocusAgent,
	}
	if m.now == nil {
		m.now = time.Now
	}
	m.snap.Version = snapshotVersion
	m.loadCache()
	m.applyFocus()
	return m
}

// loadCache reads the snapshot. A missing file leaves the panel empty. A file that
// cannot be read is reported on the status line, and one from another version is
// ignored. Neither is fatal.
func (m *Model) loadCache() {
	if m.cache == nil {
		return
	}
	var snap snapshot
	if err := m.cache.Load(snapshotFile, &snap); err != nil {
		m.setNotice(noticeBad, "cache not read: "+err.Error())
		return
	}
	if snap.Version != snapshotVersion {
		return
	}
	m.snap = snap
	for f := range numFeeds {
		m.feeds[f].fromCache = !m.snap.at(f).IsZero()
	}
}

// Init starts a fetch of every feed and, under Run, the ticks. The first frame is
// already drawn from the cache, so nothing waits on these.
func (m *Model) Init() tea.Cmd {
	cmds := m.refresh(allFeeds()...)
	if c := m.refreshPanes(); c != nil {
		cmds = append(cmds, c)
	}
	if m.live {
		cmds = append(cmds, tickCmd())
	}
	return tea.Batch(cmds...)
}

// Update applies one message and returns the command it starts, if any.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		cmd = m.keys(msg)
	case agentsMsg:
		m.land(feedAgents, msg.err)
		if msg.err == nil {
			m.snap.Agents = stampAt(m.now(), msg.v)
			m.applyFocus()
		}
		cmd = m.settle()
	case quotasMsg:
		m.land(feedQuotas, msg.err)
		if msg.err == nil {
			m.snap.Quotas = stampAt(m.now(), msg.v)
		}
		cmd = m.settle()
	case accountsMsg:
		m.land(feedAccounts, msg.err)
		if msg.err == nil {
			m.snap.Accounts = stampAt(m.now(), msg.v)
		}
		cmd = m.settle()
	case profilesMsg:
		m.land(feedProfiles, msg.err)
		if msg.err == nil {
			m.snap.Profiles = stampAt(m.now(), msg.v)
		}
		cmd = m.settle()
	case requestsMsg:
		m.land(feedRequests, msg.err)
		if msg.err == nil {
			m.snap.Requests = stampAt(m.now(), msg.v)
		}
		cmd = m.settle()
	case apiUsageMsg:
		m.land(feedAPIUsage, msg.err)
		if msg.err == nil {
			m.snap.APIUsage = stampAt(m.now(), msg.v)
		}
		cmd = m.settle()
	case sessionsMsg:
		m.land(feedSessions, msg.err)
		if msg.err == nil {
			m.snap.Sessions = stampAt(m.now(), msg.v)
		}
		cmd = m.settle()
	case panesMsg:
		m.landPanes(msg)
	case savedMsg:
		m.saveErr = ""
		if msg.err != nil {
			m.saveErr = msg.err.Error()
		}
	case switchedMsg:
		cmd = m.landSwitch(msg)
	case profileAppliedMsg:
		cmd = m.landApply(msg)
	case profileSavedMsg:
		cmd = m.landSave(msg)
	case openedMsg:
		cmd = m.landOpen(msg)
	case restartedMsg:
		cmd = m.landRestart(msg)
	case tickMsg:
		if m.live {
			cmd = tickCmd()
		}
	}
	return m, cmd
}

// keys applies one key message. Bubble Tea sends the printable characters that
// arrive in one read as a single message, so fast typing or a paste comes as
// one run of runes. Each rune is applied in turn.
func (m *Model) keys(msg tea.KeyMsg) tea.Cmd {
	if msg.Type != tea.KeyRunes || len(msg.Runes) < 2 {
		return m.key(msg.String())
	}
	cmds := make([]tea.Cmd, 0, len(msg.Runes))
	for _, r := range msg.Runes {
		cmds = append(cmds, m.key(string(r)))
	}
	return tea.Batch(cmds...)
}

// key applies one keypress and returns the command it starts, if any. The modes
// are checked in order, and the first one that is open takes the key:
//
//   - ctrl+c quits at any time.
//   - The Sessions filter takes every printable key as text, so digits and q
//     type into the filter. Only Esc, Enter, arrows, Backspace and Ctrl+U act.
//   - While a write is in flight, only the tab keys act, so nothing else can start one.
//   - A confirm line answers Enter (yes), Esc (no), s (skip, where offered) and q (quit).
//   - The restart walk and the profile list are modal: keys they do not use are ignored.
//   - Otherwise the tab keys switch tabs, and the rest belong to the tab or to
//     the global keys q, Esc, r, p and S.
func (m *Model) key(k string) tea.Cmd {
	if k == "ctrl+c" {
		return tea.Quit
	}
	if m.tab == tabSessions && m.sess.filtering {
		return m.keySessionFilter(k)
	}
	if m.writing {
		m.tabKey(k)
		return nil
	}
	if m.ask != nil {
		return m.keyAsk(k)
	}
	if m.walk.active {
		return m.keyWalk(k)
	}
	if m.flow == flowPick {
		return m.keyPick(k)
	}
	if m.prof.open {
		return m.keyProfiles(k)
	}
	if m.tabKey(k) {
		return nil
	}
	switch k {
	case "q", "esc":
		return tea.Quit
	case "r":
		return tea.Batch(m.refreshAll()...)
	case "p":
		m.openProfiles()
		return nil
	case "S":
		m.startSave()
		return nil
	}
	m.tabAction(k)
	return nil
}

// tabAction handles a key that only the current tab uses.
func (m *Model) tabAction(k string) {
	switch m.tab {
	case tabAllowances:
		switch k {
		case "up":
			m.moveCursor(-1)
		case "down":
			m.moveCursor(1)
		case "enter":
			m.startPick()
		}
	case tabAgents:
		switch k {
		case "up":
			m.moveAgentCursor(-1)
		case "down":
			m.moveAgentCursor(1)
		}
	case tabSessions:
		switch k {
		case "up":
			m.moveSessionCursor(-1)
		case "down":
			m.moveSessionCursor(1)
		case "enter":
			m.startOpen()
		case "/":
			m.sess.filtering = true
		}
	}
}

// tabKey switches tabs for the tab keys and reports whether k was one of them.
func (m *Model) tabKey(k string) bool {
	switch k {
	case "tab":
		m.tab = nextTab(m.tab)
	case "shift+tab":
		m.tab = prevTab(m.tab)
	default:
		t, ok := tabForKey(k)
		if !ok {
			return false
		}
		m.tab = t
	}
	return true
}

// moveCursor moves the card cursor by d, staying within the cards.
func (m *Model) moveCursor(d int) {
	n := len(m.cards())
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor = min(max(m.cursor+d, 0), n-1)
}

// moveAgentCursor moves the agent cursor by d, staying within the agents.
func (m *Model) moveAgentCursor(d int) {
	n := len(m.snap.Agents.Data)
	if n == 0 {
		m.agentCursor = 0
		return
	}
	m.agentCursor = min(max(m.agentCursor+d, 0), n-1)
}

// applyFocus selects the agent the panel was opened from, once the agent list is
// known. The list is searched once; a label no agent has leaves nothing selected.
func (m *Model) applyFocus() {
	if m.focusDone || m.focusAgent == "" || len(m.snap.Agents.Data) == 0 {
		return
	}
	m.focusDone = true
	for i, a := range sortedAgents(m.snap.Agents.Data) {
		if herdrLabel(a.ID) == m.focusAgent {
			m.agentCursor = i
			return
		}
	}
}

// refresh starts a fetch for each feed that is not already in flight, and returns
// their commands. Without a Source the feeds say so instead.
func (m *Model) refresh(feeds ...feed) []tea.Cmd {
	var cmds []tea.Cmd
	for _, f := range feeds {
		st := &m.feeds[f]
		switch {
		case st.pending:
		case m.src == nil:
			st.err = "no data source configured"
		default:
			st.pending = true
			cmds = append(cmds, m.fetchCmd(f))
		}
	}
	return cmds
}

// refreshAll starts every feed and the pane list.
func (m *Model) refreshAll() []tea.Cmd {
	cmds := m.refresh(allFeeds()...)
	if c := m.refreshPanes(); c != nil {
		cmds = append(cmds, c)
	}
	return cmds
}

// refreshPanes starts a read of the herdr agent list, unless one is in flight or
// there is no herdr view.
func (m *Model) refreshPanes() tea.Cmd {
	if m.herdr == nil || m.panes.pending {
		return nil
	}
	m.panes.pending = true
	return fetch(m.ctx, func(v []herdr.Agent, err error) tea.Msg { return panesMsg{v, err} }, m.herdr.Agents)
}

// fetchCmd is the command that reads one feed from the Source, bounded by fetchTimeout.
func (m *Model) fetchCmd(f feed) tea.Cmd {
	ctx, s := m.ctx, m.src
	switch f {
	case feedAgents:
		return fetch(ctx, func(v []magpie.AgentInfo, err error) tea.Msg { return agentsMsg{v, err} }, s.Agents)
	case feedQuotas:
		return fetch(ctx, func(v []magpie.Quota, err error) tea.Msg { return quotasMsg{v, err} }, s.Quotas)
	case feedAccounts:
		return fetch(ctx, func(v []magpie.Account, err error) tea.Msg { return accountsMsg{v, err} }, s.Accounts)
	case feedProfiles:
		return fetch(ctx, func(v []magpie.Profile, err error) tea.Msg { return profilesMsg{v, err} }, s.Profiles)
	case feedRequests:
		since := usageSince(m.now())
		return fetch(ctx, func(v []magpie.Request, err error) tea.Msg { return requestsMsg{v, err} },
			func(ctx context.Context) ([]magpie.Request, error) { return s.RecentRequests(ctx, since) })
	case feedAPIUsage:
		return fetch(ctx, func(v *magpie.UsageSummary, err error) tea.Msg { return apiUsageMsg{v, err} }, s.APIUsage)
	case feedSessions:
		return fetch(ctx, func(v []magpie.Session, err error) tea.Msg { return sessionsMsg{v, err} }, s.Sessions)
	}
	return nil
}

// land records that a fetch of f finished. A failure keeps the data the feed had,
// which from then on is only the cache, and is labelled as one.
func (m *Model) land(f feed, err error) {
	st := &m.feeds[f]
	st.pending = false
	st.err = ""
	st.fromCache = false
	if err != nil {
		st.err = err.Error()
		st.fromCache = !m.snap.at(f).IsZero()
	}
}

// landPanes records a read of the herdr agent list. A failure keeps the last list.
// A restart that waits for a fresh list is planned now, on the list that landed.
func (m *Model) landPanes(msg panesMsg) {
	m.panes.pending = false
	m.panes.err = ""
	if msg.err != nil {
		m.panes.err = msg.err.Error()
	} else {
		m.panes.list = msg.v
		m.panes.at = m.now()
	}
	if p := m.plan; p != nil {
		m.plan = nil
		m.startPlan(*p)
	}
}

// settle saves the snapshot once no fetch is in flight, so the cache holds the
// readings of the latest round.
func (m *Model) settle() tea.Cmd {
	if m.cache == nil {
		return nil
	}
	for _, st := range m.feeds {
		if st.pending {
			return nil
		}
	}
	return saveCmd(m.cache, m.snap)
}

// loading reports whether f is being read for the first time, with nothing to show yet.
func (m *Model) loading(f feed) bool {
	return m.feeds[f].pending && m.snap.at(f).IsZero()
}

// tabFeeds are the magpie feeds the current tab shows. The age line describes these.
func (m *Model) tabFeeds() []feed {
	switch m.tab {
	case tabAgents:
		return []feed{feedAgents}
	case tabAllowances:
		return []feed{feedQuotas, feedAccounts}
	case tabUsage:
		return []feed{feedRequests, feedAPIUsage}
	case tabRouting:
		return []feed{feedRequests}
	case tabSessions:
		return []feed{feedSessions}
	}
	return allFeeds()
}

// tabUsesPanes reports whether the current tab shows the herdr panes.
func (m *Model) tabUsesPanes() bool {
	return m.tab == tabAgents || m.tab == tabSessions
}

// setNotice sets the status line's text and colour. An empty text clears it.
func (m *Model) setNotice(k noticeKind, text string) {
	if m.dryRun && k == noticeOK {
		text = "[dry-run] " + text
	}
	m.notice, m.noticeKind = text, k
}
