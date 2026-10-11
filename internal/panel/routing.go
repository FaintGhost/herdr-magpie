package panel

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// routingRows is how many recent gateway requests the Routing tab lists.
const routingRows = 50

// swapped reports whether the gateway served r from another model than the one
// asked for. The usage log's swapped flag counts too.
func swapped(r magpie.Request) bool {
	return r.Swapped || (r.RequestedModel != "" && r.ServedModel != "" && r.ServedModel != r.RequestedModel)
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

// routeColumns says which optional columns of the routing table fit in w columns.
// The route takes what the other columns leave, so the optional ones go first.
func routeColumns(w int) (agent, provider bool) {
	switch {
	case w >= 72:
		return true, true
	case w >= 56:
		return true, false
	}
	return false, false
}

// routeCells are the texts of one routing table line, each with its style.
type routeCells struct {
	time, agent, route, provider, status, ttft seg
	gap                                        lipgloss.Style
}

// routeLine draws one routing table line in exactly w columns. The columns are
// time, agent, route, provider, http status and time to first token, two spaces
// apart. The agent and provider columns are left out where showAgent and
// showProvider are false. The route fills what the other columns leave.
func routeLine(w int, showAgent, showProvider bool, c routeCells) string {
	type col struct {
		seg
		width int // 0 for the route, which takes the rest
		right bool
	}
	cols := []col{{seg: c.time, width: 8}}
	if showAgent {
		cols = append(cols, col{seg: c.agent, width: 10})
	}
	cols = append(cols, col{seg: c.route})
	if showProvider {
		cols = append(cols, col{seg: c.provider, width: 12})
	}
	cols = append(cols, col{seg: c.status, width: 4, right: true}, col{seg: c.ttft, width: 7, right: true})

	fixed := 2 * (len(cols) - 1)
	for _, k := range cols {
		fixed += k.width
	}
	routeW := max(w-fixed, 0)
	var segs []seg
	for i, k := range cols {
		if i > 0 {
			segs = append(segs, seg{"  ", c.gap})
		}
		width := k.width
		if width == 0 {
			width = routeW
		}
		text := fit(k.text, width)
		if k.right {
			text = rjust(k.text, width)
		}
		segs = append(segs, seg{text, k.style})
	}
	return line(w, segs...)
}

// routeHeader is the routing table's header line.
func routeHeader(w int, showAgent, showProvider bool, st styles) string {
	return routeLine(w, showAgent, showProvider, routeCells{
		time:     seg{"time", st.dim},
		agent:    seg{"agent", st.dim},
		route:    seg{"requested → served", st.dim},
		provider: seg{"provider", st.dim},
		status:   seg{"http", st.dim},
		ttft:     seg{"ttft", st.dim},
		gap:      st.dim,
	})
}

// routeRow is the routing table's line for one request. A swapped request has its
// route highlighted, and a failed one has its status in red.
func routeRow(r magpie.Request, w int, showAgent, showProvider bool, st styles) string {
	route := cmp.Or(clean(r.RequestedModel), "—")
	routeStyle := st.base
	if swapped(r) {
		route += " → " + clean(r.ServedModel)
		routeStyle = st.warn
	}
	statusStyle := st.base
	if isError(r) {
		statusStyle = st.bad
	}
	return routeLine(w, showAgent, showProvider, routeCells{
		time:     seg{r.Time.Local().Format("15:04:05"), st.base},
		agent:    seg{cmp.Or(clean(r.Agent), "—"), st.base},
		route:    seg{route, routeStyle},
		provider: seg{cmp.Or(clean(r.Provider), clean(r.Host), "—"), st.base},
		status:   seg{statusCode(r.Status), statusStyle},
		ttft:     seg{formatTTFT(r.TTFT), st.base},
		gap:      st.base,
	})
}

// routingLines draws the Routing tab in h lines: the summary, the header, then the
// recent gateway requests, newest first.
func (m *Model) routingLines(w, h int) []string {
	st, now := m.st, m.now()
	reqs := m.snap.Requests.Data
	var out []string
	if e := m.feeds[feedRequests].err; e != "" {
		out = append(out, line(w, seg{"requests: " + clean(e), st.warn}))
	}
	switch {
	case m.snap.Requests.At.IsZero() && m.loading(feedRequests):
		return append(out, line(w, seg{"loading…", st.dim}))
	case m.snap.Requests.At.IsZero():
		return append(out, line(w, seg{"no usage data yet; press r to refresh", st.dim}))
	}
	out = append(out, line(w, seg{routingSummary(reqs, now), st.bold}))
	showAgent, showProvider := routeColumns(w)
	out = append(out, routeHeader(w, showAgent, showProvider, st))

	rows := recentGateway(reqs, routingRows)
	room := h - len(out)
	switch {
	case room <= 0:
		return out
	case len(rows) == 0:
		return append(out, line(w, seg{"no gateway requests today", st.dim}))
	}
	shown := min(len(rows), room)
	more := shown < len(rows)
	if more {
		shown = min(len(rows), room-1) // the last line says how many are left out
	}
	for _, r := range rows[:shown] {
		out = append(out, routeRow(r, w, showAgent, showProvider, st))
	}
	if more {
		out = append(out, line(w, seg{fmt.Sprintf("+%d older", len(rows)-shown), st.dim}))
	}
	return out
}
