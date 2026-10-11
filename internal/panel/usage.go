package panel

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// costRows is how many agents, and how many models, the cost block lists.
const costRows = 5

// stormHeading names the error table and the rule it follows.
const stormHeading = "Provider errors · last 10m · 400 and 499 are not counted"

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

// windowCount is the requests in one time window and how many of them failed.
type windowCount struct {
	Requests int
	Errors   int
}

// countSince counts the rows at or after since. A failure is a provider failure
// by the storm rule, so these counts agree with the error table.
func countSince(reqs []magpie.Request, since time.Time) windowCount {
	var c windowCount
	for _, r := range reqs {
		if r.Time.Before(since) {
			continue
		}
		c.Requests++
		if failed(r) {
			c.Errors++
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

// countsLines draws the request table: a header with the windows 10m, 1h and
// today, then the requests, the errors and the median time to first token in each.
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
	errRow, ttftRow := fit("errors", label), fit("ttft p50", label)
	for _, win := range windows {
		c := countSince(reqs, win.since)
		head += rjust(win.name, col)
		reqRow += rjust(strconv.Itoa(c.Requests), col)
		errRow += rjust(strconv.Itoa(c.Errors), col)
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
		line(w, seg{ttftRow, st.base}),
	}
}

// stormLeft lays out the error table's columns, all but the state, in w columns.
// The agents column takes what is left.
func stormLeft(w int, provider, status, count, errType, agents string) string {
	aw := max(w-59, 0)
	return fit(provider, 16) + " " + fit(status, 4) + " " + rjust(count, 5) + "  " +
		fit(errType, 18) + "  " + fit(agents, aw) + "  "
}

// stormTable draws the error table. It returns the header lines and the group
// rows separately, so the rows can give way on a short screen. With no failures,
// the header is one line saying so and the rows are nil.
func stormTable(storms []storm, w int, st styles) (head, rows []string) {
	if len(storms) == 0 {
		return []string{line(w, seg{"none in the last 10m", st.dim})}, nil
	}
	head = []string{line(w,
		seg{stormLeft(w, "provider", "http", "count", "error", "agents"), st.dim},
		seg{fit("state", 8), st.dim},
	)}
	for _, s := range storms {
		state, style := "", st.base
		if s.Active {
			state, style = "ACTIVE", st.bad
		}
		rows = append(rows, line(w,
			seg{stormLeft(w, clean(s.Provider), statusCode(s.Status), strconv.Itoa(s.Count),
				clean(s.ErrorType), clean(strings.Join(s.Agents, ", "))), st.base},
			seg{fit(state, 8), style},
		))
	}
	return head, rows
}

// costCols lays out one row of the cost table in colW columns: the name, the
// calls, the errors and the cost.
func costCols(colW int, name, calls, errs, cost string) string {
	return fit(name, max(colW-22, 0)) + rjust(calls, 6) + rjust(errs, 6) + rjust(cost, 10)
}

// costCell is row i of a cost list, or blank when the list is shorter.
func costCell(colW int, rows []magpie.UsageRow, i int) string {
	if i >= len(rows) {
		return fit("", colW)
	}
	r := rows[i]
	return costCols(colW, clean(r.Name), strconv.Itoa(r.Calls), strconv.Itoa(r.Errors), formatCost(r.Cost))
}

// costLines draws the gateway cost for today: the total, then the costliest agents
// and models side by side.
func costLines(s magpie.UsageSummary, w int, st styles) []string {
	colW := max((w-3)/2, 0)
	out := []string{
		line(w, seg{"Gateway cost · today", st.bold}),
		line(w, seg{fmt.Sprintf("%d calls · %s", s.Calls, formatCost(s.Cost)), st.base}),
		line(w, seg{costCols(colW, "by agent", "calls", "err", "cost") + "   " +
			costCols(colW, "by model", "calls", "err", "cost"), st.dim}),
	}
	agents, models := topRows(s.Agents, costRows), topRows(s.Models, costRows)
	for i := range max(len(agents), len(models)) {
		out = append(out, line(w, seg{costCell(colW, agents, i) + "   " + costCell(colW, models, i), st.base}))
	}
	return out
}

// usageLines draws the Usage tab in h lines: the request counts, the provider
// errors of the last ten minutes, then the gateway cost. On a short screen the
// error rows give way first.
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
	cost := m.costBlock(w)
	room := h - (len(top) + 1 + len(tableHead) + 1 + len(cost))
	if len(rows) > room {
		if room <= 0 {
			rows = nil
		} else {
			keep := room - 1
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

// costBlock is the cost part of the Usage tab. While it loads, or when there is
// no gateway key, it is one note; otherwise it is the table.
func (m *Model) costBlock(w int) []string {
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
			text = "loading cost…"
		}
		return append(out, line(w, seg{text, st.dim}))
	case api.Data == nil:
		return append(out, line(w, seg{"no gateway key — cost hidden", st.dim}))
	}
	return append(out, costLines(*api.Data, w, st)...)
}
