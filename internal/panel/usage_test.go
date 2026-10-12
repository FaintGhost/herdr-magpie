package panel

import (
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

func TestUsageSince(t *testing.T) {
	midnight := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{name: "afternoon: today starts at midnight", now: testNow, want: midnight},
		{name: "just after midnight: the hour reaches back further", now: midnight.Add(30 * time.Minute), want: midnight.Add(-30 * time.Minute)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := usageSince(tc.now); !got.Equal(tc.want) {
				t.Errorf("usageSince(%v) = %v, want %v", tc.now, got, tc.want)
			}
		})
	}
}

func TestCountSince(t *testing.T) {
	reqs := sampleRequests(testNow)
	tests := []struct {
		name  string
		since time.Time
		want  windowCount
	}{
		{name: "ten minutes: five 429s, a 200 and a 400 (an error, not a provider error)", since: testNow.Add(-stormWindow), want: windowCount{Requests: 7, Errors: 6, ProviderErrors: 5}},
		{name: "an hour adds the 402", since: testNow.Add(-time.Hour), want: windowCount{Requests: 8, Errors: 7, ProviderErrors: 6}},
		{name: "today adds the 90-minute-old 200 and leaves out the 30-hour-old 500", since: todayStart(testNow), want: windowCount{Requests: 9, Errors: 7, ProviderErrors: 6}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := countSince(reqs, tc.since); got != tc.want {
				t.Errorf("countSince = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTopRows(t *testing.T) {
	rows := []magpie.UsageRow{
		{Name: "a", Calls: 5, Cost: 1},
		{Name: "b", Calls: 1, Cost: 2},
		{Name: "c", Calls: 9, Cost: 1},
	}
	got := topRows(rows, 2)
	if len(got) != 2 || got[0].Name != "b" || got[1].Name != "c" {
		t.Errorf("topRows = %+v, want b then c (cost first, then calls)", got)
	}
	if n := len(rows); n != 3 {
		t.Errorf("topRows changed its input length to %d", n)
	}
	if got := topRows(rows, 10); len(got) != 3 {
		t.Errorf("topRows with n above the length returned %d rows", len(got))
	}
}

func TestStormTable(t *testing.T) {
	st := newStyles(newRenderer(io.Discard, false))
	head, rows := stormTable(nil, 80, st)
	if len(head) != 1 || !strings.Contains(head[0], "none") || rows != nil {
		t.Errorf("empty table = %q, %q", head, rows)
	}

	storms := computeStorms(sampleRequests(testNow), testNow)
	head, rows = stormTable(storms, 80, st)
	if len(head) != 1 || !strings.Contains(head[0], "PROVIDER") || !strings.Contains(head[0], "STATE") {
		t.Errorf("table header = %q", head)
	}
	if len(rows) != len(storms) {
		t.Fatalf("got %d rows for %d storms", len(rows), len(storms))
	}
	first := rows[0]
	for _, want := range []string{"opencode-go", "429", "5", "GoUsageLimitError", "omp", "ACTIVE"} {
		if !strings.Contains(first, want) {
			t.Errorf("first row %q lacks %q", first, want)
		}
	}
}

func TestCostBlock(t *testing.T) {
	src := sampleSource()
	t.Run("never read", func(t *testing.T) {
		m := testModel(t, Deps{Source: src})
		if got := strings.Join(m.costBlock(80, 40), "\n"); !strings.Contains(got, "no cost data yet") {
			t.Errorf("cost block = %q", got)
		}
	})
	t.Run("no gateway key", func(t *testing.T) {
		m := testModel(t, Deps{Source: src})
		m.snap.APIUsage = stampAt[*magpie.UsageSummary](testNow, nil)
		if got := strings.Join(m.costBlock(80, 40), "\n"); !strings.Contains(got, "no gateway key — cost hidden") {
			t.Errorf("cost block = %q", got)
		}
	})
	t.Run("with key", func(t *testing.T) {
		m := testModel(t, Deps{Source: src})
		m.snap.APIUsage = stampAt(testNow, src.api)
		got := strings.Join(m.costBlock(80, 40), "\n")
		for _, want := range []string{"Gateway cost · today", "412 calls · $3.12", "AGENT", "MODEL", "omp", "claude-sonnet"} {
			if !strings.Contains(got, want) {
				t.Errorf("cost block lacks %q:\n%s", want, got)
			}
		}
	})
	t.Run("failed after a success keeps the old total", func(t *testing.T) {
		m := testModel(t, Deps{Source: src})
		m.snap.APIUsage = stampAt(testNow, src.api)
		m.feeds[feedAPIUsage] = feedState{err: "gateway down", fromCache: true}
		got := strings.Join(m.costBlock(80, 40), "\n")
		if !strings.Contains(got, "api usage: gateway down") || !strings.Contains(got, "412 calls") {
			t.Errorf("cost block = %q", got)
		}
	})
}

func TestUsageLinesFitTheHeight(t *testing.T) {
	var reqs []magpie.Request
	for i := range 8 {
		reqs = append(reqs, magpie.Request{Time: testNow, Provider: string(rune('a' + i)), Status: 402, Error: true})
	}
	m := testModel(t, Deps{Source: sampleSource()})
	m.tab = tabUsage
	m.snap.Requests = stampAt(testNow, reqs)
	m.snap.APIUsage = stampAt(testNow, sampleSource().api)

	for _, h := range []int{20, 28} {
		lines := m.usageLines(80, h)
		if len(lines) > h {
			t.Errorf("h=%d: usageLines returned %d lines", h, len(lines))
		}
		if h == 20 && !strings.Contains(strings.Join(lines, "\n"), "more") {
			t.Errorf("h=20: the cut error rows should say how many are left out:\n%s", strings.Join(lines, "\n"))
		}
	}
}

func TestTTFTP50IsTheMedianOfTheWindow(t *testing.T) {
	reqs := []magpie.Request{
		{Time: testNow, TTFT: 100 * time.Millisecond},
		{Time: testNow, TTFT: 300 * time.Millisecond},
		{Time: testNow, TTFT: 900 * time.Millisecond},
		{Time: testNow.Add(-2 * time.Hour), TTFT: 5 * time.Second}, // outside the window
		{Time: testNow}, // no time to first token
	}
	d, ok := ttftP50(reqs, testNow.Add(-time.Hour))
	if !ok || d != 300*time.Millisecond {
		t.Errorf("median = %v, %v; want 300ms", d, ok)
	}
	if _, ok := ttftP50(nil, testNow.Add(-time.Hour)); ok {
		t.Error("no requests should have no median")
	}
}

func TestCountsLinesShowAllErrorsAndTheProviderErrors(t *testing.T) {
	st := newStyles(newRenderer(io.Discard, false))
	lines := countsLines(sampleRequests(testNow), testNow, 60, st)
	if got := strings.Fields(lines[2]); !slices.Equal(got, []string{"errors", "6", "7", "7"}) {
		t.Errorf("errors row = %q, want all failures in 10m, 1h and today", got)
	}
	if got := strings.Fields(lines[3]); !slices.Equal(got, []string{"provider", "errors", "5", "6", "6"}) {
		t.Errorf("provider errors row = %q, want the storm rule's count", got)
	}
}

func TestCostBlockStacksTheModelTableBelowNinetyColumns(t *testing.T) {
	src := sampleSource()
	m := testModel(t, Deps{Source: src})
	m.snap.APIUsage = stampAt(testNow, src.api)

	narrow := strings.Split(strings.Join(m.costBlock(80, 40), "\n"), "\n")
	if agent, model := lineWith(narrow, "AGENT"), lineWith(narrow, "MODEL"); agent < 0 || model <= agent {
		t.Errorf("at 80 columns the model table should follow the agent table: agent line %d, model line %d", agent, model)
	}
	wide := strings.Split(strings.Join(m.costBlock(120, 40), "\n"), "\n")
	if agent, model := lineWith(wide, "AGENT"), lineWith(wide, "MODEL"); agent < 0 || agent != model {
		t.Errorf("at 120 columns the tables should share a line: agent line %d, model line %d", agent, model)
	}
}

func TestCostBlockHeadingsStayWholeAtFiftyEightColumns(t *testing.T) {
	src := sampleSource()
	m := testModel(t, Deps{Source: src})
	m.snap.APIUsage = stampAt(testNow, src.api)
	got := strings.Join(m.costBlock(58, 40), "\n")
	for _, want := range []string{"AGENT", "MODEL", "ERRORS", "CALLS", "claude-sonnet", "deepseek-chat"} {
		if !strings.Contains(got, want) {
			t.Errorf("58-column cost block lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "A…") || strings.Contains(got, "M…") {
		t.Errorf("58-column headings are cut:\n%s", got)
	}
}

func TestCostBlockSaysLoadingWhileItReads(t *testing.T) {
	m := testModel(t, Deps{Source: sampleSource()})
	m.feeds[feedAPIUsage].pending = true
	if got := strings.Join(m.costBlock(80, 40), "\n"); !strings.Contains(got, "loading…") {
		t.Errorf("cost block while loading = %q", got)
	}
}

// lineWith is the index of the first line that contains s, or -1.
func lineWith(lines []string, s string) int {
	for i, l := range lines {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

func TestUsageShowsTheTTFTRow(t *testing.T) {
	m := panelModel(t, samplePanelSource(), nil, nil, 120, 32)
	press(m, "3")
	view := m.View()
	for _, want := range []string{"ttft p50", "800ms", "1.0s"} {
		if !strings.Contains(view, want) {
			t.Errorf("Usage tab lacks %q:\n%s", want, view)
		}
	}
}
