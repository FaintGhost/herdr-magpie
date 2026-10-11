package magpie

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// sessionsCall is the one command Sessions runs, whatever the window.
const sessionsCall = "magpie sessions --json --limit 100000"

// fixtureNow is a fixed clock for window tests. The fixture sessions were last
// active on 2026-10-11, 2026-10-09 (05:00Z) and 2026-10-08.
var fixtureNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func readSessionsFixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("testdata/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSessionsCommand(t *testing.T) {
	for _, days := range []int{0, -3, 7} {
		r := &fakeRunner{out: map[string]string{sessionsCall: "[]"}}
		if _, err := Sessions(t.Context(), r, "", days); err != nil {
			t.Fatalf("Sessions(days=%d) error = %v", days, err)
		}
		if !slices.Equal(r.calls, []string{sessionsCall}) {
			t.Errorf("Sessions(days=%d) calls = %q, want only %q", days, r.calls, sessionsCall)
		}
	}
}

func TestSessionsWindow(t *testing.T) {
	tests := []struct {
		name        string
		days        int
		wantCount   int
		wantFirstID string
	}{
		{name: "zero uses the 14-day default", days: 0, wantCount: 3},
		{name: "negative uses the 14-day default", days: -1, wantCount: 3},
		{name: "two days keeps only the newest session", days: 2, wantCount: 1, wantFirstID: "11111111-2222-4333-8444-555555555555"},
		{name: "one day keeps nothing from the fixture", days: 1, wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{out: map[string]string{sessionsCall: readSessionsFixture(t)}}
			got, err := sessionsSince(t.Context(), r, "", tt.days, fixtureNow)
			if err != nil {
				t.Fatalf("sessionsSince() error = %v", err)
			}
			if len(got) != tt.wantCount {
				t.Fatalf("got %d sessions, want %d", len(got), tt.wantCount)
			}
			if tt.wantFirstID != "" && got[0].ID != tt.wantFirstID {
				t.Errorf("first session = %q, want %q", got[0].ID, tt.wantFirstID)
			}
		})
	}
}

func TestSessionsDecodesFixture(t *testing.T) {
	r := &fakeRunner{out: map[string]string{sessionsCall: readSessionsFixture(t)}}
	got, err := sessionsSince(t.Context(), r, "", 3650, fixtureNow)
	if err != nil {
		t.Fatalf("sessionsSince() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d sessions, want 3", len(got))
	}

	first := got[0]
	if first.Agent != "claude" || first.ID != "11111111-2222-4333-8444-555555555555" ||
		first.CWD != "/work/example-app" || first.Title != "Example session one" {
		t.Errorf("first session identity = %+v", first)
	}
	if want := time.Date(2026, 10, 10, 1, 2, 3, 500_000_000, time.UTC); !first.Start.Equal(want) {
		t.Errorf("Start = %v, want %v", first.Start, want)
	}
	if want := time.Date(2026, 10, 11, 9, 30, 0, 0, time.UTC); !first.Last.Equal(want) {
		t.Errorf("Last = %v, want %v", first.Last, want)
	}
	if first.Input != 1210 || first.Output != 3420 || first.CacheRead != 500 || first.CacheWrite != 60 {
		t.Errorf("token totals = %d/%d/%d/%d", first.Input, first.Output, first.CacheRead, first.CacheWrite)
	}
	if first.Cost != 0.42 || first.Unpriced != 1 || !first.Transcript {
		t.Errorf("cost=%v unpriced=%d transcript=%v", first.Cost, first.Unpriced, first.Transcript)
	}
	wantModels := []ModelUsage{
		{Model: "example-model-a", Input: 1200, Output: 3400, CacheRead: 500, CacheWrite: 60, Cost: 0.42, Priced: true},
		{Model: "example-model-b", Input: 10, Output: 20, CacheRead: 0, CacheWrite: 0, Cost: 0, Priced: false},
	}
	if !slices.Equal(first.Models, wantModels) {
		t.Errorf("Models = %+v, want %+v", first.Models, wantModels)
	}

	second := got[1]
	if second.ID != "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE" {
		t.Errorf("ID = %q; the case must be preserved", second.ID)
	}
	if want := time.Date(2026, 10, 9, 4, 0, 0, 0, time.UTC); !second.Start.Equal(want) {
		t.Errorf("Start with +08:00 offset = %v, want %v", second.Start, want)
	}
	if !second.Transcript || second.Cost != 0.01 {
		t.Errorf("second session transcript=%v cost=%v", second.Transcript, second.Cost)
	}

	third := got[2]
	if !third.Start.IsZero() {
		t.Errorf("empty start should give the zero time, got %v", third.Start)
	}
	if len(third.Models) != 0 || third.Transcript {
		t.Errorf("third session models=%d transcript=%v", len(third.Models), third.Transcript)
	}
}

func TestSessionsErrors(t *testing.T) {
	errRun := errors.New("runner failed")
	tests := []struct {
		name    string
		out     string
		runErr  error
		wantErr bool
		wantIs  error
		wantLen int
	}{
		{name: "runner error is wrapped", runErr: errRun, wantErr: true, wantIs: errRun},
		{name: "malformed JSON is an error", out: "not json", wantErr: true},
		{name: "summary object instead of a list is an error", out: `{"days_used":14}`, wantErr: true},
		{name: "unparseable start time is an error", out: `[{"id":"x","start":"yesterday","last":"2026-10-11T00:00:00Z"}]`, wantErr: true},
		{name: "null is an empty list", out: "null", wantLen: 0},
		{name: "empty array is an empty list", out: "[]", wantLen: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRunner{out: map[string]string{sessionsCall: tt.out}}
			if tt.runErr != nil {
				r.errs = map[string]error{sessionsCall: tt.runErr}
			}
			got, err := Sessions(t.Context(), r, "", 0)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Sessions() = %d sessions, want error", len(got))
				}
				if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
					t.Errorf("error %v does not wrap %v", err, tt.wantIs)
				}
				return
			}
			if err != nil {
				t.Fatalf("Sessions() error = %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("got %d sessions, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestSessionsByKey(t *testing.T) {
	sessions := []Session{
		{ID: "ABCDEF01-0000-4000-8000-000000000001", Agent: "omp"},
		{ID: "", Agent: "no-id"},
		{ID: "abcdef02-0000-4000-8000-000000000002", Agent: "claude"},
	}
	m := SessionsByKey(sessions)
	if len(m) != 2 {
		t.Fatalf("index has %d keys, want 2", len(m))
	}
	if got := m["abcdef01-0000-4000-8000-000000000001"].Agent; got != "omp" {
		t.Errorf("lower-case lookup of upper-case ID gave agent %q, want omp", got)
	}
	if _, ok := m[""]; ok {
		t.Error("session without an ID was indexed")
	}
	for k := range m {
		if k != strings.ToLower(k) {
			t.Errorf("key %q is not lower-cased", k)
		}
	}
}
