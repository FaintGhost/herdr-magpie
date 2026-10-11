package panel

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// panicky is a source whose profiles call panics.
type panicky struct{ *fakeSource }

func (panicky) Profiles(context.Context) ([]magpie.Profile, error) {
	panic("kaboom")
}

// seedCache runs one full refresh of the sample source into cache, at testNow.
func seedCache(t *testing.T, cache *fakeCache) {
	t.Helper()
	m := testModel(t, Deps{Source: sampleSource(), Cache: cache})
	drain(m, m.Init())
}

// sized returns a model of the given size, with its data loaded and no pending work.
func sized(t *testing.T, d Deps, w, h int) *Model {
	t.Helper()
	m := testModel(t, d)
	drain(m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func TestInitLoadsEveryFeedAndWritesTheCache(t *testing.T) {
	src, cache := sampleSource(), newFakeCache()
	m := testModel(t, Deps{Source: src, Cache: cache})
	drain(m, m.Init())

	for f := range numFeeds {
		if m.feeds[f].pending || m.feeds[f].err != "" || m.feeds[f].fromCache {
			t.Errorf("feed %d after a clean refresh = %+v", f, m.feeds[f])
		}
	}
	if n := len(m.snap.Quotas.Data); n != 4 {
		t.Errorf("quotas = %d, want 4", n)
	}
	if !m.snap.Quotas.At.Equal(testNow) {
		t.Errorf("quotas read at %v, want %v", m.snap.Quotas.At, testNow)
	}
	if cache.saves != 1 {
		t.Errorf("cache saved %d times, want once after the round", cache.saves)
	}
	if got := len(src.calls); got != int(numFeeds) {
		t.Errorf("source called %d times, want %d: %v", got, numFeeds, src.calls)
	}
	if got := ageLineOf(m); !strings.Contains(got, "updated just now") {
		t.Errorf("age line = %q", got)
	}
}

func TestRequestsReadFromTheStartOfToday(t *testing.T) {
	src := sampleSource()
	m := testModel(t, Deps{Source: src})
	drain(m, m.Init())
	src.mu.Lock()
	defer src.mu.Unlock()
	if want := usageSince(testNow); !src.since.Equal(want) {
		t.Errorf("RecentRequests since %v, want %v", src.since, want)
	}
}

func TestCachedFirstFrameBeforeAnyFetch(t *testing.T) {
	cache := newFakeCache()
	seedCache(t, cache)

	later := testNow.Add(42 * time.Second)
	m := newModel(t.Context(), Deps{Source: sampleSource(), Cache: cache, Now: func() time.Time { return later }},
		newRenderer(io.Discard, false))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tab = tabAllowances

	view := m.View()
	if !strings.Contains(view, "cached 42s ago") {
		t.Errorf("first frame lacks the cache age:\n%s", view)
	}
	if !strings.Contains(view, "Max 20x") {
		t.Errorf("first frame should already show the cached cards:\n%s", view)
	}

	m.Init() // starts the fetches, which this test does not run
	if view := m.View(); !strings.Contains(view, "refreshing…") {
		t.Errorf("after Init the panel should say it is refreshing:\n%s", view)
	}
}

func TestFailedFeedKeepsItsCacheAndSaysSo(t *testing.T) {
	cache := newFakeCache()
	seedCache(t, cache)
	before := cache.saves

	later := testNow.Add(42 * time.Second)
	src := sampleSource()
	src.errs = map[string]error{"Quotas": errors.New("boom")}
	m := newModel(t.Context(), Deps{Source: src, Cache: cache, Now: func() time.Time { return later }},
		newRenderer(io.Discard, false))
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.tab = tabAllowances
	drain(m, m.Init())

	if !m.feeds[feedQuotas].fromCache || m.feeds[feedQuotas].err != "boom" {
		t.Errorf("quotas state = %+v, want cached and failed", m.feeds[feedQuotas])
	}
	if !m.feeds[feedAccounts].pending && m.feeds[feedAccounts].err != "" {
		t.Errorf("accounts failed too: %+v", m.feeds[feedAccounts])
	}
	view := m.View()
	if !strings.Contains(view, "quotas: boom") || !strings.Contains(view, "Max 20x") {
		t.Errorf("view should show the error and keep the cached cards:\n%s", view)
	}
	if !m.snap.Quotas.At.Equal(testNow) {
		t.Errorf("a failed read changed the quota stamp to %v", m.snap.Quotas.At)
	}
	if cache.saves != before+1 {
		t.Errorf("cache saved %d times after the refresh, want one more", cache.saves-before)
	}
}

func TestSourceTimeoutBlanksOnlyItsFeed(t *testing.T) {
	old := fetchTimeout
	fetchTimeout = 20 * time.Millisecond
	t.Cleanup(func() { fetchTimeout = old })

	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	src := sampleSource()
	src.block = map[string]chan struct{}{"Quotas": gate}

	m := testModel(t, Deps{Source: src})
	drain(m, m.Init())
	if got := m.feeds[feedQuotas].err; got != "timed out after 20ms" {
		t.Errorf("quotas error = %q", got)
	}
	if e := m.feeds[feedAccounts].err; e != "" {
		t.Errorf("accounts should be unaffected, got %q", e)
	}
}

func TestSourcePanicBecomesAnError(t *testing.T) {
	m := testModel(t, Deps{Source: panicky{sampleSource()}})
	drain(m, m.Init())
	if got := m.feeds[feedProfiles].err; got != "panic: kaboom" {
		t.Errorf("profiles error = %q", got)
	}
}

func TestNoSourceSaysSo(t *testing.T) {
	m := testModel(t, Deps{})
	if cmd := m.Init(); cmd != nil {
		t.Errorf("Init with no source returned a command")
	}
	for f := range numFeeds {
		if m.feeds[f].err != "no data source configured" || m.feeds[f].pending {
			t.Errorf("feed %d = %+v", f, m.feeds[f])
		}
	}
}

func TestTabKeys(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource()}, 80, 24)
	if m.tab != tabAgents {
		t.Fatalf("default tab = %v, want Agents", m.tab)
	}
	steps := []struct {
		key  string
		want tab
	}{
		{"2", tabAllowances},
		{"5", tabSessions},
		{"tab", tabAgents},
		{"shift+tab", tabSessions},
		{"3", tabUsage},
		{"0", tabUsage},
	}
	for _, s := range steps {
		press(m, s.key)
		if m.tab != s.want {
			t.Errorf("after %q: tab = %v, want %v", s.key, m.tab, s.want)
		}
	}
}

