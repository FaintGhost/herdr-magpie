package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/state"
)

const (
	stormWindow    = 10 * time.Minute
	stormMinCount  = 5 // failures in the window that make a storm active
	stormMin402    = 1 // a 402 (balance or payment) is active on its own
	stormCooldown  = 30 * time.Minute
	stormTopAgents = 3
	labelCells     = 40 // longest provider, error type or agent name shown
	modelCells     = 28 // longest magpie_model token

	refreshMinGap  = 5 * time.Second // a run this soon after the last one repeats it and returns
	eventMinGap    = time.Minute     // an event-triggered run is skipped this soon after a successful one
	refreshTimeout = 60 * time.Second

	metadataSource = "faintghost.magpie"
	tokenModel     = "magpie_model"
	tokenCost      = "magpie_cost"
)

// The storm functions below are pure. Status runs them on every tick, against
// the usage rows of the last stormWindow.

// Storm is one provider's recent gateway failures with one status code.
type Storm struct {
	Provider  string
	Status    int      // 0 when no HTTP response arrived
	Count     int      // failures in the window
	ErrorType string   // most frequent error_type; empty if none
	Agents    []string // busiest failing agents, at most stormTopAgents
	Active    bool
}

// key identifies a storm across ticks.
func (s Storm) key() string {
	return s.Provider + " " + strconv.Itoa(s.Status)
}

// toast is the notification for a storm that has just started.
func (s Storm) toast() stormToast {
	body := cmp.Or(s.ErrorType, "error")
	if len(s.Agents) > 0 {
		body += " · " + strings.Join(s.Agents, ", ")
	}
	return stormToast{
		key:   s.key(),
		title: fmt.Sprintf("magpie: %s %s ×%d in %dm", s.Provider, statusCode(s.Status), s.Count, int(stormWindow.Minutes())),
		body:  body,
	}
}

// failed reports whether a request is a provider-side failure. Only gateway
// rows count, since log rows come from agents that talk to providers directly.
// 499 (the client cancelled) and 400 (the caller sent a bad request) say
// nothing about provider health.
func failed(r magpie.Request) bool {
	if r.Source == "log" || r.Status == 499 || r.Status == 400 {
		return false
	}
	return r.Error || r.Status >= 400
}

// stormThreshold is how many failures make a storm active for one status code.
// A 402 means the balance is exhausted, which is worth reporting at once.
func stormThreshold(status int) int {
	if status == 402 {
		return stormMin402
	}
	return stormMinCount
}

// computeStorms groups the failures in the window before now by provider and
// status code, and marks the groups that reach their threshold. Groups are
// ordered by count, then provider and status.
func computeStorms(reqs []magpie.Request, now time.Time) []Storm {
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
	storms := make([]Storm, 0, len(groups))
	for k, g := range groups {
		s := Storm{
			Provider:  k.provider,
			Status:    k.status,
			Count:     g.count,
			ErrorType: topKey(g.errTypes),
			Agents:    topKeys(g.agents, stormTopAgents),
		}
		s.Active = s.Count >= stormThreshold(s.Status)
		storms = append(storms, s)
	}
	slices.SortFunc(storms, func(a, b Storm) int {
		return cmp.Or(
			cmp.Compare(b.Count, a.Count),
			strings.Compare(a.Provider, b.Provider),
			cmp.Compare(a.Status, b.Status),
		)
	})
	return storms
}

// stormToast is one notification and the storm it belongs to.
type stormToast struct {
	key   string
	title string
	body  string
}

// planStorms returns the toasts for storms that became active since the last
// tick and are outside their cooldown, and the storm state to keep. A storm
// that stays active does not toast again. When enabled is false nothing is
// planned, but the active set is still tracked.
func planStorms(now time.Time, storms []Storm, prev stormState, enabled bool) ([]stormToast, stormState) {
	var next stormState
	for k, at := range prev.LastToast {
		if now.Sub(at) < stormCooldown {
			if next.LastToast == nil {
				next.LastToast = make(map[string]time.Time)
			}
			next.LastToast[k] = at
		}
	}
	var toasts []stormToast
	for _, s := range storms {
		if !s.Active {
			continue
		}
		k := s.key()
		next.Active = append(next.Active, k)
		if !enabled || slices.Contains(prev.Active, k) {
			continue
		}
		if _, cooling := next.LastToast[k]; cooling {
			continue
		}
		if next.LastToast == nil {
			next.LastToast = make(map[string]time.Time)
		}
		toasts = append(toasts, s.toast())
		next.LastToast[k] = now
	}
	slices.Sort(next.Active)
	return toasts, next
}

// statusCode renders an HTTP status for a label. Zero, meaning no response, reads as "err".
func statusCode(code int) string {
	if code == 0 {
		return "err"
	}
	return strconv.Itoa(code)
}

