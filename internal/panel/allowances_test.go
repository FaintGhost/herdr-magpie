package panel

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

func TestBuildCards(t *testing.T) {
	src := sampleSource()
	cards := buildCards(src.quotas, src.accounts)
	if len(cards) != 4 {
		t.Fatalf("got %d cards, want 4", len(cards))
	}
	tests := []struct {
		i             int
		agent         string
		multi, active bool
	}{
		{0, "claude", true, true},  // dev is signed in, and claude has two accounts
		{1, "claude", true, false}, // ops is the other account
		{2, "codex", false, false}, // one account: nothing to mark
		{3, "", false, false},      // a balance has no agent
	}
	for _, tc := range tests {
		c := cards[tc.i]
		if c.agent != tc.agent || c.multi != tc.multi || c.active != tc.active {
			t.Errorf("card %d = agent %q multi %v active %v, want %q %v %v",
				tc.i, c.agent, c.multi, c.active, tc.agent, tc.multi, tc.active)
		}
	}
}

func TestSwitchChoicesSkipAccountsWithoutUser(t *testing.T) {
	accounts := []magpie.Account{
		{Agent: "claude", User: "a@example.test"},
		{Agent: "claude", User: "  "},
		{Agent: "claude", User: "b@example.test", Active: true},
		{Agent: "codex", User: "c@example.test"},
	}
	got := switchChoices(accounts, "claude")
	if len(got) != 2 || got[0].User != "a@example.test" || got[1].User != "b@example.test" {
		t.Errorf("switchChoices = %+v", got)
	}
}

