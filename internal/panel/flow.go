package panel

import (
	"context"
	"errors"
	"fmt"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// flowState is where a switch of subscription account stands. A switch goes from
// the account list to the confirm line. Esc ends it at either step, and only the
// Enter on the confirm line writes.
type flowState int

const (
	flowIdle flowState = iota
	flowPick
	flowConfirm
)

// pickState is the account list of an open switch.
type pickState struct {
	agent  string           // the agent's id, such as "claude"
	cands  []magpie.Account // the agent's accounts that have a user
	cursor int
}

// switchedMsg is the result of a SwitchAccount call.
type switchedMsg struct {
	agent string
	user  string
	err   error
}

// startPick opens the account list for the card under the cursor. Only
// subscription cards can switch, and only once their accounts are known.
func (m *Model) startPick() {
	cards := m.cards()
	if len(cards) == 0 {
		return
	}
	c := cards[min(m.cursor, len(cards)-1)]
	switch {
	case c.agent == "":
		m.setNotice(noticeInfo, "only subscription accounts can be switched")
		return
	case m.feeds[feedAccounts].err != "":
		m.setNotice(noticeBad, "accounts unavailable: "+m.feeds[feedAccounts].err)
		return
	case m.loading(feedAccounts):
		m.setNotice(noticeInfo, "accounts still loading")
		return
	}
	cands := switchChoices(m.snap.Accounts.Data, c.agent)
	if len(cands) == 0 {
		m.setNotice(noticeInfo, "no account to switch "+agentLabel(c.agent, m.snap.Agents.Data)+" to")
		return
	}
	m.flow = flowPick
	m.pick = pickState{agent: c.agent, cands: cands, cursor: firstInactive(cands)}
	m.setNotice(noticeInfo, "")
}

// firstInactive is the index of the first account that is not signed in, or 0.
func firstInactive(accts []magpie.Account) int {
	return max(slices.IndexFunc(accts, func(a magpie.Account) bool { return !a.Active }), 0)
}

// keyPick handles keys while the account list is open.
func (m *Model) keyPick(k string) tea.Cmd {
	p := &m.pick
	switch k {
	case "q":
		return tea.Quit
	case "esc":
		m.flow = flowIdle
		m.setNotice(noticeInfo, "switch cancelled")
	case "up":
		p.cursor = max(p.cursor-1, 0)
	case "down":
		p.cursor = min(p.cursor+1, len(p.cands)-1)
	case "enter":
		if a := p.cands[p.cursor]; a.Active {
			m.flow = flowIdle
			m.setNotice(noticeInfo, clean(a.User)+" is already active")
		} else {
			m.flow = flowConfirm
		}
	}
	return nil
}

// keyConfirm handles keys on the confirm line. Enter starts the write, and Esc drops it.
func (m *Model) keyConfirm(k string) tea.Cmd {
	switch k {
	case "q":
		return tea.Quit
	case "esc":
		m.flow = flowIdle
		m.setNotice(noticeInfo, "switch cancelled")
	case "enter":
		a := m.pick.cands[m.pick.cursor]
		m.flow = flowIdle
		m.writing = true
		m.setNotice(noticeInfo, "")
		return switchCmd(m.ctx, m.act, m.pick.agent, clean(a.User))
	}
	return nil
}

// confirmText is the question on the confirm line.
func (m *Model) confirmText() string {
	a := m.pick.cands[m.pick.cursor]
	return fmt.Sprintf("Switch %s to %s? Enter to confirm, Esc to cancel",
		agentLabel(m.pick.agent, m.snap.Agents.Data), clean(a.User))
}

// switchCmd runs SwitchAccount off the event loop, bounded by switchTimeout.
func switchCmd(ctx context.Context, act Actions, agent, user string) tea.Cmd {
	return func() tea.Msg {
		if act == nil {
			return switchedMsg{agent: agent, user: user, err: errors.New("switching is not configured")}
		}
		_, err := bounded(ctx, switchTimeout, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, act.SwitchAccount(ctx, agent, user)
		})
		return switchedMsg{agent: agent, user: user, err: err}
	}
}
