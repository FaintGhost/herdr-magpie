package panel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// testNow is the clock every test model reads. It is fixed so that ages and
// countdowns are exact.
var testNow = time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)

// fakeSource answers the Source methods from fields, and can fail or block them.
type fakeSource struct {
	mu sync.Mutex

	agents   []magpie.AgentInfo
	quotas   []magpie.Quota
	accounts []magpie.Account
	profiles []magpie.Profile
	requests []magpie.Request
	api      *magpie.UsageSummary

	errs  map[string]error         // a method name and the error it returns
	block map[string]chan struct{} // a method name that waits on the channel, ignoring its context
	calls []string
	since time.Time // the last RecentRequests since argument
}

// call records a call to name and applies its configured error or block.
func (f *fakeSource) call(name string) error {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	err := f.errs[name]
	gate := f.block[name]
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return err
}

func (f *fakeSource) Agents(context.Context) ([]magpie.AgentInfo, error) {
	if err := f.call("Agents"); err != nil {
		return nil, err
	}
	return f.agents, nil
}

func (f *fakeSource) Quotas(context.Context) ([]magpie.Quota, error) {
	if err := f.call("Quotas"); err != nil {
		return nil, err
	}
	return f.quotas, nil
}

func (f *fakeSource) Accounts(context.Context) ([]magpie.Account, error) {
	if err := f.call("Accounts"); err != nil {
		return nil, err
	}
	return f.accounts, nil
}

func (f *fakeSource) Profiles(context.Context) ([]magpie.Profile, error) {
	if err := f.call("Profiles"); err != nil {
		return nil, err
	}
	return f.profiles, nil
}

func (f *fakeSource) RecentRequests(_ context.Context, since time.Time) ([]magpie.Request, error) {
	f.mu.Lock()
	f.since = since
	f.mu.Unlock()
	if err := f.call("RecentRequests"); err != nil {
		return nil, err
	}
	return f.requests, nil
}

func (f *fakeSource) APIUsage(context.Context) (*magpie.UsageSummary, error) {
	if err := f.call("APIUsage"); err != nil {
		return nil, err
	}
	return f.api, nil
}

// fakeActions records the switches it is asked for.
type fakeActions struct {
	mu       sync.Mutex
	switched []string // "agent user", in call order
	err      error
}

func (a *fakeActions) UseProfile(context.Context, string) error  { return errors.New("not used") }
func (a *fakeActions) SaveProfile(context.Context, string) error { return errors.New("not used") }

func (a *fakeActions) SwitchAccount(_ context.Context, agent, user string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.switched = append(a.switched, agent+" "+user)
	return a.err
}

// fakeCache keeps files in memory, encoding them as JSON the way state.Store does.
type fakeCache struct {
	mu      sync.Mutex
	files   map[string][]byte
	saves   int
	loadErr error
	saveErr error
}

func newFakeCache() *fakeCache {
	return &fakeCache{files: map[string][]byte{}}
}

func (c *fakeCache) Load(name string, v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loadErr != nil {
		return c.loadErr
	}
	data, ok := c.files[name]
	if !ok {
		return nil
	}
	return json.Unmarshal(data, v)
}

