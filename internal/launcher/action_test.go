package launcher

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// tabCreated is herdr's answer to tab create, naming root as the new tab's pane.
func tabCreated(root string) string {
	return fmt.Sprintf(`{"id":"t","result":{"type":"tab_created","root_pane":{"pane_id":%q}}}`, root)
}

// workspaceCreated is herdr's answer to workspace create, naming ws and its root pane root.
func workspaceCreated(ws, root string) string {
	return fmt.Sprintf(`{"id":"t","result":{"type":"workspace_created","workspace":{"workspace_id":%q},"root_pane":{"pane_id":%q}}}`, ws, root)
}

// dirs is a directory check that knows only the directories named.
func dirs(names ...string) func(string) bool {
	return func(dir string) bool { return slices.Contains(names, dir) }
}

// checkCalls fails unless the fake saw exactly the argv lists in want, in order.
func checkCalls(t *testing.T, f *fakeRunner, want [][]string) {
	t.Helper()
	if len(f.calls) != len(want) {
		t.Fatalf("calls = %q\nwant   %q", f.calls, want)
	}
	for i := range want {
		if !slices.Equal(f.calls[i], want[i]) {
			t.Fatalf("call %d = %q, want %q", i, f.calls[i], want[i])
		}
	}
}

func herdrClient(f *fakeRunner) *herdr.Client {
	return &herdr.Client{Bin: "herdr", Runner: f}
}

func TestResumeOpenSession(t *testing.T) {
	tests := []struct {
		name string
		open openPane
		want [][]string
	}{
		{
			name: "focuses the workspace, then the pane",
			open: openPane{paneID: "w1-2", workspaceID: "w1"},
			want: [][]string{
				{"herdr", "workspace", "focus", "w1"},
				{"herdr", "agent", "focus", "w1-2"},
			},
		},
		{
			name: "without a workspace id only the pane is focused",
			open: openPane{paneID: "w1-2"},
			want: [][]string{{"herdr", "agent", "focus", "w1-2"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{}
			it := item{session: magpie.Session{Resume: "unused"}, open: new(tc.open)}
			if _, err := resume(t.Context(), herdrClient(f), it, nil, "w9", "/home/u", dirs()); err != nil {
				t.Fatal(err)
			}
			checkCalls(t, f, tc.want)
		})
	}
}

func TestResumeOpenSessionStopsAtFailedFocus(t *testing.T) {
	f := &fakeRunner{respond: func(args []string) (string, error) {
		if args[0] == "workspace" {
			return "", errors.New("no such workspace")
		}
		return "", nil
	}}
	it := item{session: magpie.Session{Resume: "unused"}, open: new(openPane{paneID: "w1-2", workspaceID: "w1"})}
	_, err := resume(t.Context(), herdrClient(f), it, nil, "w9", "/home/u", dirs())
	if err == nil || !strings.Contains(err.Error(), "no such workspace") {
		t.Fatalf("err = %v, want the workspace error", err)
	}
	checkCalls(t, f, [][]string{{"herdr", "workspace", "focus", "w1"}})
}

const testResume = "cd '/home/u/pp/app' && claude --resume 0a1b2c3d-1111-4222-8333-444455556666"

func TestResumeNewSession(t *testing.T) {
	const label = "claude: 修复登录"
	session := magpie.Session{Agent: "claude", Title: "修复登录", CWD: "~/pp/app/", Resume: testResume}
	tests := []struct {
		name    string
		agents  []herdr.Agent
		current string
		create  []string // the call that makes the pane, after "herdr"
		ws      string   // workspace of the new pane
		pane    string   // root pane of the new pane
	}{
		{
			name:   "workspace of the pane in the session directory",
			agents: []herdr.Agent{{CWD: "/home/u/pp/app", WorkspaceID: "w2"}},
			create: []string{"tab", "create", "--workspace", "w2", "--cwd", "/home/u/pp/app", "--label", label, "--focus"},
			ws:     "w2",
			pane:   "w2-7",
		},
		{
			name:    "current workspace wins among matches",
			agents:  []herdr.Agent{{CWD: "/home/u/pp/app/", WorkspaceID: "w2"}, {CWD: "/home/u/pp/app", WorkspaceID: "w9"}},
			current: "w9",
			create:  []string{"tab", "create", "--workspace", "w9", "--cwd", "/home/u/pp/app", "--label", label, "--focus"},
			ws:      "w9",
			pane:    "w9-7",
		},
		{
			name:    "no pane in the session directory opens a new workspace",
			agents:  []herdr.Agent{{CWD: "/elsewhere", WorkspaceID: "w2"}},
			current: "w9",
			create:  []string{"workspace", "create", "--cwd", "/home/u/pp/app", "--focus"},
			ws:      "w5",
			pane:    "w5-1",
		},
		{
			name:   "no current workspace also opens a new workspace",
			create: []string{"workspace", "create", "--cwd", "/home/u/pp/app", "--focus"},
			ws:     "w5",
			pane:   "w5-1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{respond: func(args []string) (string, error) {
				switch args[0] {
				case "tab":
					return tabCreated(tc.pane), nil
				case "workspace":
					return workspaceCreated(tc.ws, tc.pane), nil
				}
				return "", nil
			}}
			where, err := resume(t.Context(), herdrClient(f), item{session: session}, tc.agents, tc.current, "/home/u", dirs("/home/u/pp/app"))
			if err != nil {
				t.Fatal(err)
			}
			checkCalls(t, f, [][]string{
				append([]string{"herdr"}, tc.create...),
				{"herdr", "pane", "run", tc.pane, testResume},
			})
			if want := (placement{open: openPane{paneID: tc.pane, workspaceID: tc.ws}}); where != want {
				t.Errorf("placement = %+v, want %+v", where, want)
			}
		})
	}
}

