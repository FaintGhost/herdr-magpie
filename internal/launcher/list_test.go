package launcher

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// testNow is the clock every launcher test uses.
var testNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

func TestBuildItems(t *testing.T) {
	const (
		uuidA = "0a1b2c3d-1111-4222-8333-444455556666"
		uuidB = "9f8e7d6c-aaaa-4bbb-8ccc-ddddeeeeffff"
	)
	agents := []herdr.Agent{
		{PaneID: "p1", WorkspaceID: "w1", Session: &herdr.AgentSession{Kind: "id", Value: strings.ToUpper(uuidA)}},
		{PaneID: "p2", WorkspaceID: "w2", Session: &herdr.AgentSession{Kind: "path", Value: "/home/u/.omp/sessions/2026-10-12_" + uuidB + ".jsonl"}},
		{PaneID: "p3", WorkspaceID: "w3", Session: &herdr.AgentSession{Kind: "id", Value: uuidA}}, // second pane for uuidA
		{PaneID: "p4", WorkspaceID: "w4"}, // no session
	}
	sessions := []magpie.Session{
		{Agent: "claude", ID: uuidA, Last: testNow.Add(-time.Hour)},
		{Agent: "omp", ID: uuidB, Last: testNow},
		{Agent: "codex", ID: "cccc", Last: testNow.Add(-2 * time.Hour)},
		{Agent: "hermes", ID: "", Last: testNow.Add(-3 * time.Hour)},
		{Agent: "omp", ID: "dddd", Last: testNow.Add(-time.Hour)}, // ties with uuidA; agent breaks the tie
	}
	items := buildItems(sessions, agents)

	gotIDs := make([]string, 0, len(items))
	for _, it := range items {
		gotIDs = append(gotIDs, it.session.ID)
	}
	wantIDs := []string{uuidB, uuidA, "dddd", "cccc", ""}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("order = %q, want %q", gotIDs, wantIDs)
	}
	wantPane := map[string]openPane{
		uuidB: {paneID: "p2", workspaceID: "w2"},
		uuidA: {paneID: "p1", workspaceID: "w1"},
	}
	for _, it := range items {
		want, open := wantPane[it.session.ID]
		switch {
		case !open && it.open != nil:
			t.Errorf("%q: open = %+v, want closed", it.session.ID, *it.open)
		case open && (it.open == nil || *it.open != want):
			t.Errorf("%q: open = %+v, want %+v", it.session.ID, it.open, want)
		}
	}
}

func TestFilterItems(t *testing.T) {
	items := []item{
		{session: magpie.Session{Agent: "claude", Title: "修复登录页面", CWD: "/root/pp/demo-app"}},
		{session: magpie.Session{Agent: "omp", CWD: "/home/u/Work/Notes"}},
		{session: magpie.Session{Agent: "codex", Title: "Refactor Parser", CWD: "/srv/app"}},
	}
	tests := []struct {
		name   string
		query  string
		agents []string // agents of the matching items, in order
	}{
		{name: "empty query keeps all", query: "", agents: []string{"claude", "omp", "codex"}},
		{name: "blank query keeps all", query: "  \t ", agents: []string{"claude", "omp", "codex"}},
		{name: "agent, any case", query: "CLAUDE", agents: []string{"claude"}},
		{name: "title, Chinese", query: "登录", agents: []string{"claude"}},
		{name: "Chinese characters are separate terms", query: "登 录", agents: []string{"claude"}},
		{name: "title, mixed case", query: "parser", agents: []string{"codex"}},
		{name: "several terms all match", query: "demo app", agents: []string{"claude"}},
		{name: "one unmatched term excludes", query: "demo zzz", agents: nil},
		{name: "terms may match different fields", query: "omp codex", agents: nil},
		{name: "directory, as stored", query: "/srv", agents: []string{"codex"}},
		{name: "directory, with ~ for home", query: "~/work", agents: []string{"omp"}},
		{name: "directory name", query: "notes", agents: []string{"omp"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, it := range filterItems(items, tc.query, "/home/u") {
				got = append(got, it.session.Agent)
			}
			if !slices.Equal(got, tc.agents) {
				t.Errorf("agents = %q, want %q", got, tc.agents)
			}
		})
	}
}

