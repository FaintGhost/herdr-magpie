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

// costSideBySide is the narrowest width at which the agent and model tables sit side
// by side. Narrower, the model table goes below the agent table, so the names keep
// their room.
const costSideBySide = 90

// costNameMax is the most of an agent or model name that the cost tables show.
const costNameMax = 40

// costNumsW is the width of the calls, errors and cost columns of a cost table, after its name.
const costNumsW = 23

// stormHeading names the error table and the rule it follows.
const stormHeading = "Provider errors · last 10m · 400 and 499 are not counted"

// stormGap is the space between the columns of the error table.
const stormGap = 2

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

// countsLines draws the request table: a header with the windows 10m, 1h and today,
// then the requests, all the errors, the provider errors and the median time to first
// token in each.
func countsLines(reqs []magpie.Request, now time.Time, w int, st styles) []string {
	const col = 8
	windows := [...]struct {
		name  string
		since time.Time
	}{
		{"10m", now.Add(-stormWindow)},
		{"1h", now.Add(-time.Hour)},
		{"today", todayStart(now)},
	}
	label := w - col*len(windows)
	head, reqRow := fit("", label), fit("requests", label)
	errRow, provRow := fit("errors", label), fit("provider errors", label)
	ttftRow := fit("ttft p50", label)
	for _, win := range windows {
		c := countSince(reqs, win.since)
		head += rjust(win.name, col)
		reqRow += rjust(strconv.Itoa(c.Requests), col)
		errRow += rjust(strconv.Itoa(c.Errors), col)
		provRow += rjust(strconv.Itoa(c.ProviderErrors), col)
		ttft := "—"
		if d, ok := ttftP50(reqs, win.since); ok {
			ttft = formatTTFT(d)
		}
		ttftRow += rjust(ttft, col)
	}
	return []string{
		line(w, seg{head, st.dim}),
		line(w, seg{reqRow, st.base}),
		line(w, seg{errRow, st.base}),
		line(w, seg{provRow, st.base}),
		line(w, seg{ttftRow, st.base}),
	}
}

// stormCols is the width of each column of the error table, 0 where a column is left out.
type stormCols struct {
	state, provider, status, count, errType, agents int
}

// stormColumns sizes the error table in w cells. The state, the provider, the status
// and the count stay on a narrow row. The agents go first, then the error type.
func stormColumns(w int) stormCols {
	widths := fitColumns(w, stormGap, []column{
		{min: 8, want: 8},              // state
		{min: 12, want: 16},            // provider
		{min: 4, want: 4},              // http status
		{min: 5, want: 5},              // count
		{min: 8, want: 18},             // error type
		{min: 8, want: 24, flex: true}, // agents
	})
	return stormCols{state: widths[0], provider: widths[1], status: widths[2], count: widths[3], errType: widths[4], agents: widths[5]}
}

// stormText is the text of one line of the error table.
type stormText struct {
	provider, status, count, errType, agents, state string
}

// stormLine draws one line of the error table in w columns. The state comes last, in stateStyle.
func stormLine(w int, cols stormCols, t stormText, style, stateStyle lipgloss.Style) string {
	return line(w, cellSegs([]cell{
		{text: t.provider, style: style, width: cols.provider},
		{text: t.status, style: style, width: cols.status, right: true},
		{text: t.count, style: style, width: cols.count, right: true},
		{text: t.errType, style: style, width: cols.errType},
		{text: t.agents, style: style, width: cols.agents},
		{text: t.state, style: stateStyle, width: cols.state},
	}, stormGap, style)...)
}

// stormTable draws the error table. It returns the header lines and the group rows
// separately, so the rows can give way on a short screen. With no failures, the header
// is one line saying so and the rows are nil.
func stormTable(storms []storm, w int, st styles) (head, rows []string) {
	if len(storms) == 0 {
		return []string{line(w, seg{"none in the last 10m", st.dim})}, nil
	}
	cols := stormColumns(w)
	head = []string{stormLine(w, cols, stormText{
		provider: "provider", status: "http", count: "count", errType: "error", agents: "agents", state: "state",
	}, st.dim, st.dim)}
	for _, s := range storms {
		state, stateStyle := "", st.base
		if s.Active {
			state, stateStyle = "ACTIVE", st.bad
		}
		rows = append(rows, stormLine(w, cols, stormText{
			provider: clean(s.Provider),
			status:   statusCode(s.Status),
			count:    strconv.Itoa(s.Count),
			errType:  clean(s.ErrorType),
			agents:   clean(strings.Join(s.Agents, ", ")),
			state:    state,
		}, st.base, stateStyle))
	}
	return head, rows
}

// costLine lays out one line of a cost table: the name in nameW cells, then the calls,
// the errors and the cost.
func costLine(nameW int, name, calls, errs, cost string) string {
	return fit(name, nameW) + rjust(calls, 6) + rjust(errs, 7) + rjust(cost, 10)
}

