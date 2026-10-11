package panel

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// The storm rules below are the ones in refresh.go (failed, stormThreshold,
// computeStorms and their helpers). refresh.go is package main and cannot be
// imported, so the rules are repeated here. Change both together.

const (
	stormWindow    = 10 * time.Minute
	stormMinCount  = 5 // failures in the window that make a storm active
	stormMin402    = 1 // a 402 (balance or payment) is active on its own
	stormTopAgents = 3
	labelCells     = 40 // longest provider, error type or agent name kept
)

// storm is one provider's recent gateway failures with one status code.
type storm struct {
	Provider  string
	Status    int      // 0 when no HTTP response arrived
	Count     int      // failures in the window
	ErrorType string   // most frequent error type; "" if none
	Agents    []string // busiest failing agents, at most stormTopAgents
	Active    bool
}

// failed reports whether r is a provider-side failure. Only gateway rows count.
// 499 (the client cancelled) and 400 (the caller sent a bad request) say nothing
// about provider health.
func failed(r magpie.Request) bool {
	if r.Source == "log" || r.Status == 499 || r.Status == 400 {
		return false
	}
	return r.Error || r.Status >= 400
}

// stormThreshold is how many failures make a storm active for one status code.
// A 402 means the balance is exhausted, which is worth showing at once.
func stormThreshold(status int) int {
	if status == 402 {
		return stormMin402
	}
	return stormMinCount
}

// computeStorms groups the failures in the stormWindow before now by provider and
// status code, and marks the groups that reach their threshold. Groups are ordered
// by count, then provider and status.
func computeStorms(reqs []magpie.Request, now time.Time) []storm {
	cutoff := now.Add(-stormWindow)
	type groupKey struct {
		provider string
		status   int
	}
	type group struct {
		count    int
		errTypes map[string]int
		agents   map[string]int
	}
	groups := make(map[groupKey]*group)
	for _, r := range reqs {
		if r.Time.Before(cutoff) || !failed(r) {
			continue
		}
		k := groupKey{provider: cmp.Or(label(r.Provider), label(r.Host), "unknown"), status: r.Status}
		g := groups[k]
		if g == nil {
			g = &group{errTypes: map[string]int{}, agents: map[string]int{}}
			groups[k] = g
		}
		g.count++
		if et := label(r.ErrorType); et != "" {
			g.errTypes[et]++
		}
		if a := label(r.Agent); a != "" {
			g.agents[a]++
		}
	}
	storms := make([]storm, 0, len(groups))
	for k, g := range groups {
		s := storm{
			Provider:  k.provider,
			Status:    k.status,
			Count:     g.count,
			ErrorType: topKey(g.errTypes),
			Agents:    topKeys(g.agents, stormTopAgents),
		}
		s.Active = s.Count >= stormThreshold(s.Status)
		storms = append(storms, s)
	}
	slices.SortFunc(storms, func(a, b storm) int {
		return cmp.Or(
			cmp.Compare(b.Count, a.Count),
			strings.Compare(a.Provider, b.Provider),
			cmp.Compare(a.Status, b.Status),
		)
	})
	return storms
}

// statusCode renders an HTTP status for a label. Zero, meaning no response, reads "err".
func statusCode(code int) string {
	if code == 0 {
		return "err"
	}
	return strconv.Itoa(code)
}

// label makes a magpie value safe to group and show: control characters become
// spaces, whitespace runs collapse, and the result is cut to labelCells runes.
func label(s string) string {
	flat := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return truncate(strings.Join(strings.Fields(flat), " "), labelCells)
}

// truncate shortens s to at most limit runes. A cut ends in "…".
func truncate(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	if limit <= 0 {
		return ""
	}
	return string([]rune(s)[:limit-1]) + "…"
}

// topKeys returns up to n names with the highest counts. Ties sort by name.
func topKeys(counts map[string]int, n int) []string {
	names := slices.SortedFunc(maps.Keys(counts), func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), strings.Compare(a, b))
	})
	return names[:min(n, len(names))]
}

// topKey returns the most frequent name, or "" when there are none.
func topKey(counts map[string]int) string {
	if names := topKeys(counts, 1); len(names) > 0 {
		return names[0]
	}
	return ""
}
