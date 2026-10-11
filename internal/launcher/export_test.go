package launcher

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

func TestOpenFocusesASessionAPaneHolds(t *testing.T) {
	f := &fakeRunner{}
	s := magpie.Session{ID: "AAAA-1111", Agent: "claude", CWD: "/nonexistent/x", Resume: testResume}
	agents := []herdr.Agent{{
		PaneID: "w1-2", WorkspaceID: "w1", CWD: "/nonexistent/x",
		Session: &herdr.AgentSession{Kind: "id", Value: "aaaa-1111"},
	}}
	notice, err := Open(t.Context(), herdrClient(f), s, agents, "w9", "/home/u")
	if err != nil || notice != "" {
		t.Fatalf("Open = %q, %v; want no notice and no error", notice, err)
	}
	checkCalls(t, f, [][]string{
		{"herdr", "workspace", "focus", "w1"},
		{"herdr", "agent", "focus", "w1-2"},
	})
}

func TestOpenResumesInTheWorkspaceOfItsDirectory(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{respond: func(args []string) (string, error) {
		if args[0] == "tab" {
			return tabCreated("w1-9"), nil
		}
		return "", nil
	}}
	s := magpie.Session{ID: "bbbb-2222", Agent: "claude", Title: "修复", CWD: dir, Resume: testResume}
	agents := []herdr.Agent{{PaneID: "w1-1", WorkspaceID: "w1", CWD: dir}}
	notice, err := Open(t.Context(), herdrClient(f), s, agents, "w9", "/home/u")
	if err != nil || notice != "" {
		t.Fatalf("Open = %q, %v; want no notice and no error", notice, err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("calls = %q, want a tab create and a pane run", f.calls)
	}
	create := f.calls[0]
	if !slices.Equal(create[:4], []string{"herdr", "tab", "create", "--workspace"}) || create[4] != "w1" {
		t.Errorf("tab create = %q, want it in workspace w1", create)
	}
	if !slices.Contains(create, "--label") || !slices.Contains(create, "claude: 修复") {
		t.Errorf("tab create = %q, want the launcher's label", create)
	}
	want := []string{"herdr", "pane", "run", "w1-9", testResume}
	if got := f.calls[1]; !slices.Equal(got[:4], want[:4]) || got[len(got)-1] != testResume {
		t.Errorf("pane run = %q, want %q", f.calls[1], want)
	}
}

func TestOpenRefusesASessionWithoutAResumeCommand(t *testing.T) {
	f := &fakeRunner{}
	_, err := Open(t.Context(), herdrClient(f), magpie.Session{ID: "c"}, nil, "w9", "/home/u")
	if !errors.Is(err, errNoResume) {
		t.Fatalf("err = %v, want errNoResume", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %q, want none", f.calls)
	}
}

func TestExportedListLogic(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	sessions := []magpie.Session{
		{ID: "old", Agent: "codex", Title: "旧会话", CWD: "/x/old", Last: now.Add(-2 * time.Hour)},
		{ID: "new", Agent: "claude", Title: "新会话", CWD: "/x/new", Last: now.Add(-time.Minute)},
	}
	items := BuildItems(sessions, []herdr.Agent{{PaneID: "w1-1", WorkspaceID: "w1", Session: &herdr.AgentSession{Kind: "id", Value: "OLD"}}})
	if got := items[0].Session().ID; got != "new" {
		t.Fatalf("newest first: got %q", got)
	}
	open := items[1].Open()
	if open == nil || open.PaneID() != "w1-1" || open.WorkspaceID() != "w1" {
		t.Fatalf("open pane of old = %+v", open)
	}
	if got := FilterItems(items, "codex 旧", "/home/u"); len(got) != 1 || got[0].Session().ID != "old" {
		t.Errorf("FilterItems matched %d rows", len(got))
	}
	if got := FilterItems(items, "", "/home/u"); len(got) != 2 {
		t.Errorf("an empty query keeps %d rows, want 2", len(got))
	}

	for _, w := range []int{20, 40, 80} {
		lay := NewLayout(w, items, "/home/u")
		for _, it := range items {
			if dw := DisplayWidth(lay.Row(it, now)); dw != w {
				t.Errorf("row at width %d is %d columns: %q", w, dw, lay.Row(it, now))
			}
		}
	}
	row := NewLayout(80, items, "/home/u").Row(items[1], now)
	if !strings.HasPrefix(row, "● ") {
		t.Errorf("an open row should start with the open mark: %q", row)
	}
}

func TestExportedTextHelpers(t *testing.T) {
	if got := Fit("中文字", 4); got != "中… " || DisplayWidth(got) != 4 {
		t.Errorf("Fit = %q", got)
	}
	if got := TailFit("/a/very/long/path/settings.json", 12); !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "json") || DisplayWidth(got) != 12 {
		t.Errorf("TailFit keeps the end at 12 columns: %q", got)
	}
	if got := ShortPath("/home/u/pp/x", "/home/u"); got != "~/pp/x" {
		t.Errorf("ShortPath = %q", got)
	}
	s := magpie.Session{Agent: "", Title: "  ", CWD: "/home/u/pp/app/"}
	if got := AgentName(s); got != "-" {
		t.Errorf("AgentName = %q", got)
	}
	if got := DisplayTitle(s, "/home/u"); got != "app" {
		t.Errorf("DisplayTitle = %q", got)
	}
	if got := TabLabel(magpie.Session{Agent: "omp", Title: "t"}, "/home/u"); got != "omp: t" {
		t.Errorf("TabLabel = %q", got)
	}
	if got := RelTime(time.Time{}, time.Time{}); got != "-" {
		t.Errorf("RelTime of zero = %q", got)
	}
}
