package panel

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// costRows is how many agents, and how many models, the cost block lists.
const costRows = 5

// costSideBySide is the narrowest screen at which the agent and model tables sit side
// by side. Narrower, the model table goes below the agent table, so the names keep
// their room.
const costSideBySide = 90

// costSideGap is the space between the agent and model tables when they sit side by side.
const costSideGap = 4

// costNameMax is the most of an agent or model name that the cost tables show.
const costNameMax = 40

// stormHeading names the error table and the rule it follows.
const stormHeading = "Provider errors · last 10m (400 and 499 not counted)"

// todayStart is midnight at the start of now's day, in now's location.
func todayStart(now time.Time) time.Time {
	y, mo, d := now.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
}

// usageSince is the earliest time the Usage tab needs: the start of today, or an
// hour back when that is earlier, so the 1h column is whole after midnight too.
func usageSince(now time.Time) time.Time {
	since, hour := todayStart(now), now.Add(-time.Hour)
	if hour.Before(since) {
		return hour
	}
	return since
}

// windowCount is the requests in one time window, how many of them failed, and how
// many of those failures are provider errors.
type windowCount struct {
	Requests       int
	Errors         int // failed: the error is set, or the status is 400 and up
	ProviderErrors int // failed by the storm rule, which leaves 400 and 499 out
}

// countSince counts the rows at or after since. The provider errors follow the storm
// rule, so they agree with the error table.
func countSince(reqs []magpie.Request, since time.Time) windowCount {
	var c windowCount
	for _, r := range reqs {
		if r.Time.Before(since) {
			continue
		}
		c.Requests++
		if isError(r) {
			c.Errors++
		}
		if failed(r) {
			c.ProviderErrors++
		}
	}
	return c
}

// topRows returns up to n rows: the costliest first, then the busiest, then by name.
func topRows(rows []magpie.UsageRow, n int) []magpie.UsageRow {
	sorted := slices.SortedFunc(slices.Values(rows), func(a, b magpie.UsageRow) int {
		return cmp.Or(
			cmp.Compare(b.Cost, a.Cost),
			cmp.Compare(b.Calls, a.Calls),
			strings.Compare(a.Name, b.Name),
		)
	})
	return sorted[:min(n, len(sorted))]
}

// ttftP50 is the median time to first token of the requests at or after since, and
// false when none of them has one. Rows without a time to first token are skipped.
func ttftP50(reqs []magpie.Request, since time.Time) (time.Duration, bool) {
	var ms []float64
	for _, r := range reqs {
		if r.TTFT > 0 && !r.Time.Before(since) {
			ms = append(ms, float64(r.TTFT)/float64(time.Millisecond))
		}
	}
	v, ok := percentile(ms, 50)
	return time.Duration(v * float64(time.Millisecond)), ok
}

// countsLines draws the request table: a title row with the windows 10m, 1h and today,
// then the requests, all the errors, the provider errors and the median time to first
// token in each. The labels are as wide as the longest of them, and the numbers follow
// at once.
func countsLines(reqs []magpie.Request, now time.Time, w int, st styles) []string {
	bw, _ := splitWidth(w)
	const col = 8
	windows := [...]struct {
		name  string
		since time.Time
	}{
		{"10m", now.Add(-stormWindow)},
		{"1h", now.Add(-time.Hour)},
		{"today", todayStart(now)},
	}
	var reqRow, errRow, provRow, ttftRow []string
	for _, win := range windows {
		c := countSince(reqs, win.since)
		reqRow = append(reqRow, strconv.Itoa(c.Requests))
		errRow = append(errRow, strconv.Itoa(c.Errors))
		provRow = append(provRow, strconv.Itoa(c.ProviderErrors))
		ttft := "—"
		if d, ok := ttftP50(reqs, win.since); ok {
			ttft = formatTTFT(d)
		}
		ttftRow = append(ttftRow, ttft)
	}
	labelW := 0
	for _, l := range []string{"REQUESTS", "requests", "errors", "provider errors", "ttft p50"} {
		labelW = max(labelW, displayWidth(l))
	}
	row := func(label string, vals []string, style lipgloss.Style) string {
		cells := []cell{{text: label, style: style, width: labelW}}
		for _, v := range vals {
			cells = append(cells, cell{text: v, style: style, width: col, right: true})
		}
		return markLine(bw, false, st, cells)
	}
	names := make([]string, 0, len(windows))
	for _, win := range windows {
		names = append(names, win.name)
	}
	return []string{
		row("REQUESTS", names, st.dim),
		row("requests", reqRow, st.base),
		row("errors", errRow, st.base),
		row("provider errors", provRow, st.base),
		row("ttft p50", ttftRow, st.base),
	}
}

