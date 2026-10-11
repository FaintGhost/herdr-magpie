package panel

import (
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

func TestFailed(t *testing.T) {
	tests := []struct {
		name string
		r    magpie.Request
		want bool
	}{
		{name: "success", r: magpie.Request{Status: 200}, want: false},
		{name: "429 counts", r: magpie.Request{Status: 429}, want: true},
		{name: "500 counts", r: magpie.Request{Status: 500}, want: true},
		{name: "error flag with no status counts", r: magpie.Request{Error: true}, want: true},
		{name: "400 is the caller's bug", r: magpie.Request{Status: 400, Error: true}, want: false},
		{name: "499 is a cancel", r: magpie.Request{Status: 499, Error: true}, want: false},
		{name: "log rows are agents' own calls", r: magpie.Request{Status: 500, Source: "log"}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := failed(tc.r); got != tc.want {
				t.Errorf("failed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStormThreshold(t *testing.T) {
	if got := stormThreshold(402); got != 1 {
		t.Errorf("402 threshold = %d, want 1", got)
	}
	if got := stormThreshold(429); got != 5 {
		t.Errorf("429 threshold = %d, want 5", got)
	}
}

func TestComputeStorms(t *testing.T) {
	now := testNow
	at := func(d time.Duration, r magpie.Request) magpie.Request {
		r.Time = now.Add(-d)
		return r
	}
	gw := func(provider string, status int) magpie.Request {
		return magpie.Request{Provider: provider, Status: status, Error: true}
	}
	tests := []struct {
		name string
		reqs []magpie.Request
		want []storm // Provider, Status, Count, Active
	}{
		{
			name: "four failures are below the threshold",
			reqs: []magpie.Request{at(time.Minute, gw("p", 429)), at(time.Minute, gw("p", 429)),
				at(time.Minute, gw("p", 429)), at(time.Minute, gw("p", 429))},
			want: []storm{{Provider: "p", Status: 429, Count: 4, Active: false}},
		},
		{
			name: "five failures make a storm",
			reqs: []magpie.Request{at(time.Minute, gw("p", 429)), at(time.Minute, gw("p", 429)),
				at(time.Minute, gw("p", 429)), at(time.Minute, gw("p", 429)), at(time.Minute, gw("p", 429))},
			want: []storm{{Provider: "p", Status: 429, Count: 5, Active: true}},
		},
		{
			name: "one 402 is a storm on its own",
			reqs: []magpie.Request{at(time.Minute, gw("deepseek", 402))},
			want: []storm{{Provider: "deepseek", Status: 402, Count: 1, Active: true}},
		},
		{
			name: "rows older than the window are ignored",
			reqs: []magpie.Request{at(11*time.Minute, gw("p", 500)), at(time.Minute, gw("q", 500))},
			want: []storm{{Provider: "q", Status: 500, Count: 1, Active: false}},
		},
		{
			name: "groups order by count, then provider",
			reqs: []magpie.Request{at(time.Minute, gw("b", 500)), at(time.Minute, gw("a", 500)),
				at(time.Minute, gw("c", 500)), at(time.Minute, gw("c", 500))},
			want: []storm{
				{Provider: "c", Status: 500, Count: 2},
				{Provider: "a", Status: 500, Count: 1},
				{Provider: "b", Status: 500, Count: 1},
			},
		},
		{
			name: "provider falls back to host, then unknown",
			reqs: []magpie.Request{
				at(time.Minute, magpie.Request{Host: "h.example", Status: 503}),
				at(time.Minute, magpie.Request{Status: 503}),
			},
			want: []storm{
				{Provider: "h.example", Status: 503, Count: 1},
				{Provider: "unknown", Status: 503, Count: 1},
			},
		},
		{
			name: "400 and 499 are not storms",
			reqs: []magpie.Request{at(time.Minute, magpie.Request{Provider: "p", Status: 400, Error: true}),
				at(time.Minute, magpie.Request{Provider: "p", Status: 499, Error: true})},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeStorms(tc.reqs, now)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d storms %+v, want %d", len(got), got, len(tc.want))
			}
			for i, w := range tc.want {
				g := got[i]
				if g.Provider != w.Provider || g.Status != w.Status || g.Count != w.Count || g.Active != w.Active {
					t.Errorf("storm %d = %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

func TestComputeStormsTopAgentsAndType(t *testing.T) {
	now := testNow
	var reqs []magpie.Request
	for range 2 {
		reqs = append(reqs, magpie.Request{Time: now, Provider: "p", Status: 429, Error: true, Agent: "omp", ErrorType: "rate_limit"})
	}
	reqs = append(reqs,
		magpie.Request{Time: now, Provider: "p", Status: 429, Error: true, Agent: "hermes", ErrorType: "GoUsageLimitError"},
		magpie.Request{Time: now, Provider: "p", Status: 429, Error: true, Agent: "kimi", ErrorType: "GoUsageLimitError"},
		magpie.Request{Time: now, Provider: "p", Status: 429, Error: true, Agent: "codex", ErrorType: "GoUsageLimitError"},
		magpie.Request{Time: now, Provider: "p", Status: 429, Error: true, Agent: "pi"},
	)
	got := computeStorms(reqs, now)
	if len(got) != 1 {
		t.Fatalf("got %d storms, want 1", len(got))
	}
	s := got[0]
	if s.ErrorType != "GoUsageLimitError" {
		t.Errorf("ErrorType = %q, want the most frequent one", s.ErrorType)
	}
	if want := []string{"omp", "codex", "hermes"}; !equalStrings(s.Agents, want) {
		t.Errorf("Agents = %v, want %v (busiest first, at most %d)", s.Agents, want, stormTopAgents)
	}
}

func TestLabelCleansAndCuts(t *testing.T) {
	if got := label("  a\n\tb  "); got != "a b" {
		t.Errorf("label = %q, want %q", got, "a b")
	}
	long := make([]rune, 60)
	for i := range long {
		long[i] = 'x'
	}
	got := label(string(long))
	if n := len([]rune(got)); n != labelCells {
		t.Errorf("label has %d runes, want %d", n, labelCells)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
