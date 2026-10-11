package panelsrc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/state"
)

// fakeMagpie answers magpie CLI calls from a table keyed by "name arg ...", and
// fails any call that is not in the table. It records every call in order.
type fakeMagpie struct {
	out   map[string]string
	errs  map[string]error
	calls []string
}

func (f *fakeMagpie) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, key)
	if err := f.errs[key]; err != nil {
		return nil, err
	}
	out, ok := f.out[key]
	if !ok {
		return nil, errors.New("unexpected magpie call: " + key)
	}
	return []byte(out), nil
}

func TestSourceRunsTheMagpieCommands(t *testing.T) {
	f := &fakeMagpie{out: map[string]string{
		"/opt/magpie quota --json":                   `[]`,
		"/opt/magpie accounts --json":                `[]`,
		"/opt/magpie profiles":                       "no profiles yet · magpie save <name>\n",
		"/opt/magpie sessions --json --limit 100000": `[]`,
	}}
	src := Source{Magpie: f, Bin: "/opt/magpie", Days: 7}
	ctx := t.Context()
	if got, err := src.Quotas(ctx); err != nil || len(got) != 0 {
		t.Errorf("Quotas() = %v, %v", got, err)
	}
	if got, err := src.Accounts(ctx); err != nil || len(got) != 0 {
		t.Errorf("Accounts() = %v, %v", got, err)
	}
	if got, err := src.Profiles(ctx); err != nil || len(got) != 0 {
		t.Errorf("Profiles() = %v, %v", got, err)
	}
	if _, err := src.Sessions(ctx); err != nil {
		t.Errorf("Sessions() error: %v", err)
	}
	want := []string{
		"/opt/magpie quota --json",
		"/opt/magpie accounts --json",
		"/opt/magpie profiles",
		"/opt/magpie sessions --json --limit 100000",
	}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

func TestSourceAgentsKeepsTheUnrecognizedSentinel(t *testing.T) {
	f := &fakeMagpie{out: map[string]string{"/opt/magpie ls": "garbage\n"}}
	_, err := Source{Magpie: f, Bin: "/opt/magpie"}.Agents(t.Context())
	if !errors.Is(err, magpie.ErrUnrecognized) {
		t.Errorf("Agents() error = %v, want %v", err, magpie.ErrUnrecognized)
	}
}

func TestSourceSessionsUsesTheWindow(t *testing.T) {
	now := time.Now()
	recent := magpie.Session{Agent: "claude", ID: "recent"}
	old := magpie.Session{Agent: "claude", ID: "old"}
	raw, err := json.Marshal([]map[string]any{
		{"agent": recent.Agent, "id": recent.ID, "last": now.AddDate(0, 0, -1).Format(time.RFC3339), "start": now.AddDate(0, 0, -2).Format(time.RFC3339)},
		{"agent": old.Agent, "id": old.ID, "last": now.AddDate(0, 0, -30).Format(time.RFC3339), "start": now.AddDate(0, 0, -31).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeMagpie{out: map[string]string{"/opt/magpie sessions --json --limit 100000": string(raw)}}
	got, err := Source{Magpie: f, Bin: "/opt/magpie", Days: 7}.Sessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "recent" {
		t.Errorf("Sessions() kept %v, want only the recent one", got)
	}
}

func TestRecentRequestsReadsTheUsageLogSince(t *testing.T) {
	now := time.Now()
	rows := []map[string]any{
		{"t": now.Add(-2 * time.Hour).Format(time.RFC3339), "agent": "claude", "provider": "p1", "model": "m-a", "req": "p1/m-a", "status": 200, "ttft_ms": 900},
		{"t": now.Add(-5 * time.Minute).Format(time.RFC3339), "agent": "claude", "provider": "p1", "model": "m-a", "req": "p1/m-a", "served": "m-b", "status": 429, "err": "rate", "err_type": "rate_limit"},
	}
	var b strings.Builder
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Source{UsagePath: path}.RecentRequests(t.Context(), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != 429 || !got[0].Swapped || got[0].RequestedModel != "p1/m-a" {
		t.Errorf("RecentRequests() = %+v, want the one recent failed request", got)
	}

	missing := Source{UsagePath: filepath.Join(t.TempDir(), "none.jsonl")}
	if got, err := missing.RecentRequests(t.Context(), now.Add(-time.Hour)); err != nil || got != nil {
		t.Errorf("missing log = %v, %v; want nil, nil", got, err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (Source{UsagePath: path}).RecentRequests(cancelled, time.Time{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled read error = %v, want context.Canceled", err)
	}
}

// keyFile writes a gateway key file with mode 0600 in a temporary directory.
func keyFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway-key")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAPIUsageIsNilWithoutAKey(t *testing.T) {
	src := Source{API: magpie.APIClient{BaseURL: "http://127.0.0.1:1", KeyFile: filepath.Join(t.TempDir(), "missing")}}
	got, err := src.APIUsage(t.Context())
	if err != nil || got != nil {
		t.Errorf("APIUsage() = %v, %v; want nil, nil", got, err)
	}
}

func TestAPIUsageAsksForTodayWithTheKey(t *testing.T) {
	const key = "synthetic-key-for-tests"
	var mu sync.Mutex
	var period, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		period, auth = req.URL.Query().Get("period"), req.Header.Get("Authorization")
		mu.Unlock()
		if auth != "Bearer "+key {
			http.Error(w, `{"error":{"message":"no API key was sent"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"calls":12,"cost":1.5,"agents":[{"id":"claude","name":"Claude Code","calls":10,"errors":1,"cost":1.2}],"models":[]}`))
	}))
	t.Cleanup(srv.Close)

	src := Source{API: magpie.APIClient{BaseURL: srv.URL, KeyFile: keyFile(t, key+"\n"), HTTP: srv.Client()}}
	got, err := src.APIUsage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Calls != 12 || len(got.Agents) != 1 || got.Agents[0].Name != "Claude Code" {
		t.Errorf("APIUsage() = %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if period != "today" || auth != "Bearer "+key {
		t.Errorf("request period = %q, auth = %q; want today and the key", period, auth)
	}
}

func TestQuotasUseTheGatewayWithAKey(t *testing.T) {
	const key = "synthetic-key-for-tests"
	var mu sync.Mutex
	var auth string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/magpie/quotas", func(w http.ResponseWriter, req *http.Request) {
		mu.Lock()
		auth = req.Header.Get("Authorization")
		mu.Unlock()
		if auth != "Bearer "+key {
			http.Error(w, `{"error":{"message":"no API key was sent"}}`, http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"provider":"claude","name":"Claude Code","kind":"subscription","plan":"Pro","user":"dev@example.test"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	f := &fakeMagpie{}
	src := Source{Magpie: f, Bin: "/opt/magpie", API: magpie.APIClient{BaseURL: srv.URL, KeyFile: keyFile(t, key+"\n"), HTTP: srv.Client()}}
	got, err := src.Quotas(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Claude Code" || got[0].User != "dev@example.test" {
		t.Errorf("Quotas() = %+v", got)
	}
	if len(f.calls) != 0 {
		t.Errorf("the CLI ran although the gateway answered: %q", f.calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if auth != "Bearer "+key {
		t.Errorf("auth = %q, want the key", auth)
	}
}

func TestQuotasFallBackToTheCLI(t *testing.T) {
	const cliOut = `[{"provider":"claude","name":"Claude Code","kind":"subscription","plan":"Pro"}]`
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	tests := []struct {
		name string
		api  magpie.APIClient
	}{
		{"no key file", magpie.APIClient{BaseURL: failing.URL, KeyFile: filepath.Join(t.TempDir(), "missing")}},
		{"gateway fails", magpie.APIClient{BaseURL: failing.URL, KeyFile: keyFile(t, "synthetic-key-for-tests\n"), HTTP: failing.Client()}},
		{"no gateway URL", magpie.APIClient{KeyFile: keyFile(t, "synthetic-key-for-tests\n")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeMagpie{out: map[string]string{"/opt/magpie quota --json": cliOut}}
			got, err := Source{Magpie: f, Bin: "/opt/magpie", API: tc.api}.Quotas(t.Context())
			if err != nil || len(got) != 1 || got[0].Name != "Claude Code" {
				t.Fatalf("Quotas() = %+v, %v", got, err)
			}
			if !slices.Equal(f.calls, []string{"/opt/magpie quota --json"}) {
				t.Errorf("calls = %q, want the CLI quota call", f.calls)
			}
		})
	}
}

// gateRunner holds each CLI call until release is closed, and counts the calls in flight.
type gateRunner struct {
	release chan struct{}
	running atomic.Int64
	peak    atomic.Int64
}

func (g *gateRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	n := g.running.Add(1)
	defer g.running.Add(-1)
	for {
		p := g.peak.Load()
		if n <= p || g.peak.CompareAndSwap(p, n) {
			break
		}
	}
	select {
	case <-g.release:
	case <-ctx.Done():
	}
	return []byte("[]"), nil
}

// waitRunning waits until n CLI calls are in flight, or fails the test.
func (g *gateRunner) waitRunning(t *testing.T, n int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for g.running.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d CLI reads running, want %d", g.running.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestCLIReadsRunTwoAtATime(t *testing.T) {
	g := &gateRunner{release: make(chan struct{})}
	src := Source{Magpie: g, Bin: "/opt/magpie"}
	var reads sync.WaitGroup
	for range 6 {
		reads.Go(func() { _, _ = src.Accounts(t.Context()) })
	}
	g.waitRunning(t, maxCLIReads)
	time.Sleep(20 * time.Millisecond) // a third read, if it were allowed, would start now
	if n := g.running.Load(); n != maxCLIReads {
		t.Errorf("%d CLI reads running, want %d", n, maxCLIReads)
	}
	close(g.release)
	reads.Wait()
	if p := g.peak.Load(); p != maxCLIReads {
		t.Errorf("peak = %d, want %d", p, maxCLIReads)
	}
}

func TestCLIReadGivesUpWhenItsContextEnds(t *testing.T) {
	g := &gateRunner{release: make(chan struct{})}
	src := Source{Magpie: g, Bin: "/opt/magpie"}
	var holders sync.WaitGroup
	t.Cleanup(func() {
		close(g.release)
		holders.Wait()
	})
	for range maxCLIReads {
		holders.Go(func() { _, _ = src.Accounts(t.Context()) })
	}
	g.waitRunning(t, maxCLIReads)

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := src.Accounts(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Accounts() with every slot taken = %v, want the context's deadline", err)
	}
	if n := g.running.Load(); n != maxCLIReads {
		t.Errorf("%d CLI reads running, want the two holders only", n)
	}
}

// capRunner records how long the context of its call runs for.
type capRunner struct {
	left time.Duration
}

func (c *capRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	if d, ok := ctx.Deadline(); ok {
		c.left = time.Until(d)
	}
	return []byte("[]"), nil
}

func TestCLIReadsGetTwentySeconds(t *testing.T) {
	r := &capRunner{}
	if _, err := (Source{Magpie: r, Bin: "/opt/magpie"}).Accounts(t.Context()); err != nil {
		t.Fatal(err)
	}
	if r.left <= 19*time.Second || r.left > cliReadTimeout {
		t.Errorf("the read's cap leaves %v, want about %v", r.left, cliReadTimeout)
	}
}

func TestCacheKeepsOnlyThePanelSnapshot(t *testing.T) {
	dir := t.TempDir()
	store, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cache := Cache{Store: store}
	type snap struct {
		Version int `json:"version"`
	}
	if err := cache.Save("panel.json", snap{Version: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "panel.json")); err != nil {
		t.Errorf("snapshot file: %v", err)
	}
	var got snap
	if err := cache.Load("panel.json", &got); err != nil || got.Version != 2 {
		t.Errorf("Load() = %+v, %v; want version 2", got, err)
	}
	if err := cache.Save("status.json", snap{}); err == nil {
		t.Error("Save accepted another file name")
	}
	if err := cache.Load("refresh.json", &got); err == nil {
		t.Error("Load accepted another file name")
	}
}