// stormCols is the width of each column of the error table, 0 where a column is left out.
type stormCols struct {
	state, provider, status, count, errType, agents int
}

// stormColumns sizes the error table in cw cells. The state, the provider, the status
// and the count stay on a narrow row. The agents go first, then the error type.
func stormColumns(cw int) stormCols {
	widths := fitColumns(cw, tableGap, []column{
		{min: 8, want: 8},                       // state
		{min: 12, want: 16},                     // provider
		{min: 4, want: 4},                       // http status
		{min: 5, want: 5},                       // count
		{min: 8, want: 18},                      // error type
		{min: 8, want: 24, max: 40, flex: true}, // agents
	})
	return stormCols{state: widths[0], provider: widths[1], status: widths[2], count: widths[3], errType: widths[4], agents: widths[5]}
}

// stormText is the text of one line of the error table.
type stormText struct {
	provider, status, count, errType, agents, state string
}

// stormCells are the cells of one line of the error table. The state comes last, in stateStyle.
func stormCells(cols stormCols, t stormText, style, stateStyle lipgloss.Style) []cell {
	return []cell{
		{text: t.provider, style: style, width: cols.provider},
		{text: t.status, style: style, width: cols.status, right: true},
		{text: t.count, style: style, width: cols.count, right: true},
		{text: t.errType, style: style, width: cols.errType},
		{text: t.agents, style: style, width: cols.agents},
		{text: t.state, style: stateStyle, width: cols.state},
	}
}

// stormTable draws the error table. It returns the header lines and the group rows
// separately, so the rows can give way on a short screen. With no failures, the header
// is one line saying so and the rows are nil.
func stormTable(storms []storm, w int, st styles) (head, rows []string) {
	bw, cw := splitWidth(w)
	if len(storms) == 0 {
		return []string{noteLine(bw, st.dim, "none")}, nil
	}
	cols := stormColumns(cw)
	head = []string{markLine(bw, false, st, stormCells(cols, stormText{
		provider: "PROVIDER", status: "HTTP", count: "COUNT", errType: "ERROR", agents: "AGENTS", state: "STATE",
	}, st.dim, st.dim))}
	for _, s := range storms {
		state, stateStyle := "", st.base
		if s.Active {
			state, stateStyle = "ACTIVE", st.bad
		}
		rows = append(rows, markLine(bw, false, st, stormCells(cols, stormText{
			provider: clean(s.Provider),
			status:   statusCode(s.Status),
			count:    strconv.Itoa(s.Count),
			errType:  clean(s.ErrorType),
			agents:   clean(strings.Join(s.Agents, ", ")),
			state:    state,
		}, st.base, stateStyle)))
	}
	return head, rows
}

// costCols is the width of each column of a cost table: its name, then the calls, the
// errors and the cost. A column of width 0 is left out.
type costCols struct {
	head                    string
	name, calls, errs, cost int
}

// width is the table's width in cells, the gaps between its columns included.
func (c costCols) width() int {
	n := 0
	for _, w := range []int{c.name, c.calls, c.errs, c.cost} {
		if w > 0 {
			n += w + tableGap
		}
	}
	return max(n-tableGap, 0)
}

