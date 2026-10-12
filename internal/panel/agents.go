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

// Widths of the Agents tab's columns, in cells. Each column is as wide as its widest
// value, up to its maximum. The model takes the room that the others leave, up to its
// maximum. A narrow row drops the config path first, then the other fields, then the
// reasoning, so the name and the model stay.
const (
	agentNameMax   = 18
	agentNameMin   = 8
	agentModelMax  = 40 // a model name is shown in full up to this many cells
	agentModelMin  = 12
	agentReasonMax = 28 // shown whole or not at all: its least is its widest value
	agentOtherMax  = 40
	agentOtherMin  = 8
	agentPathMax   = 36
	agentPathMin   = 10
)

// agentCols is the width of each column of the Agents tab, 0 where a column is left out.
type agentCols struct {
	name, model, reason, other, path int
}

// agentColumns sizes the Agents tab's columns in cw cells, for the agents it lists.
func agentColumns(agents []magpie.AgentInfo, home string, cw int) agentCols {
	var names, models, reasons, others, paths []string
	for _, a := range agents {
		names = append(names, agentName(a))
		models = append(models, agentModel(a))
		reasons = append(reasons, agentReason(a))
		others = append(others, agentOther(a))
		paths = append(paths, agentPath(a, home))
	}
	widths := fitColumns(cw, tableGap, []column{
		sizedCol("AGENT", names, agentNameMin, agentNameMax, false),
		sizedCol("MODEL", models, agentModelMin, agentModelMax, true),
		sizedCol("REASONING", reasons, agentReasonMax, agentReasonMax, false),
		sizedCol("OTHER", others, agentOtherMin, agentOtherMax, false),
		sizedCol("CONFIG", paths, agentPathMin, agentPathMax, false),
	})
	return agentCols{name: widths[0], model: widths[1], reason: widths[2], other: widths[3], path: widths[4]}
}

// agentName is the agent's name as magpie shows it, or its id, or a stand-in.
func agentName(a magpie.AgentInfo) string {
	return cmp.Or(clean(a.Name), clean(a.ID), "(unnamed)")
}

// agentModel is the agent's model, or "—" when it has none.
func agentModel(a magpie.AgentInfo) string {
	return cmp.Or(clean(a.Model()), "—")
}

// agentReason is the agent's reasoning settings: "effort high" and "thinking on", as
// they are set, joined by " · ". It is "" when the agent has neither.
func agentReason(a magpie.AgentInfo) string {
	var parts []string
	for _, f := range a.Fields {
		if f.Name == "effort" || f.Name == "thinking" {
			parts = append(parts, clean(f.Name)+" "+cmp.Or(clean(f.Value), "—"))
		}
	}
	return strings.Join(parts, " · ")
}