func TestStartTab(t *testing.T) {
	tests := map[string]tab{
		"":            tabAgents,
		"usage":       tabUsage,
		"ALLOWANCES":  tabAllowances,
		"4":           tabRouting,
		"nonsense":    tabAgents,
		"  sessions ": tabSessions,
	}
	for start, want := range tests {
		m := testModel(t, Deps{Source: sampleSource(), StartTab: start})
		if m.tab != want {
			t.Errorf("StartTab %q: tab = %v, want %v", start, m.tab, want)
		}
	}
}

func TestBurstOfKeysIsApplied(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource()}, 80, 24)
	if _, cmd := m.Update(keyMsg("23")); cmd != nil {
		t.Errorf("a burst of tab digits started %v", cmd)
	}
	if m.tab != tabUsage {
		t.Errorf("burst \"23\": tab = %v, want Usage", m.tab)
	}
	_, cmd := m.Update(keyMsg("2q"))
	if cmd == nil {
		t.Fatal("burst ending in q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("burst ending in q did not quit: %T", cmd())
	}
	if m.tab != tabAllowances {
		t.Errorf("burst \"2q\": tab = %v, want Allowances", m.tab)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		m := testModel(t, Deps{Source: sampleSource()})
		_, cmd := m.Update(keyMsg(k))
		if cmd == nil {
			t.Errorf("%q returned no command", k)
			continue
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%q did not quit", k)
		}
	}
}