// costColumns sizes a cost table in cw cells for rows, with head as the name's title.
// The name is the flexible column: it takes the room left over, up to costNameMax.
func costColumns(rows []magpie.UsageRow, head string, cw int) costCols {
	names, calls, errs, costs := []string{head}, []string{"CALLS"}, []string{"ERRORS"}, []string{"COST"}
	for _, r := range rows {
		names = append(names, clean(r.Name))
		calls = append(calls, strconv.Itoa(r.Calls))
		errs = append(errs, strconv.Itoa(r.Errors))
		costs = append(costs, formatCost(r.Cost))
	}
	nameW := min(widestOf(names), costNameMax)
	widths := fitColumns(cw, tableGap, []column{
		{min: min(nameW, 8), want: nameW, max: costNameMax, flex: true},
		{min: widestOf(calls), want: widestOf(calls)},
		{min: widestOf(errs), want: widestOf(errs)},
		{min: widestOf(costs), want: widestOf(costs)},
	})
	return costCols{head: head, name: widths[0], calls: widths[1], errs: widths[2], cost: widths[3]}
}

// widestOf is the width of the widest of cells.
func widestOf(cells []string) int {
	w := 0
	for _, c := range cells {
		w = max(w, displayWidth(c))
	}
	return w
}

// costCells are the cells of one line of a cost table.
func costCells(c costCols, name, calls, errs, cost string, style lipgloss.Style) []cell {
	return []cell{
		{text: name, style: style, width: c.name},
		{text: calls, style: style, width: c.calls, right: true},
		{text: errs, style: style, width: c.errs, right: true},
		{text: cost, style: style, width: c.cost, right: true},
	}
}

// costRowCells is row i of a cost list, or blank cells when the list is shorter.
func costRowCells(c costCols, rows []magpie.UsageRow, i int, style lipgloss.Style) []cell {
	if i >= len(rows) {
		return costCells(c, "", "", "", "", style)
	}
	r := rows[i]
	return costCells(c, clean(r.Name), strconv.Itoa(r.Calls), strconv.Itoa(r.Errors), formatCost(r.Cost), style)
}

// splitRows gives the agent and the model tables their rows within n lines, taking
// rows from the longer table first.
func splitRows(a, b, n int) (int, int) {
	n = max(n, 0)
	for a+b > n {
		if a >= b {
			a--
		} else {
			b--
		}
	}
	return a, b
}

// costLines draws the gateway cost for today, in h lines at most: the total, then the
// costliest agents and models. From costSideBySide columns on, the two tables sit side
// by side, each with its own title. Narrower, the model table goes below the agent
// table. Rows that do not fit are left out.
func costLines(s magpie.UsageSummary, w, h int, st styles) []string {
	if h <= 0 {
		return nil
	}
	bw, cw := splitWidth(w)
	out := []string{line(bw,
		seg{"  ", st.base},
		seg{"Gateway cost · today", st.bold},
		seg{fmt.Sprintf("   %d calls · %s", s.Calls, formatCost(s.Cost)), st.base},
	)}
	agents, models := topRows(s.Agents, costRows), topRows(s.Models, costRows)
	if w >= costSideBySide {
		ac := costColumns(agents, "AGENT", (cw-costSideGap)/2)
		mc := costColumns(models, "MODEL", cw-costSideGap-ac.width())
		left := line(ac.width(), cellSegs(costCells(ac, ac.head, "CALLS", "ERRORS", "COST", st.dim), tableGap, st.dim)...)
		right := line(mc.width(), cellSegs(costCells(mc, mc.head, "CALLS", "ERRORS", "COST", st.dim), tableGap, st.dim)...)
		out = append(out, sideRow(bw, left, right, ac.width(), mc.width()))
		for i := range min(max(h-len(out), 0), max(len(agents), len(models))) {
			left := line(ac.width(), cellSegs(costRowCells(ac, agents, i, st.base), tableGap, st.base)...)
			right := line(mc.width(), cellSegs(costRowCells(mc, models, i, st.base), tableGap, st.base)...)
			out = append(out, sideRow(bw, left, right, ac.width(), mc.width()))
		}
		return out[:min(len(out), h)]
	}

	ac := costColumns(agents, "AGENT", cw)
	out = append(out, markLine(bw, false, st, costCells(ac, ac.head, "CALLS", "ERRORS", "COST", st.dim)))
	na, nm := splitRows(len(agents), len(models), h-len(out)-1) // one line is left for the model heading
	for i := range na {
		out = append(out, markLine(bw, false, st, costRowCells(ac, agents, i, st.base)))
	}
	mc := costColumns(models, "MODEL", cw)
	out = append(out, markLine(bw, false, st, costCells(mc, mc.head, "CALLS", "ERRORS", "COST", st.dim)))
	for i := range nm {
		out = append(out, markLine(bw, false, st, costRowCells(mc, models, i, st.base)))
	}
	return out[:min(len(out), h)]
}

