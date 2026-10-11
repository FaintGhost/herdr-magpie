package panel

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

func TestRecentGatewayIsNewestFirstAndSkipsLogRows(t *testing.T) {
	reqs := append(manyRequests(testNow, 60), magpie.Request{Time: testNow.Add(time.Hour), Source: "log"})
	got := recentGateway(reqs, routingRows)
	if len(got) != routingRows {
		t.Fatalf("got %d rows, want %d", len(got), routingRows)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Time.After(got[i-1].Time) {
			t.Fatalf("row %d is newer than row %d", i, i-1)
		}
	}
	for _, r := range got {
		if r.Source == "log" {
			t.Fatal("a log row was listed")
		}
	}
}

func TestSwappedAndErrorRules(t *testing.T) {
	tests := []struct {
		name    string
		r       magpie.Request
		swapped bool
		isErr   bool
	}{
		{"served the same model", magpie.Request{RequestedModel: "a", ServedModel: "a", Status: 200}, false, false},
		{"served another model", magpie.Request{RequestedModel: "a", ServedModel: "b", Status: 200}, true, false},
		{"usage log flag alone", magpie.Request{Swapped: true, Status: 200}, true, false},
		{"no served model is not a swap", magpie.Request{RequestedModel: "a"}, false, false},
		{"rate limited", magpie.Request{Status: 429, Error: true}, false, true},
		{"caller error counts as an error here", magpie.Request{Status: 400}, false, true},
		{"client cancel is an error here", magpie.Request{Status: 499}, false, true},
		{"ok", magpie.Request{Status: 200}, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := swapped(tc.r); got != tc.swapped {
				t.Errorf("swapped = %v, want %v", got, tc.swapped)
			}
			if got := isError(tc.r); got != tc.isErr {
				t.Errorf("isError = %v, want %v", got, tc.isErr)
			}
		})
	}
}

func TestRoutingSummaryCountsTheLastHour(t *testing.T) {
	reqs := sampleTTFTRequests(testNow)
	want := "last 1h · 1 swapped · provider errors 33.3% (1 of 3)"
	if got := routingSummary(reqs, testNow); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if got := routingSummary(nil, testNow); got != "last 1h · no gateway requests" {
		t.Errorf("empty summary = %q", got)
	}
}

func TestFormatTTFT(t *testing.T) {
	tests := map[time.Duration]string{
		0:                       "—",
		842 * time.Millisecond:  "842ms",
		1234 * time.Millisecond: "1.2s",
		12 * time.Second:        "12.0s",
	}
	for d, want := range tests {
		if got := formatTTFT(d); got != want {
			t.Errorf("formatTTFT(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestRoutingTabShowsRoutesStatusesAndTTFT(t *testing.T) {
	m := panelModel(t, samplePanelSource(), nil, nil, 120, 32)
	press(m, "4")
	view := m.View()
	for _, want := range []string{
		"last 1h · 1 swapped", "gpt-x → gpt-y", "429", "800ms", "1.2s", "requested → served", "openai",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("Routing tab lacks %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "k1 → k2") {
		t.Errorf("the table lists the recent gateway requests, older than the hour too:\n%s", view)
	}
}

func TestRoutingColoursSwapsAndErrors(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	m := newModel(t.Context(), Deps{Source: samplePanelSource(), Now: func() time.Time { return testNow }}, r)
	drain(m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	press(m, "4")
	out := m.View()
	if !strings.Contains(out, "\x1b[33m") {
		t.Errorf("the swapped route should be drawn in the warning colour")
	}
	if !strings.Contains(out, "\x1b[31m") {
		t.Errorf("the failed status should be drawn in red")
	}
}

func TestRoutingTabStates(t *testing.T) {
	t.Run("loading before the first read", func(t *testing.T) {
		m := testModel(t, Deps{Source: samplePanelSource()})
		m.feeds[feedRequests].pending = true
		if got := strings.Join(m.routingLines(80, 10), "\n"); !strings.Contains(got, "loading…") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("no requests today", func(t *testing.T) {
		m := testModel(t, Deps{Source: samplePanelSource()})
		m.snap.Requests = stampAt[[]magpie.Request](testNow, nil)
		got := strings.Join(m.routingLines(80, 10), "\n")
		if !strings.Contains(got, "no gateway requests today") || !strings.Contains(got, "last 1h · no gateway requests") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("many requests say how many are left out", func(t *testing.T) {
		m := testModel(t, Deps{Source: samplePanelSource()})
		m.snap.Requests = stampAt(testNow, manyRequests(testNow, 60))
		lines := m.routingLines(80, 10)
		if len(lines) != 10 {
			t.Fatalf("%d lines for 10 rows", len(lines))
		}
		if last := lines[len(lines)-1]; !strings.Contains(last, "older") {
			t.Errorf("last line = %q, want the count of older rows", last)
		}
	})
}

func TestRoutingFitsEverySize(t *testing.T) {
	src := samplePanelSource()
	src.requests = append(src.requests, magpie.Request{
		Time: testNow, Agent: "中文代理很长", Provider: "供应商名称很长很长", RequestedModel: "模型甲",
		ServedModel: "模型乙", Swapped: true, Status: 402, Error: true, TTFT: time.Second,
	})
	for _, sz := range screenSizes {
		m := panelModel(t, src, nil, nil, sz[0], sz[1])
		press(m, "4")
		lines := strings.Split(m.View(), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines, want %d", sz[0], sz[1], len(lines), sz[1])
		}
		assertLines(t, lines, sz[0])
	}
}

func TestRouteColumnsDropTTFTThenProvider(t *testing.T) {
	rows := []magpie.Request{{Agent: "Go-http-client", Provider: "deepseek", RequestedModel: "deepseek-chat", ServedModel: "deepseek-chat"}}
	for _, tc := range []struct {
		w                     int
		agent, provider, ttft bool
	}{
		{120, true, true, true},
		{80, true, true, true},
		{72, true, true, false},
		{58, true, false, false},
		{40, false, false, false},
	} {
		cols := routeColumns(tc.w, rows)
		got := [3]bool{cols.agent > 0, cols.provider > 0, cols.ttft > 0}
		if want := [3]bool{tc.agent, tc.provider, tc.ttft}; got != want {
			t.Errorf("w=%d: agent, provider and ttft shown = %v, want %v (%+v)", tc.w, got, want, cols)
		}
	}
}

func TestRouteKeepsTheAgentNameWhole(t *testing.T) {
	r := magpie.Request{Agent: "Go-http-client", Provider: "deepseek", RequestedModel: "deepseek-chat", ServedModel: "deepseek-chat", Status: 200}
	st := newStyles(newRenderer(io.Discard, false))
	for _, w := range []int{58, 80, 120} {
		if line := routeRow(r, w, routeColumns(w, []magpie.Request{r}), st); !strings.Contains(line, "Go-http-client") {
			t.Errorf("w=%d: the agent name is cut: %q", w, line)
		}
	}
}