func (c *fakeCache) Save(name string, v any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.saveErr != nil {
		return c.saveErr
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.files[name] = data
	c.saves++
	return nil
}

// sampleSource is a source with synthetic data: two Claude Code accounts (one
// signed in), one Codex account, a balance card, and some usage rows.
func sampleSource() *fakeSource {
	h := func(d time.Duration) time.Time { return testNow.Add(d) }
	return &fakeSource{
		agents: []magpie.AgentInfo{
			{Name: "Claude Code", ID: "claude"},
			{Name: "Codex", ID: "codex"},
		},
		quotas: []magpie.Quota{
			{
				Provider: "claude", Name: "Claude Code", Kind: "subscription",
				Plan: "Max 20x", User: "dev@example.test",
				Windows: []magpie.QuotaWindow{
					{Name: "5 hours", Used: 62, Remaining: 38, ResetsAt: h(134 * time.Minute)},
					{Name: "7 days", Used: 38, Remaining: 62, ResetsAt: h(76 * time.Hour)},
				},
			},
			{
				Provider: "claude", Name: "Claude Code", Kind: "subscription",
				Plan: "Pro", User: "ops@example.test",
				Windows: []magpie.QuotaWindow{{Name: "5 hours", Used: 8, Remaining: 92, ResetsAt: h(3 * time.Hour)}},
			},
			{
				Provider: "codex", Name: "Codex", Kind: "subscription",
				Plan: "Plus", User: "dev@example.test",
				Windows: []magpie.QuotaWindow{{Name: "5 hours", Used: 93, Remaining: 7, ResetsAt: h(20 * time.Minute)}},
			},
			{Provider: "deepseek", Name: "DeepSeek", Kind: "balance"},
		},
		accounts: []magpie.Account{
			{Agent: "claude", User: "dev@example.test", Plan: "Max 20x", Active: true, On: true},
			{Agent: "claude", User: "ops@example.test", Plan: "Pro"},
			{Agent: "codex", User: "dev@example.test", Plan: "Plus", Active: true, On: true},
		},
		requests: sampleRequests(testNow),
		api: &magpie.UsageSummary{
			Calls: 412,
			Cost:  3.12,
			Agents: []magpie.UsageRow{
				{Name: "omp", Calls: 200, Errors: 5, Cost: 1.20},
				{Name: "claude", Calls: 150, Errors: 0, Cost: 1.50},
				{Name: "hermes", Calls: 62, Errors: 1, Cost: 0.42},
			},
			Models: []magpie.UsageRow{
				{Name: "claude-sonnet", Calls: 150, Cost: 2.00},
				{Name: "deepseek-chat", Calls: 262, Cost: 1.12},
			},
		},
	}
}

// sampleRequests has a provider storm (five 429s from opencode-go in the last
// ten minutes), a 402 from another provider, and rows the storm rules skip.
func sampleRequests(now time.Time) []magpie.Request {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	var out []magpie.Request
	for range 5 {
		out = append(out, magpie.Request{
			Time: ago(3 * time.Minute), Agent: "omp", Provider: "opencode-go",
			Status: 429, Error: true, ErrorType: "GoUsageLimitError",
		})
	}
	out = append(out,
		magpie.Request{Time: ago(20 * time.Minute), Agent: "hermes", Provider: "deepseek", Status: 402, Error: true, ErrorType: "balance"},
		magpie.Request{Time: ago(2 * time.Minute), Agent: "claude", Provider: "claude", Status: 200},
		magpie.Request{Time: ago(5 * time.Minute), Agent: "codex", Provider: "codex", Status: 400, Error: true},
		magpie.Request{Time: ago(90 * time.Minute), Agent: "claude", Provider: "claude", Status: 200},
		magpie.Request{Time: ago(30 * time.Hour), Agent: "claude", Provider: "claude", Status: 500, Error: true},
	)
	return out
}

// testModel builds a model over d with a clock fixed at testNow and no colour.
func testModel(t *testing.T, d Deps) *Model {
	t.Helper()
	if d.Now == nil {
		d.Now = func() time.Time { return testNow }
	}
	return newModel(t.Context(), d, newRenderer(io.Discard, false))
}

// drain runs cmd and everything it starts, feeding each message back to m. It
// never runs a tick, since a test model is not live and starts none.
func drain(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c)
		}
	default:
		_, next := m.Update(msg)
		drain(m, next)
	}
}

// press sends one key to m and runs what it starts.
func press(m *Model, k string) {
	_, cmd := m.Update(keyMsg(k))
	drain(m, cmd)
}

// keyMsg makes the key message bubbletea sends for the name k, as the tests spell it.
func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}
