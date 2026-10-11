package panel

import (
	"errors"
	"strings"
	"testing"
)

// sessionsModel is the panel on the Sessions tab, over the fixtures, with the
// actions act and the pane view h.
func sessionsModel(t *testing.T, h *fakeHerdr, act *fakeActions, w, ht int) *Model {
	t.Helper()
	m := panelModel(t, samplePanelSource(), h, act, w, ht)
	press(m, "5")
	return m
}

// typeKeys presses each key in turn.
func typeKeys(m *Model, keys ...string) {
	for _, k := range keys {
		press(m, k)
	}
}

func TestSessionsHeaderCountsRowsAndOpenPanes(t *testing.T) {
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, nil, 120, 32)
	view := m.View()
	if !strings.Contains(view, "5 of 5 · 4 open") {
		t.Errorf("header lacks the counts:\n%s", view)
	}
	if !strings.Contains(view, "● ") {
		t.Errorf("open sessions should carry the open mark:\n%s", view)
	}
}

func TestSessionsFilterTakesDigitsAndQAsText(t *testing.T) {
	act := &fakeActions{}
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, act, 80, 24)
	typeKeys(m, "/", "2", "q")
	if m.tab != tabSessions {
		t.Errorf("tab = %v, want Sessions: a digit typed into the filter must not switch tabs", m.tab)
	}
	if !m.sess.filtering || m.sess.query != "2q" {
		t.Errorf("filtering = %v, query = %q; want the filter open with 2q", m.sess.filtering, m.sess.query)
	}
	if _, cmd := m.Update(keyMsg("q")); cmd != nil {
		t.Errorf("q typed into the filter returned %v, want no quit", cmd)
	}
	if got := m.sess.query; got != "2qq" {
		t.Errorf("query = %q after a second q, want 2qq", got)
	}
	if view := m.View(); !strings.Contains(view, "/2qq▏") {
		t.Errorf("the filter line should show the typed text with a caret:\n%s", view)
	}
	if !strings.Contains(m.hintLine(120), "typing filters") {
		t.Errorf("the hint should say that typing filters: %q", m.hintLine(120))
	}
}

func TestSessionsFilterNarrowsAndTakesChinese(t *testing.T) {
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, nil, 120, 32)
	typeKeys(m, "/")
	typeKeys(m, "c", "l", "a", "u", "d", "e")
	if rows := m.sessionRows(); len(rows) != 3 {
		t.Errorf("claude filter matched %d rows, want 3", len(rows))
	}
	typeKeys(m, "backspace", "backspace", "backspace", "backspace", "backspace", "backspace")
	typeKeys(m, "写", "文", "档")
	rows := m.sessionRows()
	if len(rows) != 1 || rows[0].Session().ID != "bbbb-claude-2" {
		t.Errorf("Chinese filter rows = %v", rows)
	}
}

func TestSessionsEscLeavesTheFilterAndKeepsIt(t *testing.T) {
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, nil, 80, 24)
	typeKeys(m, "/", "z", "z", "esc")
	if m.sess.filtering || m.sess.query != "zz" || m.tab != tabSessions {
		t.Errorf("after esc: filtering %v, query %q, tab %v", m.sess.filtering, m.sess.query, m.tab)
	}
	if view := m.View(); !strings.Contains(view, "filter: zz") || !strings.Contains(view, "no session matches") {
		t.Errorf("the kept filter should still apply:\n%s", view)
	}
	typeKeys(m, "1")
	if m.tab != tabAgents {
		t.Errorf("out of the filter, 1 should switch tabs; tab = %v", m.tab)
	}
}

func TestSessionsCtrlUClearsTheFilter(t *testing.T) {
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, nil, 80, 24)
	typeKeys(m, "/", "a", "b", "ctrl+u")
	if m.sess.query != "" || !m.sess.filtering {
		t.Errorf("query = %q, filtering = %v after ctrl+u", m.sess.query, m.sess.filtering)
	}
}

func TestSessionsArrowsMoveInTheFilter(t *testing.T) {
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, nil, 80, 24)
	typeKeys(m, "/", "down", "down")
	if m.sess.cursor != 2 {
		t.Errorf("cursor = %d, want 2", m.sess.cursor)
	}
}

func TestSessionsEnterAsksThenOpens(t *testing.T) {
	act := &fakeActions{}
	h := &fakeHerdr{panes: samplePanes()}
	m := sessionsModel(t, h, act, 120, 32)
	readsBefore := h.reads

	press(m, "enter")
	want := "Open claude: 修复登录? Enter/Esc"
	if m.ask == nil || m.ask.text != want {
		t.Fatalf("confirm line = %+v, want %q", m.ask, want)
	}
	if len(act.opened) != 0 {
		t.Fatal("opened before the confirm")
	}
	press(m, "enter")
	if len(act.opened) != 1 || act.opened[0].ID != "aaaa-claude-1" {
		t.Fatalf("opened = %+v", act.opened)
	}
	if m.noticeKind != noticeOK || m.notice != "focused claude: 修复登录" {
		t.Errorf("an open session should be focused: notice = %q (%v)", m.notice, m.noticeKind)
	}
	if h.reads <= readsBefore {
		t.Error("the pane list should be read again after an open")
	}
	if m.writing {
		t.Error("still writing after the result landed")
	}

	press(m, "down") // gggg: no pane holds it, so it is opened
	press(m, "enter")
	press(m, "enter")
	if len(act.opened) != 2 || act.opened[1].ID != "gggg-free-6" {
		t.Fatalf("opened = %+v", act.opened)
	}
	if m.notice != "opened claude: 没有窗格的会话" {
		t.Errorf("a closed session should be opened: notice = %q", m.notice)
	}
}

