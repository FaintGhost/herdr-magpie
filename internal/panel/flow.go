package panel

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// writeTimeout bounds one write to magpie. A write runs longer than a read, and it
// is not cut short when the panel quits, so that a change in progress completes.
var writeTimeout = 60 * time.Second

// flowState is where a switch of subscription account stands. A switch goes from
// the account list to the confirm line. Esc ends it at either step, and only the
// Enter on the confirm line writes.
type flowState int

const (
	flowIdle flowState = iota
	flowPick
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
			m.askSwitch(p.agent, clean(a.User))
		}
	}
	return nil
}

// askSwitch puts the switch's question on the confirm line. Enter starts the write
// and Esc drops the switch.
func (m *Model) askSwitch(agent, user string) {
	m.ask = &confirmLine{
		text: fmt.Sprintf("Switch %s to %s? Enter to confirm, Esc to cancel",
			agentLabel(agent, m.snap.Agents.Data), user),
		yes: func() tea.Cmd { return m.beginSwitch(agent, user) },
		no: func() {
			m.flow = flowIdle
			m.setNotice(noticeInfo, "switch cancelled")
		},
	}
}

// beginSwitch starts SwitchAccount, unless a write is already in flight.
func (m *Model) beginSwitch(agent, user string) tea.Cmd {
	m.flow = flowIdle
	if !m.beginWrite("switching…") {
		return nil
	}
	return switchCmd(m.ctx, m.act, agent, user)
}

// switchCmd runs SwitchAccount off the event loop, bounded by writeTimeout.
func switchCmd(ctx context.Context, act Actions, agent, user string) tea.Cmd {
	return func() tea.Msg {
		err := runWriteErr(ctx, func(ctx context.Context) error {
			if act == nil {
				return notConfigured("switching")
			}
			return act.SwitchAccount(ctx, agent, user)
		})
		return switchedMsg{agent: agent, user: user, err: err}
	}
}

// landSwitch records the result of a switch. A switch that worked may leave the
// agent's panes running the old account, so they are planned for a restart.
func (m *Model) landSwitch(msg switchedMsg) tea.Cmd {
	m.writing = false
	if msg.err != nil {
		m.setNotice(noticeBad, "switch failed: "+msg.err.Error())
		return m.afterWrite(nil, feedAccounts, feedQuotas)
	}
	label := agentLabel(msg.agent, m.snap.Agents.Data)
	m.setNotice(noticeOK, "switched "+label+" to "+msg.user)
	plan := restartPlan{reason: "switching " + label + " to " + msg.user, agents: []string{msg.agent}}
	return m.afterWrite(&plan, feedAccounts, feedQuotas)
}

// beginWrite marks a write as in flight, shown as text on the status line. It
// refuses, and says so, while another write is in flight.
func (m *Model) beginWrite(text string) bool {
	if m.writing {
		m.setNotice(noticeInfo, "wait for the change in progress to finish")
		return false
	}
	m.writing, m.writeText = true, text
	m.setNotice(noticeInfo, "")
	return true
}

// afterWrite reads the feeds a write changed, and the pane list. A plan is kept
// until that list lands, so that it judges the panes as they are after the write.
// Without herdr there are no panes, and so no plan.
func (m *Model) afterWrite(plan *restartPlan, feeds ...feed) tea.Cmd {
	if plan != nil && m.herdr != nil {
		m.plan = plan
	}
	cmds := m.refresh(feeds...)
	if c := m.refreshPanes(); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// runWrite runs one write bounded by writeTimeout, and returns what it returns. It
// is not cancelled when the panel quits, so a change in progress finishes.
func runWrite[T any](ctx context.Context, f func(context.Context) (T, error)) (T, error) {
	return bounded(context.WithoutCancel(ctx), writeTimeout, f)
}

// runWriteErr is runWrite for a write that returns only an error.
func runWriteErr(ctx context.Context, f func(context.Context) error) error {
	_, err := runWrite(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, f(ctx)
	})
	return err
}

// notConfigured is the error for a write that has no Actions to run it.
func notConfigured(what string) error {
	return errors.New(what + " is not configured")
}
