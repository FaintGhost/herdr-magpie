package panel

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

func TestHerdrLabels(t *testing.T) {
	for _, id := range []string{"claude", "codex", "omp", "hermes", "kimi"} {
		if got := herdrLabel(id); got != id {
			t.Errorf("herdrLabel(%q) = %q, want the same label", id, got)
		}
	}
	for _, id := range []string{"dsh", "pi", "", "Claude"} {
		if got := herdrLabel(id); got != "" {
			t.Errorf("herdrLabel(%q) = %q, want no label", id, got)
		}
	}
}

func TestPanesPairByLabelOnly(t *testing.T) {
	panes := append(samplePanes(), herdr.Agent{PaneID: "w50:p1", Agent: "dsh", Status: "idle"})
	blocks := agentBlocks(sampleAgents(), panes)
	byID := map[string][]string{}
	for _, b := range blocks {
		for _, p := range b.panes {
			byID[b.agent.ID] = append(byID[b.agent.ID], p.PaneID)
		}
	}
	if got := strings.Join(byID["claude"], ","); got != "w3H:p1,w20:p1,w3G:p1" {
		t.Errorf("claude panes = %q", got)
	}
	if got := byID["codex"]; len(got) != 1 || got[0] != "w36:p1" {
		t.Errorf("codex panes = %v", got)
	}
	if got := byID["dsh"]; len(got) != 0 {
		t.Errorf("dsh has no herdr label, but was paired with %v", got)
	}
	for id, got := range byID {
		if strings.Contains(strings.Join(got, ","), "w40:p1") {
			t.Errorf("%s was paired with an unknown label's pane", id)
		}
	}
}

func TestHiddenAgentsGoLast(t *testing.T) {
	agents := []magpie.AgentInfo{
		{ID: "dsh", Hidden: true}, {ID: "codex"}, {ID: "claude"},
	}
	var got []string
	for _, a := range sortedAgents(agents) {
		got = append(got, a.ID)
	}
	if want := "codex,claude,dsh"; strings.Join(got, ",") != want {
		t.Errorf("order = %v, want %s", got, want)
	}
	if agents[0].ID != "dsh" {
		t.Error("sortedAgents changed its input")
	}
}

// longAgents are agents whose fields are as long as magpie's ls output makes them: a
// model of 31 cells, a subagent model, and a config path of 41 cells.
func longAgents() []magpie.AgentInfo {
	return []magpie.AgentInfo{
		{Name: "DeepSeek Harness", ID: "dsh", ConfigPath: "~/.dsh/profiles/example/example.patch.yml", Fields: []magpie.AgentField{
			{Name: "model", Value: "magpie/deepseek/deepseek-flash"}, {Name: "thinking", Value: "—"},
		}},
		{Name: "omp", ID: "omp", ConfigPath: "~/.omp/agent/config.yml", Fields: []magpie.AgentField{
			{Name: "model", Value: "magpie/codex/gpt-6-astra:medium"}, {Name: "thinking", Value: "high"},
		}},
		{Name: "Codex", ID: "codex", ConfigPath: "~/.codex/config.toml", Fields: []magpie.AgentField{
			{Name: "model", Value: "codex/gpt-6-astra"}, {Name: "effort", Value: "high"},
			{Name: "subagent model", Value: "codex/gpt-6-astra"},
		}},
	}
}

func TestAgentRowFitsAndCarriesTheConfig(t *testing.T) {
	st := newStyles(newRenderer(io.Discard, false))
	agents := longAgents()
	a := agents[2]
	for _, w := range []int{20, 40, 58, 80, 120} {
		line := agentLine(a, true, w, "/home/u", st, agentColumns(agents, "/home/u", w))
		if dw := displayWidth(line); dw != w {
			t.Errorf("w=%d: row is %d columns: %q", w, dw, line)
		}
		if !strings.HasPrefix(line, "▸ ") {
			t.Errorf("w=%d: the selected row lacks its mark: %q", w, line)
		}
	}
	line := agentLine(a, false, 120, "/home/u", st, agentColumns(agents, "/home/u", 120))
	for _, want := range []string{"Codex", "codex/gpt-6-astra", "effort high", "subagent model", "~/.codex/config.toml"} {
		if !strings.Contains(line, want) {
			t.Errorf("120 columns: row lacks %q: %q", want, line)
		}
	}
}

func TestAgentColumnsDropThePathThenTheOtherFieldsThenTheEffort(t *testing.T) {
	agents := longAgents()
	for _, tc := range []struct {
		w                   int
		effort, other, path bool
	}{
		{58, false, false, false},
		{80, true, true, false},
		{120, true, true, true},
	} {
		cols := agentColumns(agents, "/home/u", tc.w)
		got := [3]bool{cols.effort > 0, cols.other > 0, cols.path > 0}
		if want := [3]bool{tc.effort, tc.other, tc.path}; got != want {
			t.Errorf("w=%d: effort, other and path shown = %v, want %v (%+v)", tc.w, got, want, cols)
		}
		if cols.name == 0 || cols.model == 0 {
			t.Errorf("w=%d: the name or the model is left out: %+v", tc.w, cols)
		}
	}
	if cols := agentColumns(agents, "/home/u", 58); cols.model != 31 {
		t.Errorf("58 columns: model column is %d cells, want the full 31", cols.model)
	}
}