func TestSessionsEscCancelsTheOpen(t *testing.T) {
	act := &fakeActions{}
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, act, 80, 24)
	press(m, "enter")
	press(m, "esc")
	if m.ask != nil || len(act.opened) != 0 {
		t.Errorf("after esc: ask %+v, opened %v", m.ask, act.opened)
	}
}

func TestSessionsOpenNoticeAndFailureShowOnTheStatusLine(t *testing.T) {
	act := &fakeActions{notice: "project dir missing, opened in current workspace"}
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, act, 120, 32)
	press(m, "enter")
	press(m, "enter")
	if got := ansiFree(m.statusLine(120)); !strings.Contains(got, "project dir missing, opened in current workspace") {
		t.Errorf("status line = %q", got)
	}

	act = &fakeActions{err: errors.New("no such workspace")}
	m = sessionsModel(t, &fakeHerdr{panes: samplePanes()}, act, 120, 32)
	press(m, "enter")
	press(m, "enter")
	if m.noticeKind != noticeBad || m.notice != "open failed: no such workspace" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}
}

func TestSessionsRefuseAClosedSessionWhenThePanesAreUnknown(t *testing.T) {
	act := &fakeActions{}
	m := sessionsModel(t, &fakeHerdr{err: errors.New("herdr down")}, act, 120, 32)
	press(m, "down") // gggg: no pane holds it
	press(m, "enter")
	if m.ask != nil || len(act.opened) != 0 {
		t.Fatalf("a session no pane holds was offered with the pane list unknown: ask %+v", m.ask)
	}
	if m.notice != "herdr agents unavailable, so the session is not opened in a new tab" {
		t.Errorf("notice = %q", m.notice)
	}

	m = sessionsModel(t, nil, act, 120, 32)
	press(m, "enter")
	if m.ask != nil || m.notice != "herdr is not configured, so the session is not opened" {
		t.Errorf("without herdr: ask %+v, notice %q", m.ask, m.notice)
	}
}

func TestSessionsOpenWritesOnlyOnce(t *testing.T) {
	act := &fakeActions{}
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, act, 80, 24)
	m.writing = true
	press(m, "enter")
	if m.ask != nil {
		t.Error("enter during a write should not ask anything")
	}
	m.writing = false
	typeKeys(m, "/")
	m.writing = true
	press(m, "enter")
	if m.ask != nil || m.notice != "wait for the change in progress to finish" {
		t.Errorf("filter enter during a write: ask %+v, notice %q", m.ask, m.notice)
	}
	if len(act.opened) != 0 {
		t.Errorf("opened during a write: %v", act.opened)
	}
}

func TestSessionsEmptyStates(t *testing.T) {
	src := samplePanelSource()
	src.sessions = nil
	m := panelModel(t, src, &fakeHerdr{panes: samplePanes()}, nil, 80, 24)
	press(m, "5")
	if view := m.View(); !strings.Contains(view, "no sessions reported; press r to refresh") {
		t.Errorf("empty list:\n%s", view)
	}

	m = sessionsModel(t, &fakeHerdr{panes: samplePanes()}, nil, 80, 24)
	typeKeys(m, "/", "z", "z", "z")
	if view := m.View(); !strings.Contains(view, `no session matches "zzz"`) {
		t.Errorf("no match:\n%s", view)
	}
}

func TestSessionsFitEverySize(t *testing.T) {
	src := samplePanelSource()
	src.sessions[0].Title = "一个非常非常长的中文会话标题用来测试截断"
	src.sessions[1].CWD = "/home/u/很长的中文目录名/子目录/还有更多"
	for _, sz := range screenSizes {
		m := panelModel(t, src, &fakeHerdr{panes: samplePanes()}, nil, sz[0], sz[1])
		press(m, "5")
		lines := strings.Split(m.View(), "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines, want %d", sz[0], sz[1], len(lines), sz[1])
		}
		assertLines(t, lines, sz[0])
	}
}

func TestRowWindowKeepsTheSelectionOnScreen(t *testing.T) {
	tests := []struct {
		selected, n, room, start, end int
	}{
		{0, 10, 3, 0, 3},
		{9, 10, 3, 7, 10},
		{5, 10, 3, 3, 6},
		{2, 2, 5, 0, 2},
		{0, 0, 5, 0, 0},
		{0, 10, 0, 0, 0},
	}
	for _, tc := range tests {
		start, end := rowWindow(tc.selected, tc.n, tc.room)
		if start != tc.start || end != tc.end {
			t.Errorf("rowWindow(%d, %d, %d) = %d, %d; want %d, %d", tc.selected, tc.n, tc.room, start, end, tc.start, tc.end)
		}
	}
}

func TestSessionsCursorStaysInTheRows(t *testing.T) {
	m := sessionsModel(t, &fakeHerdr{panes: samplePanes()}, nil, 80, 24)
	for range 20 {
		press(m, "down")
	}
	if m.sess.cursor != len(m.sessionRows())-1 {
		t.Errorf("cursor = %d past the last row", m.sess.cursor)
	}
	typeKeys(m, "/", "z")
	if m.sess.cursor != 0 {
		t.Errorf("a new filter should start at the top, cursor = %d", m.sess.cursor)
	}
}

func TestSessionsHeaderWithNoFilterHintsAtIt(t *testing.T) {
	m := sessionsModel(t, nil, nil, 80, 24)
	if got := ansiFree(m.sessionsHeader(80, 5, 5, 0)); !strings.Contains(got, "press / to filter") {
		t.Errorf("header = %q", got)
	}
}
