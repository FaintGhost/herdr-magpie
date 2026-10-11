package panel

import (
	"errors"
	"strings"
	"testing"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// agentsWithClaudeModel is sampleAgents with claude on another model, as a profile
// that changes it would leave them.
func agentsWithClaudeModel(model string) []magpie.AgentInfo {
	out := sampleAgents()
	out[0].Fields = []magpie.AgentField{{Name: "model", Value: model}, {Name: "effort", Value: "high"}}
	return out
}

// changingClaude makes a model whose apply changes claude's model, and returns it
// with the source it changes.
func changingClaude(act *fakeActions, src *fakeSource, model string) {
	act.after = func() { src.agents = agentsWithClaudeModel(model) }
}

// walkedPanes are the claude panes of samplePanes, plus one more idle claude pane
// with a resume command, for the tests that need two candidates.
func walkedPanes() []herdr.Agent {
	extra := herdr.Agent{
		PaneID: "w41:p1", WorkspaceID: "w41", Agent: "claude", Status: "idle", CWD: "/home/u/pp/app",
		Session: &herdr.AgentSession{Kind: "id", Value: "aaaa-claude-1"},
	}
	return append(samplePanes(), extra)
}

func TestRestartStepsJudgesEachPane(t *testing.T) {
	sessions := sampleSessions()
	panes := []herdr.Agent{
		{PaneID: "idle-ok", WorkspaceID: "w1", Agent: "claude", Status: "idle", CWD: "/home/u/pp/app", Session: sessionRef("aaaa-claude-1")},
		{PaneID: "done-no-cwd", Agent: "claude", Status: "done", Session: sessionRef("bbbb-claude-2")},
		{PaneID: "working", Agent: "claude", Status: "working", Session: sessionRef("aaaa-claude-1")},
		{PaneID: "blocked", Agent: "claude", Status: "blocked", Session: sessionRef("aaaa-claude-1")},
		{PaneID: "unknown", Agent: "claude", Status: "unknown", Session: sessionRef("aaaa-claude-1")},
		{PaneID: "no-session", Agent: "claude", Status: "idle"},
		{PaneID: "no-resume", Agent: "omp", Status: "idle", Session: sessionRef("cccc-omp-3")},
		{PaneID: "other-agent", Agent: "codex", Status: "idle", Session: sessionRef("dddd-codex-4")},
	}
	steps := restartSteps(panes, []string{"claude", "omp"}, sessions, false, fixtureHome)

	want := []struct {
		id, skip string
	}{
		{"idle-ok", ""},
		{"done-no-cwd", ""},
		{"working", "busy, skipped"},
		{"blocked", "busy, skipped"},
		{"unknown", "status unknown, skipped"},
		{"no-session", "no resume command, skipped"},
		{"no-resume", "no resume command, skipped"},
	}
	if len(steps) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(steps), len(want), steps)
	}
	for i, w := range want {
		if steps[i].pane.PaneID != w.id || steps[i].skip != w.skip {
			t.Errorf("step %d = %s (%q), want %s (%q)", i, steps[i].pane.PaneID, steps[i].skip, w.id, w.skip)
		}
	}

	ok := steps[0].target
	if ok.PaneID != "idle-ok" || ok.WorkspaceID != "w1" || ok.CWD != "/home/u/pp/app" || ok.Agent != "claude" ||
		ok.Resume != "cd '/home/u/pp/app' && claude --resume aaaa-claude-1" || ok.Label != "claude: 修复登录" {
		t.Errorf("target = %+v", ok)
	}
	if got := steps[1].target.CWD; got != "/home/u/pp/docs" {
		t.Errorf("a pane without a directory should use its session's, got %q", got)
	}
}

func TestRestartStepsWithoutSessions(t *testing.T) {
	panes := []herdr.Agent{{PaneID: "p", Agent: "claude", Status: "idle", Session: sessionRef("aaaa-claude-1")}}
	steps := restartSteps(panes, []string{"claude"}, nil, true, fixtureHome)
	if len(steps) != 1 || steps[0].skip != "sessions unavailable, skipped" {
		t.Errorf("steps = %+v", steps)
	}
}

func TestPaneRunsAnyNeedsALabel(t *testing.T) {
	if paneRunsAny(herdr.Agent{Agent: ""}, []string{""}) {
		t.Error("a pane with no label should never match")
	}
	if paneRunsAny(herdr.Agent{Agent: "dsh"}, []string{"dsh"}) {
		t.Error("dsh has no herdr label, so no pane runs it")
	}
	if !paneRunsAny(herdr.Agent{Agent: "omp"}, []string{"claude", "omp"}) {
		t.Error("an omp pane should match omp")
	}
}

