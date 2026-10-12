package panel

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// maxWindowRows caps the usage windows drawn on one account. The rest are counted.
const maxWindowRows = 4

// Widths of the account rows, in cells. The plan and the account are as wide as their
// widest value, up to these caps. An account with an error has no windows, so its
// message takes the rest of the row.
const (
	planMax = 12
	userMax = 30
)

// activeText is the mark on the account that the agent is signed in to.
const activeText = "● active"

// card is one subscription or key on the Allowances tab, from magpie quota.
type card struct {
	quota  magpie.Quota
	agent  string // the agent a subscription belongs to; "" for a key's plan or balance
	multi  bool   // the agent has more than one account
	active bool   // the agent is signed in to this account, and it has several
}

// buildCards makes one card per quota. A subscription's Provider is its agent's
// id, so it meets the agent's accounts on that id and on the user.
func buildCards(quotas []magpie.Quota, accounts []magpie.Account) []card {
	cards := make([]card, 0, len(quotas))
	for _, q := range quotas {
		c := card{quota: q}
		if q.Kind == "subscription" {
			same := accountsOf(accounts, q.Provider)
			c.agent = q.Provider
			c.multi = len(same) > 1
			c.active = c.multi && slices.ContainsFunc(same, func(a magpie.Account) bool {
				return a.Active && a.User == q.User
			})
		}
		cards = append(cards, c)
	}
	return cards
}

// accountsOf returns the accounts of agent, in the order given.
func accountsOf(accounts []magpie.Account, agent string) []magpie.Account {
	var out []magpie.Account
	for _, a := range accounts {
		if a.Agent == agent {
			out = append(out, a)
		}
	}
	return out
}

// switchChoices are the accounts of agent that a switch can name: those with a user.
func switchChoices(accounts []magpie.Account, agent string) []magpie.Account {
	return slices.DeleteFunc(accountsOf(accounts, agent), func(a magpie.Account) bool {
		return clean(a.User) == ""
	})
}

// agentLabel is the display name of the agent with id, as magpie ls printed it,
// or the id when magpie named none.
func agentLabel(id string, agents []magpie.AgentInfo) string {
	if i := slices.IndexFunc(agents, func(a magpie.AgentInfo) bool { return a.ID == id }); i >= 0 {
		if name := clean(agents[i].Name); name != "" {
			return name
		}
	}
	return clean(id)
}

// cardGroup is the heading an account is listed under: the agent's name, or the name
// of a key or a balance.
func cardGroup(c card, agents []magpie.AgentInfo) string {
	return cmp.Or(clean(c.quota.Name), agentLabel(c.quota.Provider, agents))
}

// acctCols is the width of each column of an account row, 0 where a column is left out.
// The windows come after them.
type acctCols struct {
	plan, user, status int
}

// acctColumns sizes the account columns for the cards: the plan and the account as wide
// as their widest value, and the status when some account is signed in.
func acctColumns(cards []card) acctCols {
	var cols acctCols
	for _, c := range cards {
		cols.plan = max(cols.plan, displayWidth(clean(c.quota.Plan)))
		cols.user = max(cols.user, displayWidth(clean(c.quota.User)))
	}
	cols.plan = min(cols.plan, planMax)
	cols.user = min(cols.user, userMax)
	if slices.ContainsFunc(cards, func(c card) bool { return c.active }) {
		cols.status = displayWidth(activeText)
	}
	return cols
}

// windowSegs is one usage window: its name, a bar, the percentage used, and when it
// resets. The bar and the percentage take the usage colour.
func windowSegs(win magpie.QuotaWindow, now time.Time, st styles) []seg {
	ls := st.levelStyle(usageLevel(win.Used))
	segs := []seg{
		{clean(win.Name) + " ", st.base},
		{progressBar(win.Used, barCells), ls},
		{fmt.Sprintf(" %3.0f%%", win.Used), ls},
	}
	if r := resetText(now, win.ResetsAt); r != "" {
		segs = append(segs, seg{"  " + r, st.dim})
	}
	return segs
}

// segsWidth is the width of segs in cells.
func segsWidth(segs []seg) int {
	n := 0
	for _, s := range segs {
		n += displayWidth(s.text)
	}
	return n
}

// windowItems is what goes after an account's columns, one item per window, each
// as the segments that draw it. A card with no windows, or a balance, has one note.
func windowItems(c card, now time.Time, st styles) [][]seg {
	q := c.quota
	switch {
	case q.Kind == "balance":
		text, style := "balance: not reported", st.dim
		if b := clean(q.Balance); b != "" {
			text, style = "balance: "+b, st.base
		}
		return [][]seg{{{text, style}}}
	case len(q.Windows) == 0:
		return [][]seg{{{"no usage windows reported", st.dim}}}
	}
	wins, extra := q.Windows, 0
	if len(wins) > maxWindowRows {
		wins, extra = wins[:maxWindowRows-1], len(q.Windows)-(maxWindowRows-1)
	}
	items := make([][]seg, 0, len(wins)+1)
	for _, win := range wins {
		items = append(items, windowSegs(win, now, st))
	}
	if extra > 0 {
		items = append(items, []seg{{fmt.Sprintf("+%d more windows", extra), st.dim}})
	}
	return items
}

