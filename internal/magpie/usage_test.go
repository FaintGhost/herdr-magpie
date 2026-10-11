package magpie

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestUsageArgs(t *testing.T) {
	tests := []struct {
		name    string
		window  string
		want    string // expected runner call; empty when no call is expected
		wantErr bool
	}{
		{name: "empty window means today", window: "", want: "magpie usage --csv today"},
		{name: "explicit window is passed through", window: "7d", want: "magpie usage --csv 7d"},
		{name: "flag-like window is refused before running anything", window: "--help", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{out: map[string]string{
				"magpie usage --csv today": "time,agent\n",
				"magpie usage --csv 7d":    "time,agent\n",
			}}
			_, _, err := Usage(t.Context(), r, "", tt.window)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Usage() error = nil, want error")
				}
				if len(r.calls) != 0 {
					t.Errorf("runner was called with %q for a refused window", r.calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Usage() error = %v", err)
			}
			if !slices.Equal(r.calls, []string{tt.want}) {
				t.Errorf("runner calls = %q, want %q", r.calls, tt.want)
			}
		})
	}
}

func TestUsageRunnerError(t *testing.T) {
	errRun := errors.New("runner failed")
	r := &fakeRunner{errs: map[string]error{"magpie usage --csv today": errRun}}
	_, _, err := Usage(t.Context(), r, "", "today")
	if !errors.Is(err, errRun) {
		t.Errorf("Usage() error = %v, want it to wrap %v", err, errRun)
	}
}

func TestUsageParsesFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/usage.csv")
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{out: map[string]string{"magpie usage --csv today": string(data)}}
	reqs, stats, err := Usage(t.Context(), r, "", "today")
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}
	if stats != (UsageStats{Rows: 4, Skipped: 4}) {
		t.Errorf("stats = %+v, want Rows 4 and Skipped 4", stats)
	}
	if len(reqs) != 4 {
		t.Fatalf("got %d requests, want 4", len(reqs))
	}
}

func TestParseUsageCSVFixture(t *testing.T) {
	f, err := os.Open("testdata/usage.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reqs, stats, err := ParseUsageCSV(f)
	if err != nil {
		t.Fatalf("ParseUsageCSV() error = %v", err)
	}
	if stats != (UsageStats{Rows: 4, Skipped: 4}) {
		t.Fatalf("stats = %+v, want Rows 4 and Skipped 4 (req-0004 short, req-0005 bad time, req-0006 bad bool, req-0007 bare quote)", stats)
	}

	first := reqs[0]
	if first.RequestID != "req-0001" || first.Agent != "claude" || first.Status != 200 ||
		first.CostUSD != 0.012345 || first.Swapped || first.Error || first.Source != "log" ||
		!first.Time.Equal(time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("first request = %+v", first)
	}

	second := reqs[1]
	if !second.Swapped || second.RequestedModel != "example-model-a" || second.ServedModel != "example-model-b" ||
		second.ProviderAccount != "acct-example-1" {
		t.Errorf("swapped request = %+v", second)
	}
	if want := time.Date(2026, 10, 11, 1, 5, 0, 0, time.UTC); !second.Time.Equal(want) {
		t.Errorf("Time with +08:00 offset = %v, want %v", second.Time, want)
	}

	third := reqs[2]
	if !third.Error || third.Status != 400 || third.CostUSD != 0 || third.ErrorType != "example_error_type" ||
		third.ErrorMessage != `upstream said: "no, really"` {
		t.Errorf("error request = %+v", third)
	}

	if reqs[3].RequestID != "req-0008" {
		t.Errorf("fourth request = %q, want req-0008", reqs[3].RequestID)
	}
}

func TestParseUsageCSVHeaderIndexing(t *testing.T) {
	// Columns are in a different order from the real output, one column is
	// unknown, and most columns are absent. Missing columns must leave fields zero.
	in := "status,agent,time,unknown_column,model\n" +
		"200,claude,2026-10-11T09:00:00Z,whatever,example-model-a\n"
	reqs, stats, err := ParseUsageCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseUsageCSV() error = %v", err)
	}
	if stats != (UsageStats{Rows: 1}) {
		t.Fatalf("stats = %+v, want one row and nothing skipped", stats)
	}
	got := reqs[0]
	if got.Status != 200 || got.Agent != "claude" || got.Model != "example-model-a" ||
		got.Time.IsZero() {
		t.Errorf("request = %+v", got)
	}
	if got.RequestID != "" || got.Session != "" || got.ErrorType != "" || got.Swapped || got.CostUSD != 0 {
		t.Errorf("missing columns were not left zero: %+v", got)
	}
}

func TestParseUsageCSVEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"no output at all", ""},
		{"header only", "time,agent\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs, stats, err := ParseUsageCSV(strings.NewReader(tt.in))
			if err != nil {
				t.Fatalf("ParseUsageCSV() error = %v", err)
			}
			if len(reqs) != 0 || stats != (UsageStats{}) {
				t.Errorf("got %d requests and stats %+v, want none", len(reqs), stats)
			}
		})
	}
}

// TestParseUsageCSVHost checks the host column, which storm grouping falls back to.
func TestParseUsageCSVHost(t *testing.T) {
	in := "time,provider,host,status\n2026-10-11T09:00:00Z,,api.example.test,503\n"
	reqs, _, err := ParseUsageCSV(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseUsageCSV() error = %v", err)
	}
	if len(reqs) != 1 || reqs[0].Provider != "" || reqs[0].Host != "api.example.test" {
		t.Errorf("request = %+v, want an empty provider and host api.example.test", reqs)
	}
}

// TestParseUsageCSVRowErrors checks that each kind of bad row is skipped and
// counted, and that the good row after it is still decoded.
func TestParseUsageCSVRowErrors(t *testing.T) {
	const header = "time,agent,status,swapped,cost_usd\n"
	const good = "2026-10-11T09:01:00Z,omp,200,true,0.2\n"
	tests := []struct {
		name        string
		bad         string
		wantSkipped int
	}{
		{name: "too few fields", bad: "2026-10-11T09:00:00Z,claude,200,false\n", wantSkipped: 1},
		{name: "too many fields", bad: "2026-10-11T09:00:00Z,claude,200,false,0.1,extra\n", wantSkipped: 1},
		{name: "bad time", bad: "not-a-time,claude,200,false,0.1\n", wantSkipped: 1},
		{name: "bad bool", bad: "2026-10-11T09:00:00Z,claude,200,maybe,0.1\n", wantSkipped: 1},
		{name: "bad status", bad: "2026-10-11T09:00:00Z,claude,ok,false,0.1\n", wantSkipped: 1},
		{name: "bad cost", bad: "2026-10-11T09:00:00Z,claude,200,false,free\n", wantSkipped: 1},
		{name: "bare quote is a CSV syntax error", bad: "2026-10-11T09:00:00Z,cl\"aude,200,false,0.1\n", wantSkipped: 1},
		{name: "blank line is not a bad row", bad: "\n", wantSkipped: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs, stats, err := ParseUsageCSV(strings.NewReader(header + tt.bad + good))
			if err != nil {
				t.Fatalf("ParseUsageCSV() error = %v", err)
			}
			if stats.Skipped != tt.wantSkipped || stats.Rows != 1 {
				t.Fatalf("stats = %+v, want Rows 1 and Skipped %d", stats, tt.wantSkipped)
			}
			if len(reqs) != 1 || reqs[0].Agent != "omp" || !reqs[0].Swapped || reqs[0].CostUSD != 0.2 {
				t.Errorf("good row after the bad one was not decoded: %+v", reqs)
			}
		})
	}
}

func TestLatestBySession(t *testing.T) {
	data, err := os.ReadFile("testdata/usage.csv")
	if err != nil {
		t.Fatal(err)
	}
	reqs, _, err := ParseUsageCSV(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("ParseUsageCSV() error = %v", err)
	}
	latest := LatestBySession(reqs)
	if len(latest) != 2 {
		t.Fatalf("got %d sessions, want 2", len(latest))
	}
	// Keys are lower-cased. The session in the fixture is upper-case in the
	// omp rows, and the newest of its rows is req-0003 (09:06Z, after 01:05Z).
	if got := latest["aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"].RequestID; got != "req-0003" {
		t.Errorf("newest omp request = %q, want req-0003", got)
	}
	if got := latest["11111111-2222-4333-8444-555555555555"].RequestID; got != "req-0008" {
		t.Errorf("newest claude request = %q, want req-0008", got)
	}
}