func sessionRef(id string) *herdr.AgentSession {
	return &herdr.AgentSession{Kind: "id", Value: strings.ToUpper(id)}
}

func TestRestartAfterAProfileRestartsOnlyAnIdlePane(t *testing.T) {
	act := &fakeActions{}
	src := samplePanelSource()
	changingClaude(act, src, "claude-sonnet-4-5")
	m := profilesModel(t, src, &fakeHerdr{panes: samplePanes()}, act)
	press(m, "p")
	press(m, "down")
	press(m, "enter")
	press(m, "enter")

	if !m.walk.active || m.ask == nil {
		t.Fatalf("walk %+v, ask %+v; want the restart walk asking", m.walk, m.ask)
	}
	if want := "Restart w20:p1 (claude, idle)? Enter / s skip / Esc stop"; m.ask.text != want {
		t.Fatalf("question = %q, want %q", m.ask.text, want)
	}
	if got := ansiFree(m.statusLine(120)); !strings.Contains(got, "Restart w20:p1") {
		t.Errorf("status line = %q", got)
	}
	if view := m.View(); !strings.Contains(view, "busy, skipped") || !strings.Contains(view, "no resume command, skipped") {
		t.Errorf("the walk should list the panes it leaves alone:\n%s", view)
	}

	press(m, "enter")
	if len(act.restarted) != 1 {
		t.Fatalf("RestartPane called %d times, want 1", len(act.restarted))
	}
	got := act.restarted[0]
	want := RestartTarget{
		PaneID: "w20:p1", WorkspaceID: "w20", CWD: "/home/u/pp/docs", Agent: "claude",
		Resume: "cd '/home/u/pp/docs' && claude --resume bbbb-claude-2", Label: "claude: 写文档",
	}
	if got != want {
		t.Errorf("RestartPane got %+v, want %+v", got, want)
	}
	if !m.walk.done || m.ask != nil {
		t.Errorf("the walk should be done with no question: done %v, ask %+v", m.walk.done, m.ask)
	}
	if m.noticeKind != noticeOK || m.notice != "restart done: 1 restarted, 0 failed, 2 left running" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}
	press(m, "esc")
	if m.walk.active {
		t.Error("esc should dismiss the finished walk")
	}
}

func TestRestartWalkSkipsAndStops(t *testing.T) {
	act := &fakeActions{}
	src := samplePanelSource()
	changingClaude(act, src, "claude-sonnet-4-5")
	m := profilesModel(t, src, &fakeHerdr{panes: walkedPanes()}, act)
	press(m, "p")
	press(m, "down")
	press(m, "enter")
	press(m, "enter")

	if m.ask == nil || !strings.HasPrefix(m.ask.text, "Restart w20:p1") {
		t.Fatalf("first question = %+v", m.ask)
	}
	press(m, "s")
	if m.ask == nil || !strings.HasPrefix(m.ask.text, "Restart w41:p1") {
		t.Fatalf("after skip, question = %+v", m.ask)
	}
	press(m, "esc")
	if len(act.restarted) != 0 {
		t.Errorf("restarted %v, want none", act.restarted)
	}
	if !m.walk.done || m.notice != "restart done: 0 restarted, 0 failed, 4 left running" {
		t.Errorf("done %v, notice %q", m.walk.done, m.notice)
	}
	view := m.View()
	for _, want := range []string{"skipped", "not restarted"} {
		if !strings.Contains(view, want) {
			t.Errorf("the finished walk lacks %q:\n%s", want, view)
		}
	}
}

func TestRestartFailureIsReportedAndTheWalkGoesOn(t *testing.T) {
	act := &fakeActions{restartErr: errors.New("pane is busy")}
	src := samplePanelSource()
	changingClaude(act, src, "claude-sonnet-4-5")
	m := profilesModel(t, src, &fakeHerdr{panes: walkedPanes()}, act)
	press(m, "p")
	press(m, "down")
	press(m, "enter")
	press(m, "enter")
	press(m, "enter") // w20:p1 fails, and the walk goes on to w41:p1
	if len(act.restarted) != 1 || m.ask == nil || !strings.HasPrefix(m.ask.text, "Restart w41:p1") {
		t.Fatalf("after a failure: restarted %d, question %+v", len(act.restarted), m.ask)
	}
	press(m, "enter") // w41:p1 fails too
	if len(act.restarted) != 2 || !m.walk.done {
		t.Fatalf("after the second failure: restarted %d, done %v", len(act.restarted), m.walk.done)
	}
	if m.noticeKind != noticeBad || m.notice != "restart done: 0 restarted, 2 failed, 2 left running" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}
	if view := m.View(); !strings.Contains(view, "failed: pane is busy") {
		t.Errorf("the failure should show on its pane's line:\n%s", view)
	}
	press(m, "esc")
	if m.walk.active {
		t.Error("esc should dismiss the finished walk")
	}
}