func TestAgentLabel(t *testing.T) {
	agents := []magpie.AgentInfo{{ID: "claude", Name: "Claude Code"}, {ID: "pi"}}
	tests := map[string]string{
		"claude": "Claude Code",
		"pi":     "pi",
		"other":  "other",
	}
	for id, want := range tests {
		if got := agentLabel(id, agents); got != want {
			t.Errorf("agentLabel(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestVisibleSpan(t *testing.T) {
	tests := []struct {
		name       string
		heights    []int
		cursor     int
		rows       int
		start, end int
	}{
		{name: "cursor at the bottom scrolls up", heights: []int{4, 4, 4, 4}, cursor: 3, rows: 9, start: 2, end: 4},
		{name: "cursor at the top shows the first", heights: []int{4, 4, 4, 4}, cursor: 0, rows: 9, start: 0, end: 2},
		{name: "everything fits", heights: []int{2, 2}, cursor: 1, rows: 10, start: 0, end: 2},
		{name: "a block taller than the screen is still shown", heights: []int{4, 9}, cursor: 1, rows: 3, start: 1, end: 2},
		{name: "no rows", heights: []int{4}, cursor: 0, rows: 0, start: 0, end: 0},
		{name: "no blocks", heights: nil, cursor: 0, rows: 5, start: 0, end: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end := visibleSpan(tc.heights, tc.cursor, tc.rows)
			if start != tc.start || end != tc.end {
				t.Errorf("visibleSpan = [%d,%d), want [%d,%d)", start, end, tc.start, tc.end)
			}
		})
	}
}

// assertLines fails when a line is wider than w columns, by either width rule.
func assertLines(t *testing.T, lines []string, w int) {
	t.Helper()
	for i, l := range lines {
		if dw := displayWidth(ansiFree(l)); dw > w {
			t.Errorf("line %d is %d columns, screen is %d: %q", i, dw, w, l)
		}
		if lw := lipgloss.Width(l); lw > w {
			t.Errorf("line %d lipgloss-measures %d columns, screen is %d: %q", i, lw, w, l)
		}
	}
}

// accountCols is the account columns for cards, as the tab sizes them.
func accountCols(cards []card) acctCols {
	return acctColumns(cards)
}

func TestAccountLinesFitAndCarryTheNumbers(t *testing.T) {
	src := sampleSource()
	cards := buildCards(src.quotas, src.accounts)
	for _, w := range []int{40, 80, 120} {
		bw, _ := splitWidth(w)
		lines := accountLines(cards[0], true, testNow, bw, accountCols(cards), newStyles(newRenderer(io.Discard, false)))
		assertLines(t, lines, w)
		got := strings.Join(lines, "\n")
		if w >= 80 {
			for _, want := range []string{"▸ ", "Max 20x", "dev@example.test", "● active", "62%", "2h14m"} {
				if !strings.Contains(got, want) {
					t.Errorf("w=%d: account lacks %q:\n%s", w, want, got)
				}
			}
		}
	}
}

func TestAccountLinesCutLongAccount(t *testing.T) {
	c := card{quota: magpie.Quota{
		Provider: "claude", Name: "Claude Code", Kind: "subscription", Plan: "Max 20x",
		User:    "someone.with.a.very.long.address.that.keeps.going@example.test",
		Windows: []magpie.QuotaWindow{{Name: "5 hours", Used: 10}},
	}}
	bw, _ := splitWidth(60)
	lines := accountLines(c, false, testNow, bw, acctColumns([]card{c}), newStyles(newRenderer(io.Discard, false)))
	assertLines(t, lines, 60)
	if !strings.Contains(lines[0], "…") {
		t.Errorf("the account %q should be cut with an ellipsis", lines[0])
	}
}

func TestAccountWindowsWrapUnderTheFirst(t *testing.T) {
	c := card{quota: magpie.Quota{Provider: "codex", Kind: "subscription", Name: "Codex", Plan: "Plus", User: "dev@example.test"}}
	for i := range 3 {
		c.quota.Windows = append(c.quota.Windows, magpie.QuotaWindow{Name: "window " + string(rune('a'+i)), Used: 50})
	}
	bw, _ := splitWidth(60)
	lines := accountLines(c, false, testNow, bw, acctColumns([]card{c}), newStyles(newRenderer(io.Discard, false)))
	if len(lines) < 2 {
		t.Fatalf("three windows at 60 columns should wrap:\n%s", strings.Join(lines, "\n"))
	}
	first := strings.Index(lines[0], "window a")
	if first < 0 || !strings.HasPrefix(strings.TrimLeft(lines[1], " "), "window b") {
		t.Errorf("the second window should follow on its own line:\n%s", strings.Join(lines, "\n"))
	}
	if got := displayWidth(lines[1][:strings.Index(lines[1], "window b")]); got != first {
		t.Errorf("the wrapped window starts at column %d, the first at %d", got, first)
	}
}

func TestAccountManyWindowsCountTheRest(t *testing.T) {
	c := card{quota: magpie.Quota{Provider: "codex", Kind: "subscription", Name: "Codex"}}
	for i := range 6 {
		c.quota.Windows = append(c.quota.Windows, magpie.QuotaWindow{Name: "w" + string(rune('a'+i)), Used: 1})
	}
	bw, _ := splitWidth(200)
	got := strings.Join(accountLines(c, false, testNow, bw, acctColumns([]card{c}), newStyles(newRenderer(io.Discard, false))), "\n")
	if !strings.Contains(got, fmt.Sprintf("+%d more windows", 6-(maxWindowRows-1))) {
		t.Errorf("six windows should show %d and count the rest:\n%s", maxWindowRows-1, got)
	}
}

func TestBalanceAccountShowsTheBalance(t *testing.T) {
	src := sampleSource()
	cards := buildCards(src.quotas, src.accounts)
	st := newStyles(newRenderer(io.Discard, false))
	bw, _ := splitWidth(80)
	cols := accountCols(cards)
	if got := strings.Join(accountLines(cards[3], false, testNow, bw, cols, st), "\n"); !strings.Contains(got, "balance: not reported") {
		t.Errorf("balance without an amount:\n%s", got)
	}
	cards[3].quota.Balance = "$12.34"
	if got := strings.Join(accountLines(cards[3], false, testNow, bw, cols, st), "\n"); !strings.Contains(got, "balance: $12.34") {
		t.Errorf("balance:\n%s", got)
	}
}

func TestQuotaErrorReplacesTheWindows(t *testing.T) {
	c := card{quota: magpie.Quota{
		Provider: "claude", Name: "Claude Code", Kind: "subscription", Plan: "Max 20x", User: "dev@example.test",
		Windows: []magpie.QuotaWindow{{Name: "5 hours", Used: 62}},
		Error:   "token expired\nlog in again",
	}}
	bw, _ := splitWidth(80)
	lines := accountLines(c, false, testNow, bw, acctColumns([]card{c}), newStyles(newRenderer(io.Discard, false)))
	assertLines(t, lines, 80)
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "⚠ token expired log in again") {
		t.Errorf("the account lacks its error inline:\n%s", got)
	}
	if strings.Contains(got, "5 hours") || strings.Contains(got, "used") {
		t.Errorf("an errored account should not draw its windows:\n%s", got)
	}
}

// ansiStyles colours with the 16 basic colours, whatever the output is.
func ansiStyles() styles {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	return newStyles(r)
}

func TestWindowColoursByUsage(t *testing.T) {
	st := ansiStyles()
	tests := []struct {
		used       float64
		wantYellow bool
		wantRed    bool
	}{
		{used: 10},
		{used: 75, wantYellow: true},
		{used: 95, wantRed: true},
	}
	for _, tc := range tests {
		got := line(80, windowSegs(magpie.QuotaWindow{Name: "5 hours", Used: tc.used}, testNow, st)...)
		if has := strings.Contains(got, "\x1b[33m"); has != tc.wantYellow {
			t.Errorf("used %v: yellow = %v, want %v", tc.used, has, tc.wantYellow)
		}
		if has := strings.Contains(got, "\x1b[31m"); has != tc.wantRed {
			t.Errorf("used %v: red = %v, want %v", tc.used, has, tc.wantRed)
		}
	}
}

func TestNoColourIsPlain(t *testing.T) {
	st := newStyles(newRenderer(io.Discard, false))
	got := line(80, windowSegs(magpie.QuotaWindow{Name: "5 hours", Used: 95}, testNow, st)...)
	if strings.Contains(got, "\x1b") {
		t.Errorf("plain renderer wrote an escape sequence: %q", got)
	}
	if !strings.Contains(got, " 95%") {
		t.Errorf("plain line lost the number: %q", got)
	}
}

func TestPickerLines(t *testing.T) {
	src := sampleSource()
	m := testModel(t, Deps{Source: src})
	m.snap.Agents = stampAt(testNow, src.agents)
	m.snap.Accounts = stampAt(testNow, src.accounts)
	m.pick = pickState{agent: "claude", cands: switchChoices(src.accounts, "claude"), cursor: 1}
	m.flow = flowPick

	lines := m.allowancesLines(80, 10)
	assertLines(t, lines, 80)
	got := strings.Join(lines, "\n")
	for _, want := range []string{"Switch Claude Code to which account?", "● dev@example.test  (active)", "▸ ○ ops@example.test"} {
		if !strings.Contains(got, want) {
			t.Errorf("picker lacks %q:\n%s", want, got)
		}
	}
}

func TestAllowancesStates(t *testing.T) {
	t.Run("loading before the first read", func(t *testing.T) {
		m := testModel(t, Deps{Source: sampleSource()})
		m.feeds[feedQuotas].pending = true
		if got := strings.Join(m.allowancesLines(80, 10), "\n"); !strings.Contains(got, "loading…") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("nothing reported", func(t *testing.T) {
		m := testModel(t, Deps{Source: sampleSource()})
		m.snap.Quotas = stampAt[[]magpie.Quota](testNow, nil)
		if got := strings.Join(m.allowancesLines(80, 10), "\n"); !strings.Contains(got, "press r to refresh") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("quota failure shows above the cards", func(t *testing.T) {
		src := sampleSource()
		m := testModel(t, Deps{Source: src})
		m.snap.Quotas = stampAt(testNow, src.quotas)
		m.feeds[feedQuotas] = feedState{err: "magpie quota: exit 1", fromCache: true}
		lines := m.allowancesLines(80, 10)
		if !strings.Contains(lines[0], "quotas: magpie quota: exit 1") {
			t.Errorf("first line = %q", lines[0])
		}
		if !strings.Contains(strings.Join(lines, "\n"), "Max 20x") {
			t.Error("the cached cards should still show")
		}
	})
}