// label makes a magpie value safe for a one-line label. Control characters
// become spaces, whitespace runs collapse, and the result is cut to labelCells.
func label(s string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return truncate(strings.Join(strings.Fields(clean), " "), labelCells)
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

// modelToken names the models a session used: costliest first, provider prefix
// removed, duplicates merged. More than one model adds " +N". The name is cut
// with "…" so that the whole token fits in modelCells.
func modelToken(models []magpie.ModelUsage) string {
	var names []string
	for _, m := range slices.SortedFunc(slices.Values(models), byUse) {
		name := label(stripProvider(m.Model))
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	suffix := ""
	if extra := len(names) - 1; extra > 0 {
		suffix = " +" + strconv.Itoa(extra)
	}
	return truncate(names[0], modelCells-utf8.RuneCountInString(suffix)) + suffix
}

// byUse orders models by cost, then tokens, then name, so the choice is deterministic.
func byUse(a, b magpie.ModelUsage) int {
	return cmp.Or(
		cmp.Compare(b.Cost, a.Cost),
		cmp.Compare(b.Input+b.Output, a.Input+a.Output),
		strings.Compare(a.Model, b.Model),
	)
}

// stripProvider removes the "<provider>/" prefix from a magpie model name.
func stripProvider(model string) string {
	if _, rest, ok := strings.Cut(model, "/"); ok {
		return rest
	}
	return model
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

// costToken formats a dollar amount. A small non-zero amount reads "<$0.01"
// rather than "$0.00", which would suggest nothing was spent.
func costToken(usd float64) string {
	if usd > 0 && usd < 0.005 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", usd)
}

// sessionTokens are the pane tokens for a session that herdr has open.
func sessionTokens(s magpie.Session) map[string]string {
	tokens := map[string]string{tokenCost: costToken(s.Cost)}
	if m := modelToken(s.Models); m != "" {
		tokens[tokenModel] = m
	}
	return tokens
}

// workspaceTokens sums the costs of a workspace's distinct open sessions. With
// no sessions it returns nil, which clears the token.
func workspaceTokens(sessions []magpie.Session) map[string]string {
	if len(sessions) == 0 {
		return nil
	}
	var total float64
	for _, s := range sessions {
		total += s.Cost
	}
	return map[string]string{tokenCost: costToken(total)}
}

// tokenDelta returns the tokens to set and the names to clear that move a
// target from prev to next. Unchanged values produce nothing.
func tokenDelta(prev, next map[string]string) (set map[string]string, unset []string) {
	set = make(map[string]string)
	for name, value := range next {
		if old, ok := prev[name]; !ok || old != value {
			set[name] = value
		}
	}
	for name := range prev {
		if _, ok := next[name]; !ok {
			unset = append(unset, name)
		}
	}
	slices.Sort(unset)
	return set, unset
}

// setTokens records the tokens that a target now has. A target with none is forgotten.
func setTokens(all map[string]map[string]string, id string, tokens map[string]string) map[string]map[string]string {
	if len(tokens) == 0 {
		delete(all, id)
		return all
	}
	if all == nil {
		all = make(map[string]map[string]string)
	}
	all[id] = tokens
	return all
}

// refreshState is refresh.json. Only refresh writes it, under the lock.
type refreshState struct {
	LastRun    time.Time                    `json:"last_run,omitzero"` // last run that got past the lock and the event filter
	LastOK     time.Time                    `json:"last_ok,omitzero"`  // last run that read sessions, agents and workspaces
	Panes      map[string]map[string]string `json:"panes,omitempty"`
	Workspaces map[string]map[string]string `json:"workspaces,omitempty"`
}

// refreshDeps are the effects of one refresh. Tests substitute fakes.
type refreshDeps struct {
	cfg       config
	store     *state.Store
	runner    magpie.Runner
	magpieBin string        // empty when the magpie CLI is missing
	herdr     *herdr.Client // nil when the herdr CLI is missing
	event     bool          // triggered by a herdr event hook, which is subject to eventMinGap
	now       func() time.Time
	stderr    io.Writer
}

// cmdRefresh runs one refresh. Herdr's event hooks come through here too, and
// events that cannot change the display are dropped before any CLI runs.
func cmdRefresh(ctx context.Context, e env, args []string) error {
	fs := newFlagSet("refresh")
	paths := addPathFlags(fs)
	if err := parseFlags(fs, e.stdout, args); err != nil {
		return err
	}
	raw := os.Getenv("HERDR_PLUGIN_EVENT_JSON")
	if raw != "" {
		if ev, err := herdr.ParseEvent(raw); err == nil && !eventWantsRefresh(ev) {
			return nil
		}
	}
	configPath, stateDir, err := resolvePaths(paths)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	store, err := state.Open(stateDir)
	if err != nil {
		return err
	}
	d := newRefreshDeps(e, cfg, store)
	d.event = raw != ""
	return runRefresh(ctx, d)
}

// eventWantsRefresh reports whether a hook event can change what refresh shows:
// an agent was detected, or one became idle, done or blocked.
func eventWantsRefresh(ev herdr.Event) bool {
	switch ev.Status {
	case "idle", "done", "blocked":
		return true
	}
	return ev.Name == "pane.agent_detected"
}

// newRefreshDeps wires a refresh to the real CLIs. A missing CLI is reported
// and left empty, which skips the steps that need it.
func newRefreshDeps(e env, cfg config, store *state.Store) refreshDeps {
	d := refreshDeps{cfg: cfg, store: store, runner: magpie.ExecRunner{}, now: e.now, stderr: e.stderr}
	if bin, err := magpie.FindBin(cfg.MagpieBin); err != nil {
		logf(e.stderr, "refresh: %v; skipping session tokens", err)
	} else {
		d.magpieBin = bin
	}
	if bin, err := herdr.FindBin(cfg.HerdrBin); err != nil {
		logf(e.stderr, "refresh: %v; skipping herdr writes", err)
	} else {
		d.herdr = &herdr.Client{Bin: bin, Runner: e.herdrRunner()}
	}
	return d
}

// runRefresh does one refresh under the state lock. A busy lock means another
// refresh covers this one. A run within refreshMinGap of the last one repeats
// it, and an event-triggered run within eventMinGap of the last successful one
// is skipped, so both return quietly.
func runRefresh(ctx context.Context, d refreshDeps) error {
	unlock, ok, err := d.store.Lock(true)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	defer unlock()

	var st refreshState
	if err := d.store.Load(refreshFile, &st); err != nil {
		logf(d.stderr, "refresh: %v; starting from empty state", err)
		st = refreshState{}
	}
	now := d.now()
	if gap := now.Sub(st.LastRun); gap >= 0 && gap < refreshMinGap {
		return nil
	}
	if gap := now.Sub(st.LastOK); d.event && gap >= 0 && gap < eventMinGap {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	if d.magpieBin != "" && d.herdr != nil {
		if d.refreshTokens(ctx, &st) {
			st.LastOK = now
		}
	}
	st.LastRun = now
	return d.store.Save(refreshFile, st)
}

// refreshTokens sets magpie_model and magpie_cost on each agent pane, and
// magpie_cost on each workspace, from the sessions herdr reports as open. Only
// changed targets get a call. A failed read skips the step rather than clearing
// tokens. A failed write keeps the old state, so the next refresh retries it.
// It reports whether every read succeeded.
func (d refreshDeps) refreshTokens(ctx context.Context, st *refreshState) bool {
	sessions, err := magpie.Sessions(ctx, d.runner, d.magpieBin, d.cfg.SessionDays)
	if err != nil {
		logf(d.stderr, "refresh: sessions: %v", err)
		return false
	}
	agents, err := d.herdr.Agents(ctx)
	if err != nil {
		logf(d.stderr, "refresh: agents: %v", err)
		return false
	}
	byKey := magpie.SessionsByKey(sessions)

	panes := make(map[string]map[string]string, len(agents)) // pane -> wanted tokens; nil clears
	open := make(map[string][]magpie.Session)                // workspace -> distinct open sessions
	counted := make(map[string]bool)
	for _, a := range agents {
		if a.PaneID == "" {
			continue
		}
		key := herdr.SessionKey(a.Session)
		s, ok := byKey[key]
		if !ok {
			panes[a.PaneID] = nil
			continue
		}
		panes[a.PaneID] = sessionTokens(s)
		if mark := a.WorkspaceID + "\x00" + key; !counted[mark] {
			counted[mark] = true
			open[a.WorkspaceID] = append(open[a.WorkspaceID], s)
		}
	}
	for _, pane := range slices.Sorted(maps.Keys(panes)) {
		next := panes[pane]
		set, unset := tokenDelta(st.Panes[pane], next)
		if len(set)+len(unset) == 0 {
			continue
		}
		if err := d.herdr.ReportPaneTokens(ctx, pane, metadataSource, set, unset); err != nil {
			logf(d.stderr, "refresh: pane tokens: %v", err)
			continue
		}
		st.Panes = setTokens(st.Panes, pane, next)
	}
	maps.DeleteFunc(st.Panes, func(pane string, _ map[string]string) bool {
		_, live := panes[pane]
		return !live
	})

	workspaces, err := d.herdr.Workspaces(ctx)
	if err != nil {
		logf(d.stderr, "refresh: workspaces: %v", err)
		return false
	}
	live := make(map[string]bool, len(workspaces))
	for _, ws := range workspaces {
		if ws.ID != "" {
			live[ws.ID] = true
		}
	}
	for _, id := range slices.Sorted(maps.Keys(live)) {
		next := workspaceTokens(open[id])
		set, unset := tokenDelta(st.Workspaces[id], next)
		if len(set)+len(unset) == 0 {
			continue
		}
		if err := d.herdr.ReportWorkspaceTokens(ctx, id, metadataSource, set, unset); err != nil {
			logf(d.stderr, "refresh: workspace tokens: %v", err)
			continue
		}
		st.Workspaces = setTokens(st.Workspaces, id, next)
	}
	maps.DeleteFunc(st.Workspaces, func(id string, _ map[string]string) bool {
		return !live[id]
	})
	return true
}
