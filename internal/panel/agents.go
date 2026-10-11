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

// Columns of an agent row, in terminal cells. The mark takes markW cells, and the
// columns after it are agentGap apart. Each column's want is the widest value on
// screen, up to its maximum, so the rows line up.
const (
	markW          = 2
	agentGap       = 2
	agentNameMax   = 18
	agentNameMin   = 8
	agentModelMax  = 40 // a model name is shown in full up to this many cells
	agentModelMin  = 12
	agentEffortMax = 16
	agentOtherMax  = 40
	agentOtherMin  = 8
	agentPathMax   = 36
	agentPathMin   = 10
)

// agentCols is the width of each column of the Agents tab, 0 where a column is left out.
type agentCols struct {
	name, model, effort, other, path int
}

// agentColumns sizes the Agents tab's columns in w cells, for the agents it lists. The
// name and the model come first, then the effort, the other fields and the config
// path. A narrow row drops the config path first, then the other fields, then the
// effort, so the name and the model stay.
func agentColumns(agents []magpie.AgentInfo, home string, w int) agentCols {
	var name, model, effort, other, path int
	for _, a := range agents {
		name = max(name, displayWidth(agentName(a)))
		model = max(model, displayWidth(agentModel(a)))
		effort = max(effort, displayWidth(agentEffort(a)))
		other = max(other, displayWidth(agentOther(a)))
		path = max(path, displayWidth(agentPath(a, home)))
	}
	widths := fitColumns(w-markW, agentGap, []column{
		fieldColumn(name, agentNameMax, agentNameMin),
		fieldColumn(model, agentModelMax, agentModelMin),
		fieldColumn(effort, agentEffortMax, agentEffortMax), // all or nothing
		fieldColumn(other, agentOtherMax, agentOtherMin),
		fieldColumn(path, agentPathMax, agentPathMin),
	})
	return agentCols{name: widths[0], model: widths[1], effort: widths[2], other: widths[3], path: widths[4]}
}

// fieldColumn is the column for content as wide as longest: it takes up to most cells,
// and shrinks to no less than least. Content that is empty leaves the column out.
func fieldColumn(longest, most, least int) column {
	want := min(longest, most)
	if want <= 0 {
		return column{}
	}
	return column{min: min(least, want), want: want}
}

// agentName is the agent's name as magpie shows it, or its id, or a stand-in.
func agentName(a magpie.AgentInfo) string {
	return cmp.Or(clean(a.Name), clean(a.ID), "(unnamed)")
}

// agentModel is the agent's model, or "—" when it has none.
func agentModel(a magpie.AgentInfo) string {
	return cmp.Or(clean(a.Model()), "—")
}

// agentEffort is the agent's effort as "effort high", or "" when it has none.
func agentEffort(a magpie.AgentInfo) string {
	for _, f := range a.Fields {
		if f.Name == "effort" {
			return "effort " + cmp.Or(clean(f.Value), "—")
		}
	}
	return ""
}

// agentOther is the agent's fields other than its model and effort, as "thinking on ·
// subagent model x". A hidden agent's tag comes first.
func agentOther(a magpie.AgentInfo) string {
	var parts []string
	if a.Hidden {
		parts = append(parts, "hidden")
	}
	for _, f := range a.Fields {
		if f.Name == "model" || f.Name == "effort" {
			continue
		}
		parts = append(parts, clean(f.Name)+" "+cmp.Or(clean(f.Value), "—"))
	}
	return strings.Join(parts, " · ")
}

// agentPath is the agent's config path, with the home directory shown as ~.
func agentPath(a magpie.AgentInfo, home string) string {
	return launcher.ShortPath(clean(a.ConfigPath), home)
}

// agentLine is one agent's row: name, model, effort, the other fields, and the config
// path, which is cut from its front. A hidden agent is dimmed.
func agentLine(a magpie.AgentInfo, selected bool, w int, home string, st styles, cols agentCols) string {
	base := st.base
	if a.Hidden {
		base = st.dim
	}
	mark, title := "  ", base
	if selected {
		mark, title = "▸ ", st.bold
	}
	cells := []cell{
		{text: agentName(a), style: title, width: cols.name},
		{text: agentModel(a), style: base, width: cols.model},
		{text: agentEffort(a), style: base, width: cols.effort},
		{text: agentOther(a), style: base, width: cols.other},
		{text: launcher.TailFit(agentPath(a, home), cols.path), style: st.dim, width: cols.path},
	}
	return line(w, append([]seg{{mark, base}}, cellSegs(cells, agentGap, base)...)...)
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
	cols := agentColumns(agents, m.home, w)
	start, end := visibleSpan(heights, cur, h-len(out))
	for i := start; i < end; i++ {
		b := blocks[i]
		out = append(out, agentLine(b.agent, i == cur, w, m.home, st, cols))
		for _, p := range b.panes {
			out = append(out, paneLine(p, b.agent.Hidden, w, m.home, st))
		}
	}
	return out
}