// agentOther is the agent's fields other than its model and its reasoning, as
// "subagent model x". A hidden agent's tag comes first.
func agentOther(a magpie.AgentInfo) string {
	var parts []string
	if a.Hidden {
		parts = append(parts, "hidden")
	}
	for _, f := range a.Fields {
		if f.Name == "model" || f.Name == "effort" || f.Name == "thinking" {
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

// agentHeader is the Agents tab's column titles, dimmed.
func agentHeader(cols agentCols, st styles) []cell {
	return []cell{
		{text: "AGENT", style: st.dim, width: cols.name},
		{text: "MODEL", style: st.dim, width: cols.model},
		{text: "REASONING", style: st.dim, width: cols.reason},
		{text: "OTHER", style: st.dim, width: cols.other},
		{text: "CONFIG", style: st.dim, width: cols.path},
	}
}

// agentLine is one agent's row: its name, model, reasoning, other fields and config
// path. The selected row is bold, and a hidden agent is dimmed.
func agentLine(a magpie.AgentInfo, selected bool, bw int, home string, st styles, cols agentCols) string {
	base := st.base
	switch {
	case selected:
		base = st.bold
	case a.Hidden:
		base = st.dim
	}
	path := ""
	if cols.path > 0 {
		path = launcher.TailFit(agentPath(a, home), cols.path)
	}
	return markLine(bw, selected, st, []cell{
		{text: agentName(a), style: base, width: cols.name},
		{text: agentModel(a), style: base, width: cols.model},
		{text: agentReason(a), style: base, width: cols.reason},
		{text: agentOther(a), style: base, width: cols.other},
		{text: path, style: st.dim, width: cols.path},
	})
}

// paneCols is the width of the pane id and the status in the pane lines, so that the
// directories of every pane on the tab start in the same column.
type paneCols struct {
	id, status int
}

// paneColumns sizes the pane id and status columns for the panes of blocks.
func paneColumns(blocks []agentBlock) paneCols {
	var pc paneCols
	for _, b := range blocks {
		for _, p := range b.panes {
			pc.id = max(pc.id, displayWidth(cmp.Or(clean(p.PaneID), "?")))
			pc.status = max(pc.status, displayWidth(cmp.Or(clean(p.Status), "unknown")))
		}
	}
	return pc
}

// paneLine is one pane that runs the agent, indented under it: its id, status and
// directory, as "└ <pane>  <status>  <dir>". The directory is cut from its front.
func paneLine(p herdr.Agent, dim bool, bw int, home string, st styles, pc paneCols) string {
	base := st.base
	if dim {
		base = st.dim
	}
	status := cmp.Or(clean(p.Status), "unknown")
	statusStyle := base
	if status == "blocked" {
		statusStyle = st.warn
	}
	const indent = "    └ " // the agent's name is at column 2 of the body, so the pane's mark is at 4
	used := displayWidth(indent) + pc.id + tableGap + pc.status + tableGap
	dir := launcher.TailFit(launcher.ShortPath(clean(p.CWD), home), max(bw-used, 0))
	return line(bw,
		seg{indent, base},
		seg{fit(cmp.Or(clean(p.PaneID), "?"), pc.id), base},
		seg{strings.Repeat(" ", tableGap), base},
		seg{fit(status, pc.status), statusStyle},
		seg{strings.Repeat(" ", tableGap), base},
		seg{dir, st.dim},
	)
}

// agentsLines draws the Agents tab in h lines: the column titles, then each agent with
// its panes below it. The selected agent's block is always on screen.
func (m *Model) agentsLines(w, h int) []string {
	st := m.st
	bw, cw := splitWidth(w)
	var out []string
	if e := m.feeds[feedAgents].err; e != "" {
		out = append(out, noteLine(bw, st.warn, "agents: "+clean(e)))
	}
	if e := m.panes.err; e != "" {
		out = append(out, noteLine(bw, st.warn, "herdr agents: "+clean(e)))
	}
	agents := m.snap.Agents.Data
	switch {
	case len(agents) == 0 && m.loading(feedAgents):
		return append(out, noteLine(bw, st.dim, "loading…"))
	case len(agents) == 0:
		return append(out, noteLine(bw, st.dim, "no agents reported; press r to refresh"))
	}
	blocks := agentBlocks(agents, m.panes.list)
	cur := min(max(m.agentCursor, 0), len(blocks)-1)
	heights := make([]int, len(blocks))
	for i, b := range blocks {
		heights[i] = 1 + len(b.panes)
	}
	cols := agentColumns(agents, m.home, cw)
	pc := paneColumns(blocks)
	out = append(out, markLine(bw, false, st, agentHeader(cols, st)))
	start, end := visibleSpan(heights, cur, h-len(out))
	for i := start; i < end; i++ {
		b := blocks[i]
		out = append(out, agentLine(b.agent, i == cur, bw, m.home, st, cols))
		for _, p := range b.panes {
			out = append(out, paneLine(p, b.agent.Hidden, bw, m.home, st, pc))
		}
	}
	return out
}