// accountLines draws one account in bw columns, the gutter included: its plan, its
// account, and its status, then its windows. A window that does not fit on the row goes on a
// line of its own, under the first window. An account with an error shows the error
// where its windows would be.
func accountLines(c card, selected bool, now time.Time, bw int, cols acctCols, st styles) []string {
	base, mark := st.base, seg{"  ", st.base}
	if selected {
		base, mark = st.bold, seg{"▸ ", st.bold}
	}
	segs := []seg{mark}
	width := gutterW
	column := func(text string, w int, style lipgloss.Style) {
		if w <= 0 {
			return
		}
		if width > gutterW {
			segs = append(segs, seg{strings.Repeat(" ", tableGap), base})
			width += tableGap
		}
		segs = append(segs, seg{fit(text, w), style})
		width += w
	}
	q := c.quota
	column(clean(q.Plan), cols.plan, base)
	column(clean(q.User), cols.user, base)
	if q.Error != "" {
		errW := bw - width
		if width > gutterW {
			errW -= tableGap
		}
		column("⚠ "+clean(q.Error), errW, st.warn)
		return []string{line(bw, segs...)}
	}
	status, statusStyle := "", st.ok
	if c.active {
		status = activeText
	}
	column(status, cols.status, statusStyle)

	// The windows start after the columns, three cells on, and a wrapped window
	// starts in that same column.
	lead := 0
	if width > gutterW {
		lead = 3
	}
	var out []string
	cur, curW, n := segs, width, 0
	for _, item := range windowItems(c, now, st) {
		iw := segsWidth(item)
		sep := 0
		switch {
		case n > 0:
			sep = 3 // between windows on one line
		case len(out) == 0:
			sep = lead // after the columns, on the first line
		}
		if n > 0 && curW+sep+iw > bw {
			out = append(out, line(bw, cur...))
			cur = []seg{{strings.Repeat(" ", width+lead), st.base}}
			curW, n, sep = width+lead, 0, 0
		}
		if sep > 0 {
			cur = append(cur, seg{strings.Repeat(" ", sep), st.base})
		}
		cur = append(cur, item...)
		curW += sep + iw
		n++
	}
	return append(out, line(bw, cur...))
}

// cardRows draws the accounts that fit in rows lines, grouped under their agents, and
// scrolled so that the cursor's account is on screen. A group's heading goes above its
// first account in the list.
func cardRows(cards []card, cursor int, now time.Time, agents []magpie.AgentInfo, bw, rows int, st styles) []string {
	cols := acctColumns(cards)
	blocks := make([][]string, len(cards))
	heights := make([]int, len(cards))
	group := ""
	for i, c := range cards {
		var b []string
		if g := cardGroup(c, agents); g != group {
			group = g
			b = append(b, noteLine(bw, st.bold, g))
		}
		b = append(b, accountLines(c, i == cursor, now, bw, cols, st)...)
		blocks[i] = b
		heights[i] = len(b)
	}
	start, end := visibleSpan(heights, cursor, rows)
	var out []string
	for _, b := range blocks[start:end] {
		out = append(out, b...)
	}
	return out[:min(len(out), max(rows, 0))]
}

// pickerLines draws an open switch: a heading, then the agent's accounts with the
// cursor on one of them and the signed-in one marked.
func pickerLines(p pickState, agents []magpie.AgentInfo, bw, h int, st styles) []string {
	if h <= 0 {
		return nil
	}
	out := []string{noteLine(bw, st.bold, "Switch "+agentLabel(p.agent, agents)+" to which account?")}
	start, end := visibleSpan(slices.Repeat([]int{1}, len(p.cands)), p.cursor, h-1)
	for i := start; i < end; i++ {
		a := p.cands[i]
		mark, dot, tag := "  ", "○", ""
		if i == p.cursor {
			mark = "▸ "
		}
		if a.Active {
			dot, tag = "●", "(active)"
		}
		out = append(out, line(bw, seg{mark + dot + " " + clean(a.User) + "  ", st.base}, seg{tag, st.ok}))
	}
	return out
}

// cards are the Allowances cards for the snapshot as it stands.
func (m *Model) cards() []card {
	return buildCards(m.snap.Quotas.Data, m.snap.Accounts.Data)
}

// allowancesLines draws the Allowances tab in h lines: the account list while a
// switch is open; otherwise the failures of quotas or accounts, then the accounts.
func (m *Model) allowancesLines(w, h int) []string {
	st := m.st
	bw, _ := splitWidth(w)
	if m.flow != flowIdle {
		return pickerLines(m.pick, m.snap.Agents.Data, bw, h, st)
	}
	var out []string
	if e := m.feeds[feedQuotas].err; e != "" {
		out = append(out, noteLine(bw, st.warn, "quotas: "+clean(e)))
	}
	if e := m.feeds[feedAccounts].err; e != "" {
		out = append(out, noteLine(bw, st.warn, "accounts: "+clean(e)))
	}
	cards := m.cards()
	switch {
	case len(cards) == 0 && m.loading(feedQuotas):
		return append(out, noteLine(bw, st.dim, "loading…"))
	case len(cards) == 0:
		return append(out, noteLine(bw, st.dim, "no subscriptions or keys reported; press r to refresh"))
	}
	cursor := min(m.cursor, len(cards)-1)
	return append(out, cardRows(cards, cursor, m.now(), m.snap.Agents.Data, bw, h-len(out), st)...)
}

// visibleSpan picks the blocks that fit in rows lines, given each block's height.
// It returns the first block shown and the one after the last. The cursor's block
// is always included: the span extends upward first, then downward. A block taller
// than rows is still returned, and the caller cuts it.
func visibleSpan(heights []int, cursor, rows int) (start, end int) {
	if len(heights) == 0 || rows <= 0 {
		return 0, 0
	}
	cursor = min(max(cursor, 0), len(heights)-1)
	start = cursor
	used := heights[cursor]
	for start > 0 && used+heights[start-1] <= rows {
		start--
		used += heights[start]
	}
	end = start + 1
	used = heights[start]
	for end < len(heights) && used+heights[end] <= rows {
		used += heights[end]
		end++
	}
	return start, end
}