func TestSwitchPlansTheAgentsPanes(t *testing.T) {
	act := &fakeActions{}
	h := &fakeHerdr{panes: samplePanes()}
	m := profilesModel(t, samplePanelSource(), h, act)
	press(m, "2")
	press(m, "enter")
	press(m, "enter")
	press(m, "enter")

	if len(act.switched) != 1 || act.switched[0] != "claude ops@example.test" {
		t.Fatalf("switched = %v", act.switched)
	}
	if m.ask == nil || !strings.HasPrefix(m.ask.text, "Restart w20:p1 (claude, idle)") {
		t.Fatalf("after the switch, question = %+v", m.ask)
	}
}

func TestSwitchWithNoIdlePaneSaysSoAndCloses(t *testing.T) {
	act := &fakeActions{}
	src := samplePanelSource()
	src.accounts = append(src.accounts, magpie.Account{Agent: "codex", User: "ops@example.test", Plan: "Plus"})
	m := profilesModel(t, src, &fakeHerdr{panes: samplePanes()}, act)
	press(m, "2")
	press(m, "down")
	press(m, "down")
	press(m, "enter") // the codex card: its accounts open on ops
	press(m, "enter")
	press(m, "enter")

	if len(act.switched) != 1 || act.switched[0] != "codex ops@example.test" {
		t.Fatalf("switched = %v", act.switched)
	}
	if !m.walk.done || m.ask != nil {
		t.Fatalf("the codex pane is blocked: want the walk done with no question, got done %v, ask %+v", m.walk.done, m.ask)
	}
	if view := m.View(); !strings.Contains(view, "busy, skipped") {
		t.Errorf("the blocked codex pane should be listed as busy:\n%s", view)
	}
	press(m, "esc")
	if m.walk.active {
		t.Error("esc should dismiss the walk")
	}
}

func TestWalkDoesNotRestartWhileAWriteIsInFlight(t *testing.T) {
	act := &fakeActions{}
	src := samplePanelSource()
	changingClaude(act, src, "claude-sonnet-4-5")
	m := profilesModel(t, src, &fakeHerdr{panes: samplePanes()}, act)
	press(m, "p")
	press(m, "down")
	press(m, "enter")
	press(m, "enter")
	if m.ask == nil {
		t.Fatal("no question")
	}
	m.writing = true
	press(m, "enter")
	if len(act.restarted) != 0 || m.ask == nil {
		t.Errorf("a write in flight let the walk restart %v (ask %+v)", act.restarted, m.ask)
	}
}

func TestNoRestartWithoutHerdr(t *testing.T) {
	act := &fakeActions{}
	m := profilesModel(t, samplePanelSource(), nil, act)
	press(m, "2")
	press(m, "enter")
	press(m, "enter")
	press(m, "enter")
	if m.walk.active || m.notice != "switched Claude Code to ops@example.test" {
		t.Errorf("without herdr: walk %v, notice %q", m.walk.active, m.notice)
	}
}

func TestPaneListFailureStopsTheRestart(t *testing.T) {
	act := &fakeActions{}
	h := &fakeHerdr{panes: samplePanes()}
	m := profilesModel(t, samplePanelSource(), h, act)
	h.err = errors.New("herdr is down")
	press(m, "2")
	press(m, "enter")
	press(m, "enter")
	press(m, "enter")
	if m.walk.active || m.noticeKind != noticeBad || m.notice != "herdr agents unavailable, so no pane is restarted: herdr is down" {
		t.Errorf("walk %v, notice %q (%v)", m.walk.active, m.notice, m.noticeKind)
	}
}

func TestRestartLinesFitEverySize(t *testing.T) {
	for _, sz := range screenSizes {
		act := &fakeActions{}
		src := samplePanelSource()
		changingClaude(act, src, "claude-sonnet-4-5")
		m := panelModel(t, src, &fakeHerdr{panes: walkedPanes()}, act, sz[0], sz[1])
		press(m, "p")
		press(m, "down")
		press(m, "enter")
		press(m, "enter")
		lines := strings.Split(m.View(), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], len(lines))
		}
		assertLines(t, lines, sz[0])
	}
}
