package panel

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// routingRows is how many recent gateway requests the Routing tab lists.
const routingRows = 50

// Columns of the routing table, in cells. The route takes the room that the other
// columns leave, up to routeRouteMax, but is not cut below routeRouteMin: a narrower
// table drops columns instead.
const (
	routeAgentMax = 14 // "Go-http-client", the longest agent name the gateway logs
	routeProvMax  = 12
	routeRouteMin = 24
	routeRouteMax = 60
)

// swapped reports whether the gateway served r from another model than the one asked
// for. When both names are known they decide, with the provider prefix and the effort
// suffix set aside, so "codex/gpt-6-astra" served as "gpt-6-astra" is no swap. The usage
// log's flag counts only when a name is missing.
func swapped(r magpie.Request) bool {
	if r.RequestedModel != "" && r.ServedModel != "" {
		return !magpie.SameModel(r.RequestedModel, r.ServedModel)
	}
	return r.Swapped
}

// isError reports whether r failed in any way, for the red status. Unlike failed,
// it counts 400 and 499 too, since the table shows what the caller saw.
func isError(r magpie.Request) bool {
	return r.Error || r.Status >= 400
}

// recentGateway returns up to n gateway requests, newest first. A log row is a
// direct call from an agent and never passed the gateway, so it is left out.
func recentGateway(reqs []magpie.Request, n int) []magpie.Request {
	var out []magpie.Request
	for _, r := range reqs {
		if r.Source != "log" {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b magpie.Request) int { return b.Time.Compare(a.Time) })
	return out[:min(n, len(out))]
}

// hourTotals counts the gateway requests of the last hour, the swapped ones among
// them, and the provider failures (the storm rule's, so the Usage tab agrees).
func hourTotals(reqs []magpie.Request, now time.Time) (total, swaps, failures int) {
	since := now.Add(-time.Hour)
	for _, r := range reqs {
		if r.Source == "log" || r.Time.Before(since) {
			continue
		}
		total++
		if swapped(r) {
			swaps++
		}
		if failed(r) {
			failures++
		}
	}
	return total, swaps, failures
}

// routingSummary is the line above the table: the swaps and the provider error rate
// of the last hour.
func routingSummary(reqs []magpie.Request, now time.Time) string {
	total, swaps, failures := hourTotals(reqs, now)
	if total == 0 {
		return "last 1h · no gateway requests"
	}
	rate := strconv.FormatFloat(100*float64(failures)/float64(total), 'f', 1, 64)
	return fmt.Sprintf("last 1h · %d swapped · provider errors %s%% (%d of %d)", swaps, rate, failures, total)
}

