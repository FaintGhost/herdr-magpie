package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/state"
)

// rep repeats one usage row n times.
func rep(n int, r magpie.Request) []magpie.Request {
	return slices.Repeat([]magpie.Request{r}, n)
}

// summarize renders storms compactly for comparison.
func summarize(storms []Storm) []string {
	out := make([]string, 0, len(storms))
	for _, s := range storms {
		out = append(out, fmt.Sprintf("%s/%d=%d active=%t type=%q agents=%s",
			s.Provider, s.Status, s.Count, s.Active, s.ErrorType, strings.Join(s.Agents, ",")))
	}
	return out
}

func TestComputeStorms(t *testing.T) {
	recent := testNow.Add(-time.Minute)
	gateway := func(provider string, status int, agent, errType string) magpie.Request {
		return magpie.Request{Time: recent, Provider: provider, Status: status, Agent: agent, ErrorType: errType, Error: true, Source: "gateway"}
	}
	tests := []struct {
		name string
		reqs []magpie.Request
		want []string
	}{
		{
			name: "five failures are active, four are not",
			reqs: append(rep(5, gateway("p1", 429, "a1", "RateLimit")), rep(4, gateway("p2", 500, "a2", "Upstream"))...),
			want: []string{
				`p1/429=5 active=true type="RateLimit" agents=a1`,
				`p2/500=4 active=false type="Upstream" agents=a2`,
			},
		},
		{
			name: "a 402 is active on its own",
			reqs: rep(1, gateway("p3", 402, "billing", "InsufficientBalance")),
			want: []string{`p3/402=1 active=true type="InsufficientBalance" agents=billing`},
		},
		{
			name: "499, 400 and log rows never count",
			reqs: slices.Concat(
				rep(6, gateway("p4", 499, "a", "Cancel")),
				rep(6, gateway("p4", 400, "a", "BadRequest")),
				rep(6, magpie.Request{Time: recent, Provider: "p4", Status: 429, Error: true, Source: "log"}),
				rep(6, magpie.Request{Time: recent, Provider: "p4", Status: 200, Source: "gateway"}),
			),
			want: nil,
		},
		{
			name: "window edge: exactly ten minutes ago counts, older does not",
			reqs: slices.Concat(
				rep(5, magpie.Request{Time: testNow.Add(-stormWindow), Provider: "p5", Status: 429, Error: true, Source: "gateway"}),
				rep(5, magpie.Request{Time: testNow.Add(-stormWindow - time.Second), Provider: "p5", Status: 429, Error: true, Source: "gateway"}),
			),
			want: []string{`p5/429=5 active=true type="" agents=`},
		},
		{
			name: "host stands in for a missing provider",
			reqs: slices.Concat(
				rep(5, magpie.Request{Time: recent, Host: "gw.example.test", Status: 503, Error: true, Source: "gateway"}),
				rep(1, magpie.Request{Time: recent, Status: 503, Error: true, Source: "gateway"}),
			),
			want: []string{
				`gw.example.test/503=5 active=true type="" agents=`,
				`unknown/503=1 active=false type="" agents=`,
			},
		},
		{
			name: "busiest agents, most frequent error type",
			reqs: slices.Concat(
				rep(3, gateway("p6", 429, "alpha", "RateLimit")),
				rep(2, gateway("p6", 429, "beta", "RateLimit")),
				rep(2, gateway("p6", 429, "gamma", "Quota")),
				rep(1, gateway("p6", 429, "delta", "Quota")),
			),
			want: []string{`p6/429=8 active=true type="RateLimit" agents=alpha,beta,gamma`},
		},
		{
			name: "order by count, then provider, then status",
			reqs: slices.Concat(
				rep(5, gateway("p7", 500, "a", "")),
				rep(5, gateway("p7", 429, "a", "")),
				rep(6, gateway("p8", 429, "a", "")),
			),
			want: []string{
				`p8/429=6 active=true type="" agents=a`,
				`p7/429=5 active=true type="" agents=a`,
				`p7/500=5 active=true type="" agents=a`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarize(computeStorms(tt.reqs, testNow))
			if !slices.Equal(got, tt.want) {
				t.Errorf("computeStorms() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestStormToast(t *testing.T) {
	tests := []struct {
		name      string
		storm     Storm
		wantTitle string
		wantBody  string
	}{
		{
			name:      "full storm",
			storm:     Storm{Provider: "opencode-go", Status: 429, Count: 6, ErrorType: "GoUsageLimitError", Agents: []string{"alpha", "beta"}},
			wantTitle: "magpie: opencode-go 429 ×6 in 10m",
			wantBody:  "GoUsageLimitError · alpha, beta",
		},
		{
			name:      "no error type or agents",
			storm:     Storm{Provider: "x", Status: 0, Count: 5},
			wantTitle: "magpie: x err ×5 in 10m",
			wantBody:  "error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.storm.toast()
			if got.title != tt.wantTitle || got.body != tt.wantBody || got.key != tt.storm.key() {
				t.Errorf("toast() = %+v, want title %q body %q", got, tt.wantTitle, tt.wantBody)
			}
		})
	}
}

// TestPlanStorms walks a storm through start, continuation, a restart inside
// the cooldown, a restart after it, and a run with notifications off.
func TestPlanStorms(t *testing.T) {
	type step struct {
		minutes int  // offset from testNow
		active  bool // whether the storm is active at this refresh
		enabled bool // notifications on
		toasts  int  // toasts this refresh must plan
	}
	steps := []step{
		{minutes: 0, active: true, enabled: true, toasts: 1},
		{minutes: 1, active: true, enabled: true, toasts: 0},
		{minutes: 2, active: false, enabled: true, toasts: 0},
		{minutes: 5, active: true, enabled: true, toasts: 0},   // restarted inside the cooldown
		{minutes: 40, active: false, enabled: true, toasts: 0}, // cooldown has expired
		{minutes: 41, active: true, enabled: true, toasts: 1},  // a new start toasts again
		{minutes: 42, active: true, enabled: false, toasts: 0}, // still active, notifications off
	}
	var st stormState
	for i, s := range steps {
		now := testNow.Add(time.Duration(s.minutes) * time.Minute)
		var storms []Storm
		if s.active {
			storms = []Storm{{Provider: "p", Status: 429, Count: 6, Active: true}}
		} else {
			storms = []Storm{{Provider: "p", Status: 429, Count: 2}}
		}
		toasts, next := planStorms(now, storms, st, s.enabled)
		if len(toasts) != s.toasts {
			t.Fatalf("step %d (minute %d): %d toasts, want %d", i, s.minutes, len(toasts), s.toasts)
		}
		st = next
	}
}

func TestPlanStormsKeepsActiveSorted(t *testing.T) {
	storms := []Storm{
		{Provider: "b", Status: 500, Count: 9, Active: true},
		{Provider: "a", Status: 429, Count: 7, Active: true},
		{Provider: "c", Status: 429, Count: 1},
	}
	_, next := planStorms(testNow, storms, stormState{}, true)
	want := []string{"a 429", "b 500"}
	if !slices.Equal(next.Active, want) {
		t.Errorf("active = %q, want %q", next.Active, want)
	}
}

func TestModelToken(t *testing.T) {
	long := strings.Repeat("m", 40)
	tests := []struct {
		name   string
		models []magpie.ModelUsage
		want   string
	}{
		{name: "no models", want: ""},
		{
			name:   "provider prefix is removed",
			models: []magpie.ModelUsage{{Model: "opencode-go/kimi-k2", Cost: 0.004}},
			want:   "kimi-k2",
		},
		{
			name: "costliest first, extra models counted",
			models: []magpie.ModelUsage{
				{Model: "openai/gpt-y", Cost: 0.25},
				{Model: "anthropic/claude-sonnet-x", Cost: 1.5},
			},
			want: "claude-sonnet-x +1",
		},
		{
			name: "ties go to more tokens, then name",
			models: []magpie.ModelUsage{
				{Model: "p/small", Input: 1, Output: 1},
				{Model: "p/big", Input: 100, Output: 100},
			},
			want: "big +1",
		},
		{
			name: "same model under two providers is listed once",
			models: []magpie.ModelUsage{
				{Model: "a/m", Cost: 1},
				{Model: "b/m", Cost: 0.5},
			},
			want: "m",
		},
		{
			name:   "empty names are skipped",
			models: []magpie.ModelUsage{{Model: ""}, {Model: "p/"}},
			want:   "",
		},
		{
			name:   "long name is cut with room for the count",
			models: []magpie.ModelUsage{{Model: "p/" + long, Cost: 2}, {Model: "p/other", Cost: 1}},
			want:   strings.Repeat("m", 24) + "… +1",
		},
		{
			name:   "long single name fits in 28 cells",
			models: []magpie.ModelUsage{{Model: "p/" + long}},
			want:   strings.Repeat("m", 27) + "…",
		},
		{
			name:   "control characters become spaces",
			models: []magpie.ModelUsage{{Model: "p/x\ny"}},
			want:   "x y",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modelToken(tt.models); got != tt.want {
				t.Errorf("modelToken() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCostToken(t *testing.T) {
	tests := []struct {
		usd  float64
		want string
	}{
		{usd: 0, want: "$0.00"},
		{usd: 0.004, want: "<$0.01"},
		{usd: 0.005, want: "$0.01"},
		{usd: 1.75, want: "$1.75"},
		{usd: 12.5, want: "$12.50"},
		{usd: 1234.5, want: "$1234.50"},
	}
	for _, tt := range tests {
		if got := costToken(tt.usd); got != tt.want {
			t.Errorf("costToken(%v) = %q, want %q", tt.usd, got, tt.want)
		}
	}
}

func TestSessionAndWorkspaceTokens(t *testing.T) {
	session := magpie.Session{
		Cost: 1.75,
		Models: []magpie.ModelUsage{
			{Model: "anthropic/claude-sonnet-x", Cost: 1.5},
			{Model: "openai/gpt-y", Cost: 0.25},
		},
	}
	bare := magpie.Session{}
	tests := []struct {
		name string
		got  map[string]string
		want map[string]string
	}{
		{
			name: "session with models",
			got:  sessionTokens(session),
			want: map[string]string{"magpie_cost": "$1.75", "magpie_model": "claude-sonnet-x +1"},
		},
		{
			name: "session without models keeps only the cost",
			got:  sessionTokens(bare),
			want: map[string]string{"magpie_cost": "$0.00"},
		},
		{
			name: "workspace sums its open sessions",
			got:  workspaceTokens([]magpie.Session{{Cost: 1.5}, {Cost: 0.25}}),
			want: map[string]string{"magpie_cost": "$1.75"},
		},
		{
			name: "workspace without sessions clears",
			got:  workspaceTokens(nil),
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !maps.Equal(tt.got, tt.want) {
				t.Errorf("tokens = %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestTokenDelta(t *testing.T) {
	tests := []struct {
		name      string
		prev      map[string]string
		next      map[string]string
		wantSet   map[string]string
		wantUnset []string
	}{
		{name: "new target", next: map[string]string{"a": "1"}, wantSet: map[string]string{"a": "1"}},
		{
			name: "changed value", prev: map[string]string{"a": "1", "b": "2"}, next: map[string]string{"a": "1", "b": "3"},
			wantSet: map[string]string{"b": "3"},
		},
		{
			name: "removed token", prev: map[string]string{"a": "1", "b": "2"}, next: map[string]string{"a": "1"},
			wantUnset: []string{"b"},
		},
		{name: "target cleared", prev: map[string]string{"a": "1"}, wantUnset: []string{"a"}},
		{name: "unchanged is silent", prev: map[string]string{"a": "1"}, next: map[string]string{"a": "1"}},
		{
			name: "replaced set sorts its clears", prev: map[string]string{"a": "1", "b": "2"}, next: map[string]string{"c": "3"},
			wantSet: map[string]string{"c": "3"}, wantUnset: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, unset := tokenDelta(tt.prev, tt.next)
			if !maps.Equal(set, tt.wantSet) || !slices.Equal(unset, tt.wantUnset) {
				t.Errorf("tokenDelta() = (%v, %v), want (%v, %v)", set, unset, tt.wantSet, tt.wantUnset)
			}
		})
	}
}

func TestEventWantsRefresh(t *testing.T) {
	tests := []struct {
		name string
		ev   herdr.Event
		want bool
	}{
		{name: "idle", ev: herdr.Event{Name: "pane.agent_status_changed", Status: "idle"}, want: true},
		{name: "done", ev: herdr.Event{Name: "pane.agent_status_changed", Status: "done"}, want: true},
		{name: "blocked", ev: herdr.Event{Name: "pane.agent_status_changed", Status: "blocked"}, want: true},
		{name: "working", ev: herdr.Event{Name: "pane.agent_status_changed", Status: "working"}, want: false},
		{name: "unknown status", ev: herdr.Event{Name: "pane.agent_status_changed", Status: "unknown"}, want: false},
		{name: "agent detected", ev: herdr.Event{Name: "pane.agent_detected"}, want: true},
		{name: "other event", ev: herdr.Event{Name: "pane.closed"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventWantsRefresh(tt.ev); got != tt.want {
				t.Errorf("eventWantsRefresh() = %t, want %t", got, tt.want)
			}
		})
	}
}

// sessionCalls counts the magpie sessions calls among calls.
func sessionCalls(calls []string) int {
	n := 0
	for _, c := range calls {
		if strings.HasPrefix(c, "magpie sessions") {
			n++
		}
	}
	return n
}

// TestRefreshEndToEnd runs refresh four times against fake CLIs. Refresh reads
// sessions and never usage. The first run sets tokens. The second changes
// nothing and writes nothing. The third has a new cost and a lost session, so it
// sets one token and clears two. The fourth comes within refreshMinGap and does
// nothing at all.
func TestRefreshEndToEnd(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTools{out: map[string]string{
		"magpie sessions --json --limit 100000": sessionsJSON(t, 1.75),
		"herdr agent list":                      agentsJSON(true),
		"herdr workspace list":                  workspacesJSON,
	}}
	var stderr bytes.Buffer
	clock := base
	d := refreshDeps{
		cfg:       defaultConfig(),
		store:     store,
		runner:    fake,
		magpieBin: "magpie",
		herdr:     &herdr.Client{Bin: "herdr", Runner: fake},
		now:       func() time.Time { return clock },
		stderr:    &stderr,
	}
	run := func(at time.Time) []string {
		t.Helper()
		clock = at
		fake.calls = nil
		if err := runRefresh(t.Context(), d); err != nil {
			t.Fatalf("runRefresh() error = %v", err)
		}
		return slices.Clone(fake.calls)
	}

	first := run(base)
	wantFirst := []string{
		"magpie sessions --json --limit 100000",
		"herdr agent list",
		"herdr pane report-metadata p1 --source faintghost.magpie --token magpie_cost=$1.75 --token \"magpie_model=claude-sonnet-x +1\"",
		"herdr pane report-metadata p2 --source faintghost.magpie --token magpie_cost=<$0.01 --token magpie_model=kimi-k2",
		"herdr workspace list",
		"herdr workspace report-metadata w1 --source faintghost.magpie --token magpie_cost=$1.75",
	}
	if !slices.Equal(first, wantFirst) {
		t.Errorf("first run calls =\n%q\nwant\n%q", first, wantFirst)
	}

	second := run(base.Add(time.Minute))
	wantSecond := []string{
		"magpie sessions --json --limit 100000",
		"herdr agent list",
		"herdr workspace list",
	}
	if !slices.Equal(second, wantSecond) {
		t.Errorf("second run calls = %q, want only reads %q", second, wantSecond)
	}

	fake.out["magpie sessions --json --limit 100000"] = sessionsJSON(t, 2.10)
	fake.out["herdr agent list"] = agentsJSON(false)
	third := run(base.Add(20 * time.Minute))
	wantThird := []string{
		"magpie sessions --json --limit 100000",
		"herdr agent list",
		"herdr pane report-metadata p1 --source faintghost.magpie --token magpie_cost=$2.10",
		"herdr pane report-metadata p2 --source faintghost.magpie --clear-token magpie_cost --clear-token magpie_model",
		"herdr workspace list",
		"herdr workspace report-metadata w1 --source faintghost.magpie --token magpie_cost=$2.10",
	}
	if !slices.Equal(third, wantThird) {
		t.Errorf("third run calls =\n%q\nwant\n%q", third, wantThird)
	}

	if fourth := run(base.Add(20*time.Minute + time.Second)); len(fourth) != 0 {
		t.Errorf("run within refreshMinGap made calls %q, want none", fourth)
	}

	var st refreshState
	if err := store.Load(refreshFile, &st); err != nil {
		t.Fatal(err)
	}
	if got := st.Panes["p1"]; !maps.Equal(got, map[string]string{"magpie_cost": "$2.10", "magpie_model": "claude-sonnet-x +1"}) {
		t.Errorf("pane p1 state = %v", got)
	}
	if _, ok := st.Panes["p2"]; ok {
		t.Errorf("pane p2 still in state after its tokens were cleared: %v", st.Panes["p2"])
	}
	if !maps.Equal(st.Workspaces["w1"], map[string]string{"magpie_cost": "$2.10"}) {
		t.Errorf("workspace w1 state = %v", st.Workspaces["w1"])
	}
	if want := base.Add(20 * time.Minute); !st.LastOK.Equal(want) || !st.LastRun.Equal(want) {
		t.Errorf("last ok %v, last run %v; want both %v", st.LastOK, st.LastRun, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
}

// TestRefreshEventThrottle checks the event throttle. An event-triggered run is
// skipped within eventMinGap of the last successful refresh, other runs are not,
// and refreshMinGap still applies to every run.
func TestRefreshEventThrottle(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTools{out: map[string]string{
		"magpie sessions --json --limit 100000": sessionsJSON(t, 1),
		"herdr agent list":                      agentsJSON(false),
		"herdr workspace list":                  workspacesJSON,
	}}
	clock := base
	d := refreshDeps{
		cfg:       defaultConfig(),
		store:     store,
		runner:    fake,
		magpieBin: "magpie",
		herdr:     &herdr.Client{Bin: "herdr", Runner: fake},
		now:       func() time.Time { return clock },
		stderr:    new(bytes.Buffer),
	}
	steps := []struct {
		at    time.Duration
		event bool
		want  int // sessions calls this run makes
	}{
		{at: 0, event: true, want: 1},                  // no successful refresh yet
		{at: 30 * time.Second, event: true, want: 0},   // within eventMinGap of that success
		{at: 40 * time.Second, event: false, want: 1},  // a startup or manual run is not throttled
		{at: 70 * time.Second, event: true, want: 0},   // 30 s after that success
		{at: 101 * time.Second, event: true, want: 1},  // 61 s after it
		{at: 102 * time.Second, event: false, want: 0}, // within refreshMinGap of the last run
	}
	for i, s := range steps {
		clock = base.Add(s.at)
		d.event = s.event
		fake.calls = nil
		if err := runRefresh(t.Context(), d); err != nil {
			t.Fatalf("step %d: runRefresh() error = %v", i, err)
		}
		if got := sessionCalls(fake.calls); got != s.want {
			t.Errorf("step %d (at %v, event %t): %d sessions calls, want %d", i, s.at, s.event, got, s.want)
		}
		if s.want == 0 && len(fake.calls) != 0 {
			t.Errorf("step %d: skipped run made calls %q", i, fake.calls)
		}
	}
}

// TestRefreshFailedSessionsKeepsLastOK checks that a run whose sessions read
// fails is not a successful refresh. Status keeps its own retry schedule, and
// event runs are not throttled by the failed attempt.
func TestRefreshFailedSessionsKeepsLastOK(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTools{} // every magpie call fails
	d := refreshDeps{
		cfg:       defaultConfig(),
		store:     store,
		runner:    fake,
		magpieBin: "magpie",
		herdr:     &herdr.Client{Bin: "herdr", Runner: fake},
		now:       func() time.Time { return base },
		stderr:    new(bytes.Buffer),
	}
	if err := runRefresh(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	var st refreshState
	if err := store.Load(refreshFile, &st); err != nil {
		t.Fatal(err)
	}
	if !st.LastOK.IsZero() {
		t.Errorf("last ok = %v after a failed read, want none", st.LastOK)
	}
	if !st.LastRun.Equal(base) {
		t.Errorf("last run = %v, want %v", st.LastRun, base)
	}
}

// sessionsJSON is synthetic `magpie sessions --json` output. costA sets the
// first session's cost, so tests can change it between refreshes.
func sessionsJSON(t *testing.T, costA float64) string {
	t.Helper()
	data, err := json.Marshal([]map[string]any{
		{
			"agent": "claude", "id": "AAAAAAAA-1111-4222-8333-444444444444", "cwd": "/work/a", "title": "a",
			"start": time.Now().Add(-2 * time.Hour).Format(time.RFC3339), "last": time.Now().Add(-time.Hour).Format(time.RFC3339),
			"models": []map[string]any{
				{"model": "anthropic/claude-sonnet-x", "input": 100, "output": 50, "cost": 1.5, "priced": true},
				{"model": "openai/gpt-y", "input": 10, "output": 5, "cost": 0.25, "priced": true},
			},
			"cost": costA,
		},
		{
			"agent": "omp", "id": "bbbbbbbb-2222-4333-8444-555555555555", "cwd": "/work/b", "title": "b",
			"start": time.Now().Add(-3 * time.Hour).Format(time.RFC3339), "last": time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
			"models": []map[string]any{
				{"model": "opencode-go/kimi-k2", "input": 1, "output": 1, "cost": 0.004, "priced": true},
			},
			"cost": 0.004,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// agentsJSON is synthetic `herdr agent list` output. p1 is a Claude session by
// id, p2 an omp session by transcript path, and p3 has no session. withP2Session
// false clears p2's session, as if the agent had lost it.
func agentsJSON(withP2Session bool) string {
	p2Session := `null`
	if withP2Session {
		p2Session = `{"agent":"omp","kind":"path","source":"herdr","value":"/home/u/.omp/sessions/2026-10-12_bbbbbbbb-2222-4333-8444-555555555555.jsonl"}`
	}
	return `{"id":"1","result":{"agents":[` +
		`{"agent":"claude","agent_status":"idle","cwd":"/work/a","pane_id":"p1","tab_id":"t1","workspace_id":"w1","terminal_title_stripped":"a","focused":false,` +
		`"agent_session":{"agent":"claude","kind":"id","source":"herdr","value":"aaaaaaaa-1111-4222-8333-444444444444"}},` +
		`{"agent":"omp","agent_status":"working","cwd":"/work/b","pane_id":"p2","tab_id":"t1","workspace_id":"w1","terminal_title_stripped":"b","focused":false,` +
		`"agent_session":` + p2Session + `},` +
		`{"agent":"pi","agent_status":"idle","cwd":"/work/c","pane_id":"p3","tab_id":"t2","workspace_id":"w2","terminal_title_stripped":"c","focused":false,"agent_session":null}` +
		`]}}`
}

const workspacesJSON = `{"id":"2","result":{"workspaces":[` +
	`{"workspace_id":"w1","label":"one","focused":true},` +
	`{"workspace_id":"w2","label":"two","focused":false}]}}`
