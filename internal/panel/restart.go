package panel

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/launcher"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// restartPlan is the restart a change calls for. It is made once a pane list read
// after the change has landed, so that the panes are judged as they now are.
type restartPlan struct {
	reason string   // what changed, for the walk's heading, such as "switching Claude Code to ops"
	agents []string // ids of the agents whose panes may need a restart
	idle   string   // the notice when no pane needs one; "" keeps the current notice
}

// restartStep is one pane the restart walk looks at.
type restartStep struct {
	pane    herdr.Agent
	target  RestartTarget // what RestartPane is given; set only for a candidate
	skip    string        // why the pane is left alone, such as "busy, skipped"; "" for a candidate
	outcome string        // what became of a candidate: "restarted", "skipped", "not restarted", or "failed: ..."
}

// restartWalk is the restart flow: the panes, and where the walk has got to. Once
// done, the walk stays on screen until the user dismisses it.
type restartWalk struct {
	active bool
	done   bool
	reason string
	steps  []restartStep
	next   int // index of the step the walk asks about next
}

// restartedMsg is the result of one RestartPane call. index is the step's.
type restartedMsg struct {
	index int
	err   error
}

// paneRunsAny reports whether herdr's agent p is one of the magpie agents ids.
func paneRunsAny(p herdr.Agent, ids []string) bool {
	return p.Agent != "" && slices.ContainsFunc(ids, func(id string) bool {
		return herdrLabel(id) == p.Agent
	})
}

// restartSteps lists the panes that run one of the agents, in herdr's order. A pane
// that is idle or done can be restarted, when magpie gave its session a resume
// command. A working or blocked pane is left running, and so is one whose status is
// unknown. sessionsMissing says magpie's sessions could not be read at all.
func restartSteps(panes []herdr.Agent, agents []string, sessions []magpie.Session, sessionsMissing bool, home string) []restartStep {
	byKey := magpie.SessionsByKey(sessions)
	var steps []restartStep
	for _, p := range panes {
		if !paneRunsAny(p, agents) {
			continue
		}
		step := restartStep{pane: p}
		switch p.Status {
		case "idle", "done":
			s, ok := byKey[herdr.SessionKey(p.Session)]
			switch {
			case sessionsMissing:
				step.skip = "sessions unavailable, skipped"
			case !ok || s.Resume == "":
				step.skip = "no resume command, skipped"
			default:
				step.target = RestartTarget{
					PaneID:      p.PaneID,
					WorkspaceID: p.WorkspaceID,
					CWD:         cmp.Or(p.CWD, s.CWD),
					Agent:       p.Agent,
					Resume:      s.Resume,
					Label:       launcher.TabLabel(s, home),
				}
			}
		case "working", "blocked":
			step.skip = "busy, skipped"
		default:
			step.skip = "status unknown, skipped"
		}
		steps = append(steps, step)
	}
	return steps
}

// startPlan opens the restart walk for the panes that run the planned agents. With
// no such pane it sets the plan's idle notice, if it has one.
func (m *Model) startPlan(p restartPlan) {
	if m.panes.err != "" {
		m.setNotice(noticeBad, "herdr agents unavailable, so no pane is restarted: "+clean(m.panes.err))
		return
	}
	missing := len(m.snap.Sessions.Data) == 0 && m.feeds[feedSessions].err != ""
	steps := restartSteps(m.panes.list, p.agents, m.snap.Sessions.Data, missing, m.home)
	if len(steps) == 0 {
		if p.idle != "" {
			m.setNotice(noticeInfo, p.idle)
		}
		return
	}
	m.walk = restartWalk{active: true, reason: p.reason, steps: steps}
	m.askNextRestart()
}

// askNextRestart puts the question for the next candidate on the confirm line, or
// ends the walk when no candidate is left.
func (m *Model) askNextRestart() {
	w := &m.walk
	for i := w.next; i < len(w.steps); i++ {
		if st := w.steps[i]; st.skip == "" && st.outcome == "" {
			w.next = i
			m.askRestart(i)
			return
		}
	}
	m.finishWalk()
}

// askRestart asks about the candidate at step i: Enter restarts it, s skips it, and
// Esc stops the walk, which leaves every later candidate alone.
func (m *Model) askRestart(i int) {
	p := m.walk.steps[i].pane
	m.ask = &confirmLine{
		text: fmt.Sprintf("Restart %s (%s, %s)? Enter / s skip / Esc stop", p.PaneID, p.Agent, p.Status),
		hint: "Enter restart · s skip · Esc stop",
		yes:  func() tea.Cmd { return m.beginRestart(i) },
		skip: func() { m.skipRestart(i) },
		no:   m.stopWalk,
	}
}

// beginRestart starts RestartPane for step i, unless a write is already in flight.
// A refused start asks the same question again, so the walk does not stall.
func (m *Model) beginRestart(i int) tea.Cmd {
	t := m.walk.steps[i].target
	if !m.beginWrite("restarting " + t.PaneID + "…") {
		m.askRestart(i)
		return nil
	}
	return restartCmd(m.ctx, m.act, i, t)
}

