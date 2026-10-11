package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/state"
)

// testNow is the fixed clock for the pure tests.
var testNow = time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)

func TestGatewayObserve(t *testing.T) {
	type step struct {
		tr   transition
		down bool // whether the gateway shows as down after the step
	}
	tests := []struct {
		name   string
		probes []bool
		want   []step
	}{
		{
			name:   "steady up",
			probes: []bool{true, true},
			want:   []step{{noChange, false}, {noChange, false}},
		},
		{
			name:   "one failure is not enough",
			probes: []bool{true, false},
			want:   []step{{noChange, false}, {noChange, false}},
		},
		{
			name:   "two failures confirm down",
			probes: []bool{true, false, false},
			want:   []step{{noChange, false}, {noChange, false}, {wentDown, true}},
		},
		{
			name:   "staying down is quiet",
			probes: []bool{true, false, false, false},
			want:   []step{{noChange, false}, {noChange, false}, {wentDown, true}, {noChange, true}},
		},
		{
			name:   "recovery toasts",
			probes: []bool{true, false, false, true},
			want:   []step{{noChange, false}, {noChange, false}, {wentDown, true}, {cameBack, false}},
		},
		{
			name:   "a blip never toasts",
			probes: []bool{true, false, true, false, true},
			want:   []step{{noChange, false}, {noChange, false}, {noChange, false}, {noChange, false}, {noChange, false}},
		},
		{
			name:   "down seen first is confirmed without a toast",
			probes: []bool{false, false},
			want:   []step{{noChange, false}, {noChange, true}},
		},
		{
			name:   "recovery after a quiet down toasts back",
			probes: []bool{false, false, true},
			want:   []step{{noChange, false}, {noChange, true}, {cameBack, false}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g gatewayState
			for i, up := range tt.probes {
				var tr transition
				g, tr = g.observe(up, testNow.Add(time.Duration(i)*time.Minute))
				if want := tt.want[i]; tr != want.tr || g.down() != want.down {
					t.Fatalf("probe %d (up=%t): transition %d, down %t; want transition %d, down %t",
						i, up, tr, g.down(), want.tr, want.down)
				}
			}
		})
	}
}