func TestResumeMissingDirectory(t *testing.T) {
	// The pane in the missing directory must not pull the session into its workspace.
	agents := []herdr.Agent{{CWD: "/home/u/pp/gone", WorkspaceID: "w2"}}
	const label = "claude: 修复登录"
	tests := []struct {
		name    string
		cwd     string
		current string
		create  []string // the call that makes the pane, after "herdr"
		ws      string   // workspace of the new pane
	}{
		{
			name:    "missing directory opens a tab in the current workspace",
			cwd:     "/home/u/pp/gone",
			current: "w9",
			create:  []string{"tab", "create", "--workspace", "w9", "--label", label, "--focus"},
			ws:      "w9",
		},
		{
			name:    "empty directory opens a tab in the current workspace",
			current: "w9",
			create:  []string{"tab", "create", "--workspace", "w9", "--label", label, "--focus"},
			ws:      "w9",
		},
		{
			name:   "no current workspace leaves the tab to herdr",
			cwd:    "/home/u/pp/gone",
			create: []string{"tab", "create", "--label", label, "--focus"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			session := magpie.Session{Agent: "claude", Title: "修复登录", CWD: tc.cwd, Resume: testResume}
			f := &fakeRunner{respond: func(args []string) (string, error) {
				if args[0] == "tab" {
					return tabCreated("w9-7"), nil
				}
				return "", nil
			}}
			where, err := resume(t.Context(), herdrClient(f), item{session: session}, agents, tc.current, "/home/u", dirs("/home/u/pp/app"))
			if err != nil {
				t.Fatal(err)
			}
			checkCalls(t, f, [][]string{
				append([]string{"herdr"}, tc.create...),
				{"herdr", "pane", "run", "w9-7", testResume},
			})
			want := placement{open: openPane{paneID: "w9-7", workspaceID: tc.ws}, notice: noticeMissingDir}
			if where != want {
				t.Errorf("placement = %+v, want %+v", where, want)
			}
		})
	}
}

func TestResumeNewSessionErrors(t *testing.T) {
	session := magpie.Session{Agent: "claude", CWD: "/home/u/pp/app", Resume: testResume}
	matching := []herdr.Agent{{CWD: "/home/u/pp/app", WorkspaceID: "w2"}}
	tests := []struct {
		name      string
		session   magpie.Session
		agents    []herdr.Agent
		respond   func(args []string) (string, error)
		wantErr   string
		wantCalls int
	}{
		{
			name:    "no resume command runs nothing",
			session: magpie.Session{Agent: "claude", CWD: "/home/u/pp/app"},
			wantErr: "no resume command",
		},
		{
			name:    "failed tab create runs nothing in a pane",
			session: session,
			agents:  matching,
			respond: func(args []string) (string, error) {
				if args[0] == "tab" {
					return "", errors.New("boom")
				}
				return "", nil
			},
			wantErr:   "boom",
			wantCalls: 1,
		},
		{
			name:    "failed workspace create runs nothing in a pane",
			session: session,
			respond: func(args []string) (string, error) {
				if args[0] == "workspace" {
					return "", errors.New("boom")
				}
				return "", nil
			},
			wantErr:   "boom",
			wantCalls: 1,
		},
		{
			name:    "workspace create without a root pane runs nothing in a pane",
			session: session,
			respond: func(args []string) (string, error) {
				return `{"id":"t","result":{"type":"workspace_created","workspace":{"workspace_id":"w5"}}}`, nil
			},
			wantErr:   "root_pane.pane_id",
			wantCalls: 1,
		},
		{
			name:    "failed pane run names the pane",
			session: session,
			respond: func(args []string) (string, error) {
				switch args[0] {
				case "pane":
					return "", errors.New("boom")
				case "workspace":
					return workspaceCreated("w5", "w5-1"), nil
				}
				return "", nil
			},
			wantErr:   "resume in new pane w5-1",
			wantCalls: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{respond: tc.respond}
			_, err := resume(t.Context(), herdrClient(f), item{session: tc.session}, tc.agents, "", "/home/u", dirs("/home/u/pp/app"))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
			if len(f.calls) != tc.wantCalls {
				t.Errorf("herdr calls = %d, want %d: %q", len(f.calls), tc.wantCalls, f.calls)
			}
		})
	}
	if _, err := resume(t.Context(), herdrClient(&fakeRunner{}), item{session: magpie.Session{}}, nil, "", "", dirs()); !errors.Is(err, errNoResume) {
		t.Errorf("empty resume: err = %v, want errNoResume", err)
	}
}