// restartCmd runs RestartPane off the event loop, bounded by writeTimeout.
func restartCmd(ctx context.Context, act Actions, i int, t RestartTarget) tea.Cmd {
	return func() tea.Msg {
		err := runWriteErr(ctx, func(ctx context.Context) error {
			if act == nil {
				return notConfigured("restarting")
			}
			return act.RestartPane(ctx, t)
		})
		return restartedMsg{index: i, err: err}
	}
}

// landRestart records the result of one restart and asks about the next candidate.
func (m *Model) landRestart(msg restartedMsg) tea.Cmd {
	m.writing = false
	if msg.index >= 0 && msg.index < len(m.walk.steps) {
		st := &m.walk.steps[msg.index]
		if msg.err != nil {
			st.outcome = "failed: " + clean(msg.err.Error())
		} else {
			st.outcome = "restarted"
		}
		m.walk.next = msg.index + 1
	}
	cmd := m.refreshPanes()
	m.askNextRestart()
	return cmd
}

// skipRestart leaves the candidate at step i alone and asks about the next one.
func (m *Model) skipRestart(i int) {
	m.walk.steps[i].outcome = "skipped"
	m.walk.next = i + 1
	m.askNextRestart()
}

// stopWalk ends the walk at the candidate it was asking about. The candidates after
// it, and any not yet answered, are not restarted.
func (m *Model) stopWalk() {
	for i := range m.walk.steps {
		if st := &m.walk.steps[i]; st.skip == "" && st.outcome == "" {
			st.outcome = "not restarted"
		}
	}
	m.finishWalk()
}

// finishWalk ends the walk. The question closes, the result stays on screen until
// it is dismissed, and the notice sums it up.
func (m *Model) finishWalk() {
	m.ask = nil
	m.walk.done = true
	restarted, failed := 0, 0
	for _, st := range m.walk.steps {
		switch {
		case st.outcome == "restarted":
			restarted++
		case strings.HasPrefix(st.outcome, "failed"):
			failed++
		}
	}
	left := len(m.walk.steps) - restarted - failed
	text := fmt.Sprintf("restart done: %d restarted, %d failed, %d left running", restarted, failed, left)
	kind := noticeInfo
	switch {
	case failed > 0:
		kind = noticeBad
	case restarted > 0:
		kind = noticeOK
	}
	m.setNotice(kind, text)
}

// keyWalk handles a key while the walk is on screen without a question, which is
// once it is done. Esc or Enter dismisses the result.
func (m *Model) keyWalk(k string) tea.Cmd {
	switch k {
	case "q":
		return tea.Quit
	case "esc", "enter":
		m.walk = restartWalk{}
	}
	return nil
}

// restartLines draws the restart walk in h lines: the heading, then one line per
// pane with its state, and the step being asked about marked.
func (m *Model) restartLines(w, h int) []string {
	st := m.st
	heading := "Restart after " + m.walk.reason
	if m.walk.done {
		heading += " · done"
	}
	out := []string{line(w, seg{heading, st.bold})}
	steps := m.walk.steps
	heights := make([]int, len(steps))
	for i := range heights {
		heights[i] = 1
	}
	cur := m.walk.next
	if m.walk.done || len(steps) == 0 {
		cur = -1
	}
	cur = min(max(cur, 0), len(steps)-1)
	start, end := visibleSpan(heights, cur, h-len(out))
	for i := start; i < end; i++ {
		out = append(out, restartLine(steps[i], i == cur && !m.walk.done, w, m.home, st))
	}
	return out
}

// restartLine is one pane's line in the walk: its id, agent and status, what the walk
// did or will do with it, and its directory.
func restartLine(s restartStep, current bool, w int, home string, st styles) string {
	mark := "  "
	if current {
		mark = "▸ "
	}
	note, style := s.skip, st.dim
	switch {
	case s.skip != "":
	case s.outcome == "restarted":
		note, style = s.outcome, st.ok
	case strings.HasPrefix(s.outcome, "failed"):
		note, style = s.outcome, st.bad
	case s.outcome != "":
		note = s.outcome
	case current:
		note, style = "asking", st.bold
	default:
		note = "waiting"
	}
	head := mark + fit(clean(s.pane.PaneID), 8) + "  " + fit(clean(s.pane.Agent), 8) + "  " + fit(clean(s.pane.Status), 8) + "  "
	noteW := min(max(w-displayWidth(head)-2, 0), 26)
	dirW := max(w-displayWidth(head)-noteW-2, 0)
	dir := launcher.TailFit(launcher.ShortPath(clean(s.pane.CWD), home), dirW)
	return line(w, seg{head, st.base}, seg{fit(note, noteW), style}, seg{"  ", st.base}, seg{dir, st.dim})
}