// sideRow is one line of two tables side by side, behind the blank gutter, in bw
// columns. left and right are drawn already, lw and rw cells wide.
func sideRow(bw int, left, right string, lw, rw int) string {
	used := gutterW + lw + costSideGap + rw
	return strings.Repeat(" ", gutterW) + left + strings.Repeat(" ", costSideGap) + right + strings.Repeat(" ", max(bw-used, 0))
}

// usageLines draws the Usage tab in h lines: the request counts, the provider errors of
// the last ten minutes, then the gateway cost. On a short screen the error rows give way
// first, and the cost rows after them.
func (m *Model) usageLines(w, h int) []string {
	st, now := m.st, m.now()
	bw, _ := splitWidth(w)
	var top []string
	if e := m.feeds[feedRequests].err; e != "" {
		top = append(top, noteLine(bw, st.warn, "requests: "+clean(e)))
	}
	switch reqs := m.snap.Requests; {
	case !reqs.At.IsZero():
		top = append(top, countsLines(reqs.Data, now, w, st)...)
	case m.loading(feedRequests):
		top = append(top, noteLine(bw, st.dim, "loading…"))
	default:
		top = append(top, noteLine(bw, st.dim, "no usage data yet"))
	}
	top = append(top, line(bw))

	tableHead, rows := stormTable(computeStorms(m.snap.Requests.Data, now), w, st)
	room := h - (len(top) + 1 + len(tableHead) + 1) // the heading, and the blank line before the cost
	cost := m.costBlock(w, room)
	rowRoom := room - len(cost)
	if len(rows) > rowRoom {
		if rowRoom <= 0 {
			rows = nil
		} else {
			keep := rowRoom - 1
			rows = append(rows[:keep:keep], noteLine(bw, st.dim, fmt.Sprintf("+%d more", len(rows)-keep)))
		}
	}

	out := make([]string, 0, h)
	out = append(out, top...)
	out = append(out, noteLine(bw, st.bold, stormHeading))
	out = append(out, tableHead...)
	out = append(out, rows...)
	out = append(out, line(bw))
	return append(out, cost...)
}

// costBlock is the cost part of the Usage tab, in h lines at most. While it loads, or
// when there is no gateway key, it is one note; otherwise it is the cost table.
func (m *Model) costBlock(w, h int) []string {
	st := m.st
	bw, _ := splitWidth(w)
	var out []string
	if e := m.feeds[feedAPIUsage].err; e != "" {
		out = append(out, noteLine(bw, st.warn, "api usage: "+clean(e)))
	}
	api := m.snap.APIUsage
	switch {
	case api.At.IsZero():
		text := "no cost data yet"
		if m.loading(feedAPIUsage) {
			text = "loading…"
		}
		return append(out, noteLine(bw, st.dim, text))
	case api.Data == nil:
		return append(out, noteLine(bw, st.dim, "no gateway key — cost hidden"))
	}
	return append(out, costLines(*api.Data, w, h-len(out), st)...)
}