func TestChooseTarget(t *testing.T) {
	agents := []herdr.Agent{
		{CWD: "/home/u/pp/app/", WorkspaceID: "w2"},
		{CWD: "~/pp/other", WorkspaceID: "w3"},
		{CWD: "/home/u/pp/app", WorkspaceID: "w4"},
		{CWD: "/home/u/pp/app", WorkspaceID: ""}, // no workspace: never chosen
	}
	exists := dirs("/home/u/pp/app", "/home/u/pp/other", "/home/u/pp/new")
	tests := []struct {
		name    string
		cwd     string
		current string
		want    target
	}{
		{name: "tilde and trailing slash match a pane", cwd: "~/pp/app", current: "w9", want: target{workspace: "w2", dir: "/home/u/pp/app"}},
		{name: "current wins when it matches", cwd: "/home/u/pp/app/", current: "w4", want: target{workspace: "w4", dir: "/home/u/pp/app"}},
		{name: "tilde in the pane's cwd matches too", cwd: "/home/u/pp/other", current: "w9", want: target{workspace: "w3", dir: "/home/u/pp/other"}},
		{name: "first match when current is not one", cwd: "/home/u/pp/app", current: "", want: target{workspace: "w2", dir: "/home/u/pp/app"}},
		{name: "no pane in the directory opens a new workspace", cwd: "/home/u/pp/new", current: "w9", want: target{newWorkspace: true, dir: "/home/u/pp/new"}},
		{name: "no current workspace still opens a new workspace", cwd: "/home/u/pp/new", want: target{newWorkspace: true, dir: "/home/u/pp/new"}},
		{name: "empty directory keeps the current workspace", cwd: "", current: "w9", want: target{workspace: "w9", notice: noticeMissingDir}},
		{name: "missing directory keeps the current workspace", cwd: "/nowhere", current: "w9", want: target{workspace: "w9", notice: noticeMissingDir}},
		{name: "missing directory with no current workspace", cwd: "/nowhere", want: target{notice: noticeMissingDir}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chooseTarget(agents, tc.cwd, tc.current, "/home/u", exists); got != tc.want {
				t.Errorf("chooseTarget = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTabLabel(t *testing.T) {
	tests := []struct {
		name    string
		session magpie.Session
		want    string
	}{
		{
			name:    "title cut to 30 columns",
			session: magpie.Session{Agent: "claude", Title: strings.Repeat("中", 40)},
			want:    "claude: " + strings.Repeat("中", 15),
		},
		{
			name:    "no trailing space after the cut",
			session: magpie.Session{Agent: "claude", Title: strings.Repeat("a", 29) + " b"},
			want:    "claude: " + strings.Repeat("a", 29),
		},
		{
			name:    "no title uses the directory",
			session: magpie.Session{Agent: "codex", CWD: "/srv/app/"},
			want:    "codex: app",
		},
		{
			name:    "no agent",
			session: magpie.Session{Title: "fix"},
			want:    "session: fix",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tabLabel(tc.session, "/home/u"); got != tc.want {
				t.Errorf("tabLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeRunner records every call's argv and answers through respond. It never
// runs a command, so write commands can be checked safely.
type fakeRunner struct {
	calls   [][]string
	respond func(args []string) (string, error) // nil answers "" with no error
}

// Run implements herdr.Runner and magpie.Runner.
func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.respond == nil {
		return nil, nil
	}
	out, err := f.respond(args)
	return []byte(out), err
}