// formatTTFT is a time to first token: "—" when unknown, "842ms" under a second,
// and "12.3s" from a second up.
func formatTTFT(d time.Duration) string {
	switch {
	case d <= 0:
		return "—"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// routeCols is the width of each column of the routing table, 0 where a column is left out.
type routeCols struct {
	time, agent, route, provider, status, ttft int
}

// routeAgentText is the agent column's text for r.
func routeAgentText(r magpie.Request) string {
	return cmp.Or(clean(r.Agent), "—")
}

// routeProviderText is the provider column's text for r: its provider, or else its host.
func routeProviderText(r magpie.Request) string {
	return cmp.Or(clean(r.Provider), clean(r.Host), "—")
}

// routeText is the route column's text for r: the model asked for, and the model served
// after an arrow when the two really differ.
func routeText(r magpie.Request) string {
	route := cmp.Or(clean(r.RequestedModel), "—")
	if swapped(r) && r.ServedModel != "" {
		route += " → " + clean(r.ServedModel)
	}
	return route
}

// routeColumns sizes the routing table in cw cells, for the rows it lists. The columns
// come in priority order: the time, the route and the status first, then the agent,
// the provider and the time to first token. A narrow table drops the time to first
// token first, and then the provider.
func routeColumns(cw int, rows []magpie.Request) routeCols {
	agent, provider, route := displayWidth("AGENT"), displayWidth("PROVIDER"), displayWidth("REQUESTED → SERVED")
	for _, r := range rows {
		agent = max(agent, displayWidth(routeAgentText(r)))
		provider = max(provider, displayWidth(routeProviderText(r)))
		route = max(route, displayWidth(routeText(r)))
	}
	agent, provider = min(agent, routeAgentMax), min(provider, routeProvMax)
	widths := fitColumns(cw, tableGap, []column{
		{min: 8, want: 8},
		{min: routeRouteMin, want: min(max(route, routeRouteMin), routeRouteMax), max: min(max(route, routeRouteMin), routeRouteMax), flex: true},
		{min: 4, want: 4},
		{min: agent, want: agent},
		{min: provider, want: provider},
		{min: 7, want: 7},
	})
	return routeCols{time: widths[0], route: widths[1], status: widths[2], agent: widths[3], provider: widths[4], ttft: widths[5]}
}

// routeHeader is the routing table's header line, dimmed.
func routeHeader(bw int, cols routeCols, st styles) string {
	return markLine(bw, false, st, []cell{
		{text: "TIME", style: st.dim, width: cols.time},
		{text: "AGENT", style: st.dim, width: cols.agent},
		{text: "REQUESTED → SERVED", style: st.dim, width: cols.route},
		{text: "PROVIDER", style: st.dim, width: cols.provider},
		{text: "HTTP", style: st.dim, width: cols.status, right: true},
		{text: "TTFT", style: st.dim, width: cols.ttft, right: true},
	})
}

// routeRow is the routing table's line for one request. A swapped request has its
// route highlighted, and a failed one has its status in red.
func routeRow(r magpie.Request, bw int, cols routeCols, st styles) string {
	routeStyle := st.base
	if swapped(r) && r.ServedModel != "" {
		routeStyle = st.warn
	}
	statusStyle := st.base
	if isError(r) {
		statusStyle = st.bad
	}
	return markLine(bw, false, st, []cell{
		{text: r.Time.Local().Format("15:04:05"), style: st.base, width: cols.time},
		{text: routeAgentText(r), style: st.base, width: cols.agent},
		{text: routeText(r), style: routeStyle, width: cols.route},
		{text: routeProviderText(r), style: st.base, width: cols.provider},
		{text: statusCode(r.Status), style: statusStyle, width: cols.status, right: true},
		{text: formatTTFT(r.TTFT), style: st.base, width: cols.ttft, right: true},
	})
}

// routingLines draws the Routing tab in h lines: the summary, the header, then the
// recent gateway requests, newest first.
func (m *Model) routingLines(w, h int) []string {
	st, now := m.st, m.now()
	bw, cw := splitWidth(w)
	reqs := m.snap.Requests.Data
	var out []string
	if e := m.feeds[feedRequests].err; e != "" {
		out = append(out, noteLine(bw, st.warn, "requests: "+clean(e)))
	}
	switch {
	case m.snap.Requests.At.IsZero() && m.loading(feedRequests):
		return append(out, noteLine(bw, st.dim, "loading…"))
	case m.snap.Requests.At.IsZero():
		return append(out, noteLine(bw, st.dim, "no usage data yet; press r to refresh"))
	}
	out = append(out, noteLine(bw, st.bold, routingSummary(reqs, now)))
	rows := recentGateway(reqs, routingRows)
	cols := routeColumns(cw, rows)
	out = append(out, routeHeader(bw, cols, st))

	room := h - len(out)
	switch {
	case room <= 0:
		return out
	case len(rows) == 0:
		return append(out, noteLine(bw, st.dim, "no gateway requests today"))
	}
	shown := min(len(rows), room)
	more := shown < len(rows)
	if more {
		shown = min(len(rows), room-1) // the last line says how many are left out
	}
	for _, r := range rows[:shown] {
		out = append(out, routeRow(r, bw, cols, st))
	}
	if more {
		out = append(out, noteLine(bw, st.dim, fmt.Sprintf("+%d older", len(rows)-shown)))
	}
	return out
}