func TestRefreshKeyStartsEveryFeedOnce(t *testing.T) {
	src := sampleSource()
	m := sized(t, Deps{Source: src}, 80, 24)
	_, cmd := m.Update(keyMsg("r"))
	if cmd == nil {
		t.Fatal("r returned no command")
	}
	for f := range numFeeds {
		if !m.feeds[f].pending {
			t.Errorf("feed %d not pending after r", f)
		}
	}
	if _, again := m.Update(keyMsg("r")); again != nil {
		t.Error("a second r while everything is in flight should start nothing")
	}
	drain(m, cmd)
	if got := len(src.calls); got != 2*int(numFeeds) {
		t.Errorf("source called %d times over two rounds, want %d", got, 2*numFeeds)
	}
}

func TestCursorMovesAmongTheCards(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource()}, 80, 24)
	press(m, "2")
	press(m, "down")
	if m.cursor != 1 {
		t.Fatalf("cursor = %d after one down, want 1", m.cursor)
	}
	for range 5 {
		press(m, "down")
	}
	if m.cursor != 3 {
		t.Errorf("cursor = %d at the bottom, want 3", m.cursor)
	}
	for range 10 {
		press(m, "up")
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d at the top, want 0", m.cursor)
	}
}

func TestSwitchWritesOnlyAfterTheConfirmLine(t *testing.T) {
	act := &fakeActions{}
	src := sampleSource()
	m := sized(t, Deps{Source: src, Actions: act}, 80, 24)
	press(m, "2")

	press(m, "enter") // the signed-in dev card: the list opens on ops
	if m.flow != flowPick || m.pick.cands[m.pick.cursor].User != "ops@example.test" {
		t.Fatalf("after enter: flow %v, cursor %+v", m.flow, m.pick)
	}
	press(m, "enter") // ops is not active, so the confirm line appears
	want := "Switch Claude Code to ops@example.test? Enter to confirm, Esc to cancel"
	if m.ask == nil || clean(m.ask.text) != want {
		t.Fatalf("confirm line = %+v", m.ask)
	}
	if got := ansiFree(m.statusLine(120)); !strings.Contains(got, want) {
		t.Errorf("status line = %q", got)
	}
	if len(act.switched) != 0 {
		t.Fatalf("switched before Enter on the confirm line: %v", act.switched)
	}

	press(m, "enter")
	if len(act.switched) != 1 || act.switched[0] != "claude ops@example.test" {
		t.Errorf("switched = %v", act.switched)
	}
	if m.writing {
		t.Error("still writing after the result landed")
	}
	if m.noticeKind != noticeOK || m.notice != "switched Claude Code to ops@example.test" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}
	if got := src.calls[len(src.calls)-2:]; got[0] != "Accounts" || got[1] != "Quotas" {
		t.Errorf("after a switch the accounts and quotas should be read again, calls end %v", got)
	}
}

func TestSwitchCancelsAtEitherStep(t *testing.T) {
	act := &fakeActions{}
	m := sized(t, Deps{Source: sampleSource(), Actions: act}, 80, 24)
	press(m, "2")

	press(m, "enter")
	press(m, "esc")
	if m.flow != flowIdle || m.notice != "switch cancelled" {
		t.Errorf("esc on the list: flow %v, notice %q", m.flow, m.notice)
	}

	press(m, "enter")
	press(m, "enter")
	press(m, "esc")
	if m.flow != flowIdle || len(act.switched) != 0 {
		t.Errorf("esc on the confirm line: flow %v, switched %v", m.flow, act.switched)
	}
}