func TestStatusLine(t *testing.T) {
	storm := func(provider string, status, count int) cachedStorm {
		return cachedStorm{Provider: provider, Status: status, Count: count}
	}
	tests := []struct {
		name   string
		down   bool
		storms []cachedStorm
		color  bool
		want   string
	}{
		{name: "up", want: "◆ magpie"},
		{name: "up with color", color: true, want: "🟢 ◆ magpie"},
		{name: "down", down: true, want: "◇ magpie down"},
		{name: "down with color", down: true, color: true, want: "🔴 ◇ magpie down"},
		{
			name:   "storm",
			storms: []cachedStorm{storm("opencode-go", 429, 438)},
			want:   "◆ magpie ⚠ opencode-go 429×438",
		},
		{
			name:   "storm with color",
			storms: []cachedStorm{storm("opencode-go", 429, 438)},
			color:  true,
			want:   "🟡 ◆ magpie ⚠ opencode-go 429×438",
		},
		{
			name:   "worst storm wins and the rest are counted",
			storms: []cachedStorm{storm("a", 429, 5), storm("b", 500, 9), storm("c", 402, 1)},
			want:   "◆ magpie ⚠ b 500×9 +2",
		},
		{
			name:   "no response reads as err",
			storms: []cachedStorm{storm("x", 0, 5)},
			want:   "◆ magpie ⚠ x err×5",
		},
		{
			name:   "an outage hides storms",
			down:   true,
			storms: []cachedStorm{storm("a", 429, 5)},
			want:   "◇ magpie down",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusLine(tt.down, tt.storms, tt.color); got != tt.want {
				t.Errorf("statusLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNeedsRefresh(t *testing.T) {
	ago := func(d time.Duration) time.Time { return testNow.Add(-d) }
	tests := []struct {
		name        string
		refreshedAt time.Time
		lastSpawn   time.Time
		want        bool
	}{
		{name: "fresh refresh", refreshedAt: ago(10 * time.Second), want: false},
		{name: "exactly at the staleness limit is fresh", refreshedAt: ago(refreshStale), want: false},
		{name: "past the staleness limit", refreshedAt: ago(refreshStale + time.Second), want: true},
		{name: "never refreshed", want: true},
		{name: "stale but started within the interval", refreshedAt: ago(time.Hour), lastSpawn: ago(time.Minute), want: false},
		{name: "stale and the last start is a full interval old", refreshedAt: ago(time.Hour), lastSpawn: ago(refreshStale + time.Second), want: true},
		{name: "refresh from the future counts as stale", refreshedAt: testNow.Add(5 * time.Second), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsRefresh(testNow, tt.refreshedAt, tt.lastSpawn); got != tt.want {
				t.Errorf("needsRefresh() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestVisibleStorms(t *testing.T) {
	storms := []cachedStorm{{Provider: "p", Status: 429, Count: 6}}
	tests := []struct {
		name        string
		refreshedAt time.Time
		want        []cachedStorm
	}{
		{name: "recent cache shows storms", refreshedAt: testNow.Add(-9 * time.Minute), want: storms},
		{name: "old cache shows nothing", refreshedAt: testNow.Add(-11 * time.Minute), want: nil},
		{name: "no cache shows nothing", want: nil},
		{name: "cache from the future shows nothing", refreshedAt: testNow.Add(time.Minute), want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := visibleStorms(stormView{RefreshedAt: tt.refreshedAt, Storms: storms}, testNow)
			if !slices.Equal(got, tt.want) {
				t.Errorf("visibleStorms() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGatewayToast(t *testing.T) {
	tests := []struct {
		name                string
		tr                  transition
		shownURL, reason    string
		wantTitle, wantBody string
		wantSound           string
	}{
		{
			name:      "down with a reason",
			tr:        wentDown,
			shownURL:  "http://gw.example.test:3425",
			reason:    "connection refused",
			wantTitle: "magpie gateway down",
			wantBody:  "http://gw.example.test:3425 · connection refused",
			wantSound: "request",
		},
		{
			name:      "down without a reason",
			tr:        wentDown,
			shownURL:  "http://gw.example.test:3425",
			wantTitle: "magpie gateway down",
			wantBody:  "http://gw.example.test:3425",
			wantSound: "request",
		},
		{
			name:      "back",
			tr:        cameBack,
			shownURL:  "http://gw.example.test:3425",
			wantTitle: "magpie gateway back",
			wantBody:  "http://gw.example.test:3425",
			wantSound: "done",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, body, sound := gatewayToast(tt.tr, tt.shownURL, tt.reason)
			if title != tt.wantTitle || body != tt.wantBody || sound != tt.wantSound {
				t.Errorf("gatewayToast() = (%q, %q, %q), want (%q, %q, %q)",
					title, body, sound, tt.wantTitle, tt.wantBody, tt.wantSound)
			}
		})
	}
}

func TestResolveGateway(t *testing.T) {
	fresh := discovery{URL: "http://cached.test:3425", At: testNow.Add(-time.Minute)}
	expired := discovery{URL: "http://old.test:3425", At: testNow.Add(-time.Hour)}
	tests := []struct {
		name       string
		cached     discovery
		configURL  string
		found      string
		cancelled  bool // parent already done, so the discovery budget cuts the lookup short
		wantURL    string
		wantCalls  int
		wantStored bool
	}{
		{name: "fresh cache is reused", cached: fresh, found: "http://new.test:3425", wantURL: fresh.URL},
		{
			name: "expired cache is replaced", cached: expired, found: "http://new.test:3425",
			wantURL: "http://new.test:3425", wantCalls: 1, wantStored: true,
		},
		{
			name: "a changed config ignores the cache", cached: fresh, configURL: "http://other.test:3425",
			found: "http://other.test:3425", wantURL: "http://other.test:3425", wantCalls: 1, wantStored: true,
		},
		{
			name: "cut short keeps the cached answer", cached: expired, found: "http://127.0.0.1:3425",
			cancelled: true, wantURL: expired.URL, wantCalls: 1,
		},
		{
			name: "cut short without a cache uses the answer for now", found: "http://127.0.0.1:3425",
			cancelled: true, wantURL: "http://127.0.0.1:3425", wantCalls: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			d := statusDeps{
				cfg: config{GatewayURL: tt.configURL},
				now: testNow,
				discover: func(context.Context, string) string {
					calls++
					return tt.found
				},
			}
			ctx := t.Context()
			if tt.cancelled {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			st := statusState{Discovery: tt.cached}
			if got := d.resolveGateway(ctx, &st); got != tt.wantURL {
				t.Errorf("resolveGateway() = %q, want %q", got, tt.wantURL)
			}
			if calls != tt.wantCalls {
				t.Errorf("discover ran %d times, want %d", calls, tt.wantCalls)
			}
			stored := st.Discovery.URL == tt.found
			if stored != tt.wantStored {
				t.Errorf("discovery stored = %t (%+v), want %t", stored, st.Discovery, tt.wantStored)
			}
		})
	}
}

// TestStatusGatewayFlow runs status several times against a gateway that goes
// down and comes back, and checks the printed line and the herdr notifications.
func TestStatusGatewayFlow(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTools{}
	client := &herdr.Client{Bin: "herdr", Runner: fake}
	probes := []magpie.Health{
		{Up: true, Status: 401},
		{Up: false, Err: "connection refused"},
		{Up: false, Err: "connection refused"},
		{Up: true, Status: 401},
	}
	var errs bytes.Buffer
	var lines []string
	for i := range probes {
		d := statusDeps{
			cfg:      defaultConfig(),
			store:    store,
			now:      testNow.Add(time.Duration(i) * 20 * time.Second),
			herdr:    client,
			stderr:   &errs,
			probe:    func(context.Context, string) magpie.Health { return probes[i] },
			discover: func(context.Context, string) string { return "http://gw.test:3425" },
			spawn:    func(context.Context) error { return nil },
		}
		lines = append(lines, status(t.Context(), d))
	}

	wantLines := []string{"◆ magpie", "◆ magpie", "◇ magpie down", "◆ magpie"}
	if !slices.Equal(lines, wantLines) {
		t.Errorf("status lines = %q, want %q", lines, wantLines)
	}
	wantWrites := []string{
		`herdr notification show "magpie gateway down" --body "http://gw.test:3425 · connection refused" --sound request`,
		`herdr notification show "magpie gateway back" --body http://gw.test:3425 --sound done`,
	}
	if got := fake.writes(); !slices.Equal(got, wantWrites) {
		t.Errorf("herdr writes = %q, want %q", got, wantWrites)
	}
	if errs.Len() != 0 {
		t.Errorf("unexpected stderr: %s", errs.String())
	}
}

func TestOpenRefreshLogRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, refreshLog)
	if err := os.WriteFile(path, make([]byte, logLimit+1), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openRefreshLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Size() != 0 {
		t.Errorf("new log: stat = %v, %v; want an empty file", fi, err)
	}
	if fi, err := os.Stat(path + ".1"); err != nil || fi.Size() != logLimit+1 {
		t.Errorf("rotated log: stat = %v, %v; want the old %d bytes", fi, err, logLimit+1)
	}
}

// stormFailures returns n failed gateway rows for one provider, all at one time.
func stormFailures(at time.Time, n int, provider string, status int) []magpie.Request {
	var rows []magpie.Request
	for range n {
		rows = append(rows, magpie.Request{Time: at, Provider: provider, Status: status, Error: true, ErrorType: "RateLimit", Agent: "alpha"})
	}
	return rows
}

// logReader serves rows the way magpie.TailUsage does: rows at or after since only.
func logReader(rows []magpie.Request) func(string, time.Time) ([]magpie.Request, error) {
	return func(_ string, since time.Time) ([]magpie.Request, error) {
		var out []magpie.Request
		for _, r := range rows {
			if !r.Time.Before(since) {
				out = append(out, r)
			}
		}
		return out, nil
	}
}

// stormDeps are the status deps for one tick at now: a healthy gateway, a usage
// log at a placeholder path, and the given reader.
func stormDeps(store *state.Store, client *herdr.Client, errs *bytes.Buffer, now time.Time, read func(string, time.Time) ([]magpie.Request, error)) statusDeps {
	return statusDeps{
		cfg:       defaultConfig(),
		store:     store,
		now:       now,
		herdr:     client,
		stderr:    errs,
		probe:     func(context.Context, string) magpie.Health { return magpie.Health{Up: true, Status: 401} },
		discover:  func(context.Context, string) string { return "http://gw.test:3425" },
		spawn:     func(context.Context) error { return nil },
		usagePath: "/unused/usage.jsonl",
		readUsage: read,
	}
}

// TestStormAlertsFollowTheLog runs status over several ticks. A storm toasts when
// it starts, stays quiet while it lasts, and toasts again only after its
// cooldown, which must have run out while the storm was away.
func TestStormAlertsFollowTheLog(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTools{}
	client := &herdr.Client{Bin: "herdr", Runner: fake}
	steps := []struct {
		at         time.Duration // offset of the tick from testNow
		failures   int           // 429 rows logged one minute before the tick
		wantLine   string
		wantToasts int // storm toasts sent so far
	}{
		{at: 0, failures: 5, wantLine: "◆ magpie ⚠ p1 429×5", wantToasts: 1},
		{at: 10 * time.Second, failures: 5, wantLine: "◆ magpie ⚠ p1 429×5", wantToasts: 1},
		{at: 2 * time.Minute, wantLine: "◆ magpie", wantToasts: 1},
		{at: 5 * time.Minute, failures: 5, wantLine: "◆ magpie ⚠ p1 429×5", wantToasts: 1}, // back inside the cooldown
		{at: 6 * time.Minute, wantLine: "◆ magpie", wantToasts: 1},
		{at: 45 * time.Minute, failures: 5, wantLine: "◆ magpie ⚠ p1 429×5", wantToasts: 2}, // cooldown is over
	}
	var errs bytes.Buffer
	for i, s := range steps {
		now := testNow.Add(s.at)
		rows := stormFailures(now.Add(-time.Minute), s.failures, "p1", 429)
		line := status(t.Context(), stormDeps(store, client, &errs, now, logReader(rows)))
		if line != s.wantLine {
			t.Errorf("step %d: line = %q, want %q", i, line, s.wantLine)
		}
		if got := len(fake.writes()); got != s.wantToasts {
			t.Errorf("step %d: %d herdr writes, want %d: %q", i, got, s.wantToasts, fake.writes())
		}
	}
	const wantFirst = `herdr notification show "magpie: p1 429 ×5 in 10m" --body "RateLimit · alpha" --sound request`
	if writes := fake.writes(); len(writes) > 0 && writes[0] != wantFirst {
		t.Errorf("first toast = %q, want %q", writes[0], wantFirst)
	}
	if errs.Len() != 0 {
		t.Errorf("unexpected stderr: %s", errs.String())
	}
}

// TestStormAlertsOffWithoutLog checks that a missing usage log, or no usage log
// path at all, turns storm alerts off quietly: no storm in the line, no toast,
// and no error output.
func TestStormAlertsOffWithoutLog(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTools{}
	client := &herdr.Client{Bin: "herdr", Runner: fake}
	var errs bytes.Buffer
	missing := func(string, time.Time) ([]magpie.Request, error) { return nil, magpie.ErrNoUsageLog }
	if line := status(t.Context(), stormDeps(store, client, &errs, testNow, missing)); line != "◆ magpie" {
		t.Errorf("missing log: line = %q, want %q", line, "◆ magpie")
	}
	off := stormDeps(store, client, &errs, testNow, logReader(stormFailures(testNow.Add(-time.Minute), 5, "p1", 429)))
	off.usagePath = ""
	if line := status(t.Context(), off); line != "◆ magpie" {
		t.Errorf("no path: line = %q, want %q", line, "◆ magpie")
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Errorf("herdr writes = %q, want none", writes)
	}
	if errs.Len() != 0 {
		t.Errorf("unexpected stderr: %s", errs.String())
	}
}

// TestStormReadBudget checks the soft budget. A usage read that runs over keeps
// the last storm reading while that reading is recent enough to show, and drops
// it once it is older than stormVisible.
func TestStormReadBudget(t *testing.T) {
	store, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeTools{}
	client := &herdr.Client{Bin: "herdr", Runner: fake}
	release := make(chan struct{})
	defer close(release)
	hang := func(string, time.Time) ([]magpie.Request, error) {
		<-release
		return nil, nil
	}
	var errs bytes.Buffer
	read := stormDeps(store, client, &errs, testNow, logReader(stormFailures(testNow.Add(-time.Minute), 5, "p1", 429)))
	if line := status(t.Context(), read); line != "◆ magpie ⚠ p1 429×5" {
		t.Fatalf("first tick: line = %q", line)
	}
	slow := stormDeps(store, client, &errs, testNow.Add(time.Minute), hang)
	slow.usageBudget = 10 * time.Millisecond
	if line := status(t.Context(), slow); line != "◆ magpie ⚠ p1 429×5" {
		t.Errorf("slow read: line = %q, want the last reading", line)
	}
	late := stormDeps(store, client, &errs, testNow.Add(11*time.Minute), hang)
	late.usageBudget = 10 * time.Millisecond
	if line := status(t.Context(), late); line != "◆ magpie" {
		t.Errorf("slow read after the reading expired: line = %q, want %q", line, "◆ magpie")
	}
	if errs.Len() != 0 {
		t.Errorf("unexpected stderr: %s", errs.String())
	}
}