func TestModelMovesAndScrolls(t *testing.T) {
	var sessions []magpie.Session
	for i := range 10 {
		sessions = append(sessions, magpie.Session{Agent: "a", ID: fmt.Sprint(i), Last: testNow.Add(-time.Duration(i) * time.Minute)})
	}
	m := newModel("/home/u", 14)
	m.resize(80, 5) // three list rows
	m.setLoaded(sessions, nil, true)

	steps := []struct {
		name   string
		key    key
		cursor int
		top    int
	}{
		{name: "start", cursor: 0, top: 0},
		{name: "down", key: key{kind: keyDown}, cursor: 1, top: 0},
		{name: "down again", key: key{kind: keyDown}, cursor: 2, top: 0},
		{name: "down past the window", key: key{kind: keyDown}, cursor: 3, top: 1},
		{name: "page down", key: key{kind: keyPageDown}, cursor: 6, top: 4},
		{name: "page down to the end", key: key{kind: keyPageDown}, cursor: 9, top: 7},
		{name: "down at the end stays", key: key{kind: keyDown}, cursor: 9, top: 7},
		{name: "page up", key: key{kind: keyPageUp}, cursor: 6, top: 6},
		{name: "up to the top", key: key{kind: keyUp}, cursor: 5, top: 5},
	}
	for _, s := range steps[1:] {
		m.press(s.key)
		if m.cursor != s.cursor || m.top != s.top {
			t.Fatalf("%s: cursor, top = %d, %d; want %d, %d", s.name, m.cursor, m.top, s.cursor, s.top)
		}
	}
	for range 10 {
		m.press(key{kind: keyUp})
	}
	if m.cursor != 0 || m.top != 0 {
		t.Errorf("after moving up past the start: cursor, top = %d, %d; want 0, 0", m.cursor, m.top)
	}
}

func TestModelFilterEditing(t *testing.T) {
	m := newModel("/home/u", 14)
	m.resize(80, 10)
	m.setLoaded([]magpie.Session{{Agent: "claude", Title: "中文测试"}, {Agent: "omp", Title: "other"}}, nil, true)

	for _, r := range "中文" {
		m.press(key{kind: keyRune, r: r})
	}
	if m.query != "中文" || len(m.shown) != 1 {
		t.Fatalf("after typing 中文: query = %q, shown = %d; want 中文 and 1", m.query, len(m.shown))
	}
	m.press(key{kind: keyBackspace})
	if m.query != "中" {
		t.Errorf("after one backspace: query = %q, want 中", m.query)
	}
	m.press(key{kind: keyBackspace})
	m.press(key{kind: keyBackspace}) // on an empty query
	if m.query != "" || len(m.shown) != 2 {
		t.Errorf("after clearing: query = %q, shown = %d; want empty and 2", m.query, len(m.shown))
	}
}

func TestModelActions(t *testing.T) {
	tests := []struct {
		name string
		key  key
		want action
	}{
		{name: "Esc quits", key: key{kind: keyEsc}, want: actQuit},
		{name: "Ctrl-C quits", key: key{kind: keyCtrlC}, want: actQuit},
		{name: "Enter picks", key: key{kind: keyEnter}, want: actPick},
		{name: "a letter edits the filter", key: key{kind: keyRune, r: 'a'}, want: actNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel("/home/u", 14)
			m.resize(80, 10)
			if got := m.press(tc.key); got != tc.want {
				t.Errorf("action = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestModelNoticeLastsUntilNextKey(t *testing.T) {
	m := newModel("/home/u", 14)
	m.resize(80, 10)
	m.message = "boom"
	m.press(key{kind: keyEnter})
	if m.message != "boom" {
		t.Fatalf("Enter cleared the notice: %q", m.message)
	}
	m.press(key{kind: keyDown})
	if m.message != "" {
		t.Errorf("notice after a key = %q, want empty", m.message)
	}
}
