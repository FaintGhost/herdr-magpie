package panel

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// maxWindowRows caps the usage windows drawn on one card. The rest are counted.
const maxWindowRows = 4

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

// cardTitle is a card's name, plan and account, joined. The account comes last,
// so a narrow card cuts it first.
func cardTitle(c card, agents []magpie.AgentInfo) string {
	q := c.quota
	parts := []string{cmp.Or(clean(q.Name), agentLabel(q.Provider, agents))}
	if plan := clean(q.Plan); plan != "" {
		parts = append(parts, plan)
	}
	if user := clean(q.User); user != "" {
		parts = append(parts, user)
	}
	return strings.Join(parts, " · ")
}

// windowLine draws one usage window: its name, a bar, the percentage used, and
// when it resets. The bar and the percentage take the usage colour.
func windowLine(win magpie.QuotaWindow, now time.Time, w int, st styles) string {
	ls := st.levelStyle(usageLevel(win.Used))
	segs := []seg{
		{"    " + fit(clean(win.Name), 9) + " ", st.base},
		{progressBar(win.Used, barCells), ls},
		{fmt.Sprintf("  %3.0f%% used", win.Used), ls},
	}
	if r := resetText(now, win.ResetsAt); r != "" {
		segs = append(segs, seg{" · " + r, st.dim})
	}
	return line(w, segs...)
}

// cardLines draws one card: a title line, then one line per usage window, or a
// note for a balance or a card with no windows, then a blank line below.
func cardLines(c card, selected bool, now time.Time, agents []magpie.AgentInfo, w int, st styles) []string {
	mark, titleStyle := "  ", st.base
	if selected {
		mark, titleStyle = "▸ ", st.bold
	}
	tag := ""
	if c.active {
		tag = " ● active"
	}
	title := fit(mark+cardTitle(c, agents), w-displayWidth(tag))
	lines := []string{line(w, seg{title, titleStyle}, seg{tag, st.ok})}

	q := c.quota
	switch {
	case q.Kind == "balance":
		lines = append(lines, line(w, seg{"    balance: amount not wired yet (batch 2)", st.dim}))
	case len(q.Windows) == 0:
		lines = append(lines, line(w, seg{"    no usage windows reported", st.dim}))
	default:
		wins, extra := q.Windows, 0
		if len(wins) > maxWindowRows {
			wins, extra = wins[:maxWindowRows-1], len(q.Windows)-(maxWindowRows-1)
		}
		for _, win := range wins {
			lines = append(lines, windowLine(win, now, w, st))
		}
		if extra > 0 {
			lines = append(lines, line(w, seg{fmt.Sprintf("    +%d more windows", extra), st.dim}))
		}
	}
	return append(lines, line(w))
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

// cardRows draws the cards that fit in rows lines, scrolled so that the cursor's
// card is on screen.
func cardRows(cards []card, cursor int, now time.Time, agents []magpie.AgentInfo, w, rows int, st styles) []string {
	blocks := make([][]string, len(cards))
	heights := make([]int, len(cards))
	for i, c := range cards {
		blocks[i] = cardLines(c, i == cursor, now, agents, w, st)
		heights[i] = len(blocks[i])
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
func pickerLines(p pickState, agents []magpie.AgentInfo, w, h int, st styles) []string {
	if h <= 0 {
		return nil
	}
	out := []string{line(w, seg{"Switch " + agentLabel(p.agent, agents) + " to which account?", st.bold})}
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
		out = append(out, line(w, seg{mark + dot + " " + clean(a.User) + "  ", st.base}, seg{tag, st.ok}))
	}
	return out
}

// cards are the Allowances cards for the snapshot as it stands.
func (m *Model) cards() []card {
	return buildCards(m.snap.Quotas.Data, m.snap.Accounts.Data)
}

// allowancesLines draws the Allowances tab in h lines: the account list while a
// switch is open; otherwise the failures of quotas or accounts, then the cards.
func (m *Model) allowancesLines(w, h int) []string {
	st := m.st
	if m.flow != flowIdle {
		return pickerLines(m.pick, m.snap.Agents.Data, w, h, st)
	}
	var out []string
	if e := m.feeds[feedQuotas].err; e != "" {
		out = append(out, line(w, seg{"quotas: " + clean(e), st.warn}))
	}
	if e := m.feeds[feedAccounts].err; e != "" {
		out = append(out, line(w, seg{"accounts: " + clean(e), st.warn}))
	}
	cards := m.cards()
	switch {
	case len(cards) == 0 && m.loading(feedQuotas):
		return append(out, line(w, seg{"loading…", st.dim}))
	case len(cards) == 0:
		return append(out, line(w, seg{"no subscriptions or keys reported; press r to refresh", st.dim}))
	}
	cursor := min(m.cursor, len(cards)-1)
	return append(out, cardRows(cards, cursor, m.now(), m.snap.Agents.Data, w, h-len(out), st)...)
}