// costHeading is the heading of a cost table: "by agent", or "agent" where the name
// column is too narrow for the longer one.
func costHeading(nameW int, kind string) string {
	if h := "by " + kind; nameW >= displayWidth(h) {
		return h
	}
	return kind
}

// costRow is row i of a cost list, or blank when the list is shorter.
func costRow(nameW int, rows []magpie.UsageRow, i int) string {
	if i >= len(rows) {
		return strings.Repeat(" ", nameW+costNumsW)
	}
	r := rows[i]
	return costLine(nameW, clean(r.Name), strconv.Itoa(r.Calls), strconv.Itoa(r.Errors), formatCost(r.Cost))
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
// by side. Narrower, the model table goes below the agent table. Rows that do not fit
// are left out.
func costLines(s magpie.UsageSummary, w, h int, st styles) []string {
	if h <= 0 {
		return nil
	}
	out := []string{
		line(w, seg{"Gateway cost · today", st.bold}),
		line(w, seg{fmt.Sprintf("%d calls · %s", s.Calls, formatCost(s.Cost)), st.base}),
	}
	agents, models := topRows(s.Agents, costRows), topRows(s.Models, costRows)
	if w >= costSideBySide {
		colW := (w - 3) / 2
		nameW := min(colW-costNumsW, costNameMax)
		head := fit(costLine(nameW, costHeading(nameW, "agent"), "calls", "errors", "cost"), colW) + "   " +
			costLine(nameW, costHeading(nameW, "model"), "calls", "errors", "cost")
		out = append(out, line(w, seg{head, st.dim}))
		for i := range min(max(h-len(out), 0), max(len(agents), len(models))) {
			out = append(out, line(w, seg{fit(costRow(nameW, agents, i), colW) + "   " + costRow(nameW, models, i), st.base}))
		}
		return out[:min(len(out), h)]
	}

	nameW := min(w-costNumsW, costNameMax)
	out = append(out, line(w, seg{costLine(nameW, costHeading(nameW, "agent"), "calls", "errors", "cost"), st.dim}))
	na, nm := splitRows(len(agents), len(models), h-len(out)-1) // one line is left for the model heading
	for i := range na {
		out = append(out, line(w, seg{costRow(nameW, agents, i), st.base}))
	}
	out = append(out, line(w, seg{costLine(nameW, costHeading(nameW, "model"), "calls", "errors", "cost"), st.dim}))
	for i := range nm {
		out = append(out, line(w, seg{costRow(nameW, models, i), st.base}))
	}
	return out[:min(len(out), h)]
}

// usageLines draws the Usage tab in h lines: the request counts, the provider errors of
// the last ten minutes, then the gateway cost. On a short screen the error rows give way
// first, and the cost rows after them.
func (m *Model) usageLines(w, h int) []string {
	st, now := m.st, m.now()
	var top []string
	if e := m.feeds[feedRequests].err; e != "" {
		top = append(top, line(w, seg{"requests: " + clean(e), st.warn}))
	}
	switch reqs := m.snap.Requests; {
	case !reqs.At.IsZero():
		top = append(top, countsLines(reqs.Data, now, w, st)...)
	case m.loading(feedRequests):
		top = append(top, line(w, seg{"loading…", st.dim}))
	default:
		top = append(top, line(w, seg{"no usage data yet", st.dim}))
	}
	top = append(top, line(w))

	tableHead, rows := stormTable(computeStorms(m.snap.Requests.Data, now), w, st)
	room := h - (len(top) + 1 + len(tableHead) + 1) // the heading, and the blank line before the cost
	cost := m.costBlock(w, room)
	rowRoom := room - len(cost)
	if len(rows) > rowRoom {
		if rowRoom <= 0 {
			rows = nil
		} else {
			keep := rowRoom - 1
			rows = append(rows[:keep:keep], line(w, seg{fmt.Sprintf("+%d more", len(rows)-keep), st.dim}))
		}
	}

	out := make([]string, 0, h)
	out = append(out, top...)
	out = append(out, line(w, seg{stormHeading, st.bold}))
	out = append(out, tableHead...)
	out = append(out, rows...)
	out = append(out, line(w))
	return append(out, cost...)
}

// costBlock is the cost part of the Usage tab, in h lines at most. While it loads, or
// when there is no gateway key, it is one note; otherwise it is the cost table.
func (m *Model) costBlock(w, h int) []string {
	st := m.st
	var out []string
	if e := m.feeds[feedAPIUsage].err; e != "" {
		out = append(out, line(w, seg{"api usage: " + clean(e), st.warn}))
	}
	api := m.snap.APIUsage
	switch {
	case api.At.IsZero():
		text := "no cost data yet"
		if m.loading(feedAPIUsage) {
			text = "loading…"
		}
		return append(out, line(w, seg{text, st.dim}))
	case api.Data == nil:
		return append(out, line(w, seg{"no gateway key — cost hidden", st.dim}))
	}
	return append(out, costLines(*api.Data, w, h-len(out), st)...)
}