func TestPickingTheActiveAccountSaysSo(t *testing.T) {
	act := &fakeActions{}
	m := sized(t, Deps{Source: sampleSource(), Actions: act}, 80, 24)
	press(m, "2")
	press(m, "enter")
	press(m, "up") // dev, the signed-in one
	press(m, "enter")
	if m.flow != flowIdle || m.notice != "dev@example.test is already active" || len(act.switched) != 0 {
		t.Errorf("flow %v, notice %q, switched %v", m.flow, m.notice, act.switched)
	}
}

func TestOnlySubscriptionsSwitch(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource(), Actions: &fakeActions{}}, 80, 24)
	press(m, "2")
	for range 3 {
		press(m, "down")
	}
	press(m, "enter")
	if m.flow != flowIdle || m.notice != "only subscription accounts can be switched" {
		t.Errorf("balance card: flow %v, notice %q", m.flow, m.notice)
	}
}

func TestSwitchNeedsKnownAccounts(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource(), Actions: &fakeActions{}}, 80, 24)
	press(m, "2")

	m.feeds[feedAccounts] = feedState{err: "magpie accounts: exit 1", fromCache: true}
	press(m, "enter")
	if m.notice != "accounts unavailable: magpie accounts: exit 1" {
		t.Errorf("notice = %q", m.notice)
	}

	m.feeds[feedAccounts] = feedState{pending: true}
	m.snap.Accounts = stamp[[]magpie.Account]{}
	press(m, "enter")
	if m.notice != "accounts still loading" {
		t.Errorf("notice while loading = %q", m.notice)
	}
}

func TestSwitchFailuresAreReported(t *testing.T) {
	act := &fakeActions{err: errors.New("account locked")}
	m := sized(t, Deps{Source: sampleSource(), Actions: act}, 80, 24)
	press(m, "2")
	press(m, "enter")
	press(m, "enter")
	press(m, "enter")
	if m.noticeKind != noticeBad || m.notice != "switch failed: account locked" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}

	none := sized(t, Deps{Source: sampleSource()}, 80, 24)
	press(none, "2")
	press(none, "enter")
	press(none, "enter")
	press(none, "enter")
	if none.notice != "switch failed: switching is not configured" {
		t.Errorf("notice without Actions = %q", none.notice)
	}
}

func TestNothingQuitsWhileAWriteIsInFlight(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource(), Actions: &fakeActions{}}, 80, 24)
	m.writing = true
	for _, k := range []string{"q", "esc", "r", "enter"} {
		if _, cmd := m.Update(keyMsg(k)); cmd != nil {
			t.Errorf("%q during a write returned a command", k)
		}
	}
	if _, cmd := m.Update(keyMsg("ctrl+c")); cmd == nil {
		t.Error("ctrl+c should still quit during a write")
	}
	press(m, "3")
	if m.tab != tabUsage {
		t.Error("tab keys should still work during a write")
	}
}

func TestWindowSizeIsTracked(t *testing.T) {
	m := testModel(t, Deps{Source: sampleSource()})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	if m.width != 120 || m.height != 32 {
		t.Errorf("size = %dx%d", m.width, m.height)
	}
	if lines := strings.Split(m.View(), "\n"); len(lines) != 32 {
		t.Errorf("View has %d lines at 120x32", len(lines))
	}
}

func TestEveryTabHasItsOwnBody(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource()}, 80, 24)
	for _, c := range []struct {
		key, want string
	}{
		{"1", "Agents"}, {"2", "Max 20x"}, {"3", "Provider errors"}, {"4", "last 1h"}, {"5", "press / to filter"},
	} {
		press(m, c.key)
		if got := m.View(); !strings.Contains(got, c.want) {
			t.Errorf("tab %v lacks %q:\n%s", m.tab, c.want, got)
		}
	}
}

// ageLineOf is the age line of m as text.
func ageLineOf(m *Model) string {
	return m.ageText()
}

// ansiFree strips the SGR sequences the panel draws, for text comparisons.
func ansiFree(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