func TestAgentModelIsShownWholeToFortyCells(t *testing.T) {
	agents := longAgents()
	agents[0].Fields[0].Value = strings.Repeat("m", 45)
	st := newStyles(newRenderer(io.Discard, false))
	cols := agentColumns(agents, "/home/u", 120)
	if cols.model != 40 {
		t.Errorf("model column is %d cells, want the 40-cell cap", cols.model)
	}
	if line := agentLine(agents[0], false, 120, "/home/u", st, cols); !strings.Contains(line, strings.Repeat("m", 39)+"…") {
		t.Errorf("a 45-cell model should be cut to 40 cells with an ellipsis: %q", line)
	}
}

func TestPaneLineNamesThePaneAndShortensItsDirectory(t *testing.T) {
	st := newStyles(newRenderer(io.Discard, false))
	p := herdr.Agent{PaneID: "w3H:p1", Agent: "claude", Status: "working", CWD: "/home/u/pp/x"}
	line := paneLine(p, false, 80, "/home/u", st)
	if want := "    w3H:p1 · claude · working · ~/pp/x"; !strings.HasPrefix(line, want) {
		t.Errorf("pane line = %q, want it to start %q", line, want)
	}
	if dw := displayWidth(paneLine(p, false, 12, "/home/u", st)); dw != 12 {
		t.Errorf("narrow pane line is %d columns", dw)
	}
}

func TestAgentsTabShowsThePanesUnderTheirAgent(t *testing.T) {
	src := samplePanelSource()
	m := panelModel(t, src, &fakeHerdr{panes: samplePanes()}, nil, 120, 32)
	press(m, "1")
	view := m.View()
	for _, want := range []string{"Claude Code", "w3H:p1 · claude · working", "w20:p1 · claude · idle", "w36:p1 · codex · blocked", "DeepSeek Harness"} {
		if !strings.Contains(view, want) {
			t.Errorf("Agents tab lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "w40:p1") {
		t.Errorf("a pane with an unknown label should not show under an agent:\n%s", view)
	}
	claude, codex, hidden := strings.Index(view, "Claude Code"), strings.Index(view, "Codex"), strings.Index(view, "DeepSeek Harness")
	if !(claude < codex && codex < hidden) {
		t.Errorf("order claude=%d codex=%d hidden=%d: hidden agents should be last", claude, codex, hidden)
	}
}

func TestHiddenAgentIsDimmedInColour(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	m := newModel(t.Context(), Deps{
		Source: samplePanelSource(), Herdr: &fakeHerdr{panes: samplePanes()},
		Now: func() time.Time { return testNow },
	}, r)
	drain(m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	press(m, "1")
	out := m.View()
	idx := strings.Index(out, "DeepSeek Harness")
	if idx < 0 {
		t.Fatalf("hidden agent not drawn:\n%s", out)
	}
	if !strings.Contains(out[:idx], "\x1b[2m") {
		t.Errorf("the hidden agent's row should carry the faint attribute")
	}
}

func TestFocusAgentSelectsItsRow(t *testing.T) {
	m := testModel(t, Deps{Source: samplePanelSource(), Herdr: &fakeHerdr{panes: samplePanes()}, FocusAgent: "codex"})
	drain(m, m.Init())
	if m.agentCursor != 1 || m.tab != tabAgents {
		t.Errorf("cursor = %d, tab = %v; want the codex row on the Agents tab", m.agentCursor, m.tab)
	}

	m = testModel(t, Deps{Source: samplePanelSource(), FocusAgent: "pi"})
	drain(m, m.Init())
	if m.agentCursor != 0 {
		t.Errorf("a label no agent has should leave the cursor at 0, got %d", m.agentCursor)
	}
}

func TestFocusAgentDoesNotOverrideStartTab(t *testing.T) {
	m := testModel(t, Deps{Source: samplePanelSource(), FocusAgent: "codex", StartTab: "usage"})
	drain(m, m.Init())
	if m.tab != tabUsage {
		t.Errorf("tab = %v, want Usage from StartTab", m.tab)
	}
	if m.agentCursor != 1 {
		t.Errorf("the focused agent should still be selected, cursor = %d", m.agentCursor)
	}
}

func TestAgentCursorStaysOnScreen(t *testing.T) {
	src := samplePanelSource()
	src.agents = nil
	for i := range 30 {
		src.agents = append(src.agents, magpie.AgentInfo{Name: "agent " + string(rune('a'+i%26)), ID: "x" + string(rune('a'+i%26))})
	}
	src.agents = append(src.agents, magpie.AgentInfo{Name: "Target Agent", ID: "claude"})
	m := panelModel(t, src, nil, nil, 80, 10)
	press(m, "1")
	for range 40 {
		press(m, "down")
	}
	if !strings.Contains(m.View(), "Target Agent") {
		t.Errorf("the selected agent is off screen:\n%s", m.View())
	}
}

func TestAgentsTabSaysWhenPanesFail(t *testing.T) {
	m := panelModel(t, samplePanelSource(), &fakeHerdr{err: errors.New("boom")}, nil, 80, 24)
	press(m, "1")
	if view := m.View(); !strings.Contains(view, "herdr agents: boom") {
		t.Errorf("view lacks the pane error:\n%s", view)
	}
}

func TestAgentsTabFitsEverySize(t *testing.T) {
	src := samplePanelSource()
	src.agents[0].Name = "中文代理很长很长的名字"
	for _, sz := range screenSizes {
		m := panelModel(t, src, &fakeHerdr{panes: samplePanes()}, nil, sz[0], sz[1])
		press(m, "1")
		lines := strings.Split(m.View(), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], len(lines))
		}
		assertLines(t, lines, sz[0])
	}
}
