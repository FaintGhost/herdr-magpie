package panel

import (
	"context"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// noticeKind colours the status line.
type noticeKind int

const (
	noticeInfo noticeKind = iota
	noticeOK
	noticeBad
)

// Model is the panel's bubbletea model. NewModel builds it and Run drives it.
type Model struct {
	ctx   context.Context
	src   Source
	act   Actions
	herdr HerdrView // carried for batch 2; the panel does not call it yet
	cache CacheStore
	now   func() time.Time
	st    styles
	live  bool // set by Run: ticks keep the ages and countdowns current

	width, height int
	tab           tab

	snap  snapshot
	feeds [numFeeds]feedState

	cursor int // the selected card on Allowances

	flow    flowState
	pick    pickState
	writing bool // a SwitchAccount call is in flight

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
	m := &Model{
		ctx:   ctx,
		src:   d.Source,
		act:   d.Actions,
		herdr: d.Herdr,
		cache: d.Cache,
		now:   d.Now,
		st:    newStyles(r),
		tab:   start,
	}
	if m.now == nil {
		m.now = time.Now
	}
	m.snap.Version = snapshotVersion
	m.loadCache()
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
	case savedMsg:
		m.saveErr = ""
		if msg.err != nil {
			m.saveErr = msg.err.Error()
		}
	case switchedMsg:
		m.writing = false
		if msg.err != nil {
			m.setNotice(noticeBad, "switch failed: "+msg.err.Error())
		} else {
			m.setNotice(noticeOK, "switched "+agentLabel(msg.agent, m.snap.Agents.Data)+" to "+msg.user)
		}
		// The signed-in account and the windows have changed, so read them again.
		cmd = tea.Batch(m.refresh(feedAccounts, feedQuotas)...)
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

// key applies one keypress and returns the command it starts, if any. ctrl+c quits
// at any time. q and Esc quit too, except that Esc first cancels an open switch,
// and nothing quits while a switch is being written.
func (m *Model) key(k string) tea.Cmd {
	if k == "ctrl+c" {
		return tea.Quit
	}
	if m.writing {
		m.tabKey(k)
		return nil
	}
	switch m.flow {
	case flowPick:
		return m.keyPick(k)
	case flowConfirm:
		return m.keyConfirm(k)
	}
	if m.tabKey(k) {
		return nil
	}
	switch k {
	case "q", "esc":
		return tea.Quit
	case "r":
		return tea.Batch(m.refresh(allFeeds()...)...)
	}
	if m.tab == tabAllowances {
		switch k {
		case "up":
			m.moveCursor(-1)
		case "down":
			m.moveCursor(1)
		case "enter":
			m.startPick()
		}
	}
	return nil
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

// tabFeeds are the feeds the current tab shows. The age line describes these.
func (m *Model) tabFeeds() []feed {
	switch m.tab {
	case tabAllowances:
		return []feed{feedQuotas, feedAccounts}
	case tabUsage:
		return []feed{feedRequests, feedAPIUsage}
	}
	return allFeeds()
}

// setNotice sets the status line's text and colour. An empty text clears it.
func (m *Model) setNotice(k noticeKind, text string) {
	m.notice, m.noticeKind = text, k
}
