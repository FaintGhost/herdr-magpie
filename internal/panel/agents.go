package panel

import (
	"cmp"
	"slices"
	"strings"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/launcher"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// herdrLabels pairs the magpie agent ids with herdr's label for the same agent.
// Labels were checked against herdr 0.9.3: `herdr agent list` showed claude and omp
// (the live list had no other kind), and herdr's api schema lists codex, hermes and
// kimi among its integrations. An id not listed here has no herdr label, so no pane
// is paired with it. dsh (DeepSeek Harness) is one: herdr has no label for it.
var herdrLabels = map[string]string{
	"claude": "claude",
	"codex":  "codex",
	"omp":    "omp",
	"hermes": "hermes",
	"kimi":   "kimi",
}

// herdrLabel is herdr's agent label for the magpie agent id, or "" when herdr has none.
func herdrLabel(id string) string {
	return herdrLabels[id]
}

// sortedAgents returns the agents in display order: magpie's order, with the hidden
// agents last.
func sortedAgents(agents []magpie.AgentInfo) []magpie.AgentInfo {
	out := slices.Clone(agents)
	slices.SortStableFunc(out, func(a, b magpie.AgentInfo) int {
		switch {
		case a.Hidden == b.Hidden:
			return 0
		case a.Hidden:
			return 1
		}
		return -1
	})
	return out
}

// panesOf returns the herdr panes that run the agent, in herdr's order. An agent
// with no herdr label runs in no pane.
func panesOf(a magpie.AgentInfo, panes []herdr.Agent) []herdr.Agent {
	label := herdrLabel(a.ID)
	if label == "" {
		return nil
	}
	var out []herdr.Agent
	for _, p := range panes {
		if p.Agent == label {
			out = append(out, p)
		}
	}
	return out
}

// agentBlock is one agent of the Agents tab and the panes that run it.
type agentBlock struct {
	agent magpie.AgentInfo
	panes []herdr.Agent
}

// agentBlocks pairs each agent, in display order, with its panes.
func agentBlocks(agents []magpie.AgentInfo, panes []herdr.Agent) []agentBlock {
	sorted := sortedAgents(agents)
	blocks := make([]agentBlock, len(sorted))
	for i, a := range sorted {
		blocks[i] = agentBlock{agent: a, panes: panesOf(a, panes)}
	}
	return blocks
}

// Columns of an agent row, in terminal cells. The config path takes what the fixed
// columns leave, up to agentPathMax, and the other fields take the rest.
const (
	markW        = 2
	agentNameW   = 18
	agentModelW  = 22
	agentGap     = 2
	agentPathMax = 36
)

// agentFields are the agent's fields other than the model, as "effort high · thinking on".
func agentFields(a magpie.AgentInfo) string {
	var parts []string
	for _, f := range a.Fields {
		if f.Name == "model" {
			continue
		}
		parts = append(parts, clean(f.Name)+" "+cmp.Or(clean(f.Value), "—"))
	}
	return strings.Join(parts, " · ")
}

// agentLine is one agent's row: name, model, the other fields, and the config path,
// which is cut from its front. A hidden agent is dimmed and marked as hidden.
func agentLine(a magpie.AgentInfo, selected bool, w int, home string, st styles) string {
	base := st.base
	if a.Hidden {
		base = st.dim
	}
	mark, title := "  ", base
	if selected {
		mark, title = "▸ ", st.bold
	}
	name := cmp.Or(clean(a.Name), clean(a.ID), "(unnamed)")
	extras := agentFields(a)
	if a.Hidden {
		// The tag goes first in the fields, where a narrow row keeps it.
		extras = strings.TrimSpace("hidden · " + extras)
		extras = strings.TrimSuffix(extras, " ·")
	}
	rest := max(w-(markW+agentNameW+agentGap+agentModelW+agentGap), 0)
	pathW := min(rest*2/5, agentPathMax)
	extrasW := rest - pathW
	if pathW > 0 {
		extrasW -= agentGap
	}
	segs := []seg{
		{mark, base},
		{fit(name, agentNameW), title},
		{"  ", base},
		{fit(cmp.Or(clean(a.Model()), "—"), agentModelW), base},
		{"  ", base},
		{fit(extras, extrasW), base},
	}
	if pathW > 0 {
		path := launcher.ShortPath(clean(a.ConfigPath), home)
		segs = append(segs, seg{"  ", base}, seg{launcher.TailFit(path, pathW), st.dim})
	}
	return line(w, segs...)
}

// paneLine is one pane that runs the agent, indented under it: its id, agent, status
// and directory. The directory is cut from its front to fit.
func paneLine(p herdr.Agent, dim bool, w int, home string, st styles) string {
	base := st.base
	if dim {
		base = st.dim
	}
	status := cmp.Or(clean(p.Status), "unknown")
	statusStyle := base
	if status == "blocked" {
		statusStyle = st.warn
	}
	head := "    " + cmp.Or(clean(p.PaneID), "?") + " · " + cmp.Or(clean(p.Agent), "?") + " · "
	dirW := max(w-displayWidth(head)-displayWidth(status)-3, 0)
	dir := launcher.TailFit(launcher.ShortPath(clean(p.CWD), home), dirW)
	return line(w, seg{head, base}, seg{status, statusStyle}, seg{" · ", base}, seg{dir, st.dim})
}

// agentsLines draws the Agents tab in h lines: each agent with its panes below it.
// The selected agent's block is always on screen.
func (m *Model) agentsLines(w, h int) []string {
	st := m.st
	var out []string
	if e := m.feeds[feedAgents].err; e != "" {
		out = append(out, line(w, seg{"agents: " + clean(e), st.warn}))
	}
	if e := m.panes.err; e != "" {
		out = append(out, line(w, seg{"herdr agents: " + clean(e), st.warn}))
	}
	agents := m.snap.Agents.Data
	switch {
	case len(agents) == 0 && m.loading(feedAgents):
		return append(out, line(w, seg{"loading…", st.dim}))
	case len(agents) == 0:
		return append(out, line(w, seg{"no agents reported; press r to refresh", st.dim}))
	}
	blocks := agentBlocks(agents, m.panes.list)
	cur := min(max(m.agentCursor, 0), len(blocks)-1)
	heights := make([]int, len(blocks))
	for i, b := range blocks {
		heights[i] = 1 + len(b.panes)
	}
	start, end := visibleSpan(heights, cur, h-len(out))
	for i := start; i < end; i++ {
		b := blocks[i]
		out = append(out, agentLine(b.agent, i == cur, w, m.home, st))
		for _, p := range b.panes {
			out = append(out, paneLine(p, b.agent.Hidden, w, m.home, st))
		}
	}
	return out
}
