package panelsrc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/panel"
)

// fakeHerdr answers herdr calls by their first two words ("agent get", "pane
// close", ...) with canned stdout or an error, and fails a call it has no answer
// for. It records every call as the argument list, joined with spaces.
type fakeHerdr struct {
	out   map[string]string
	errs  map[string]error
	calls []string
}

func (f *fakeHerdr) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	key := strings.Join(args[:min(2, len(args))], " ")
	if err := f.errs[key]; err != nil {
		return nil, err
	}
	out, ok := f.out[key]
	if !ok {
		return nil, errors.New("unexpected herdr call: " + key)
	}
	return []byte(out), nil
}

// newFakeHerdr returns a Herdr client over f.
func newFakeHerdr(f *fakeHerdr) *herdr.Client {
	return &herdr.Client{Bin: "herdr", Runner: f}
}

// idlePaneAnswers answers the calls of a restart: the pane is agent in status,
// closing it works, and the new tab's root pane is w1-9.
func idlePaneAnswers(agent, status string) map[string]string {
	return map[string]string{
		"agent get":        `{"id":"t","result":{"agent":{"agent":"` + agent + `","agent_status":"` + status + `","pane_id":"w1-2","workspace_id":"w1","cwd":"/srv/demo"}}}`,
		"pane close":       `{"id":"t","result":{"type":"ok"}}`,
		"tab create":       `{"id":"t","result":{"type":"tab_created","tab":{"tab_id":"w1-t9"},"root_pane":{"pane_id":"w1-9"}}}`,
		"pane run":         `{"id":"t","result":{"type":"ok"}}`,
		"agent list":       `{"id":"t","result":{"agents":[]}}`,
		"workspace create": `{"id":"t","result":{"type":"workspace_created","workspace":{"workspace_id":"w5"},"root_pane":{"pane_id":"w5-1"}}}`,
	}
}

func restartTarget() panel.RestartTarget {
	return panel.RestartTarget{
		PaneID:      "w1-2",
		WorkspaceID: "w1",
		CWD:         "/srv/demo",
		Agent:       "claude",
		Resume:      "cd '/srv/demo' && claude --resume abc",
		Label:       "claude: demo",
	}
}

func TestRestartPaneClosesAndResumesAnIdlePane(t *testing.T) {
	for _, status := range []string{"idle", "done"} {
		t.Run(status, func(t *testing.T) {
			f := &fakeHerdr{out: idlePaneAnswers("claude", status)}
			a := Actions{Herdr: newFakeHerdr(f)}
			if err := a.RestartPane(t.Context(), restartTarget()); err != nil {
				t.Fatal(err)
			}
			want := []string{
				"agent get w1-2",
				"pane close w1-2",
				"tab create --workspace w1 --cwd /srv/demo --label claude: demo --no-focus",
				"pane run w1-9 cd '/srv/demo' && claude --resume abc",
			}
			if !slices.Equal(f.calls, want) {
				t.Errorf("herdr calls:\n%q\nwant\n%q", f.calls, want)
			}
		})
	}
}

func TestRestartPaneRefusesAPaneThatIsNotIdle(t *testing.T) {
	for _, tc := range []struct {
		name, agent, status string
	}{
		{name: "working", agent: "claude", status: "working"},
		{name: "blocked", agent: "claude", status: "blocked"},
		{name: "unknown", agent: "claude", status: "unknown"},
		{name: "another agent", agent: "codex", status: "idle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeHerdr{out: idlePaneAnswers(tc.agent, tc.status)}
			err := (Actions{Herdr: newFakeHerdr(f)}).RestartPane(t.Context(), restartTarget())
			if err == nil {
				t.Fatal("RestartPane restarted a pane that is not idle")
			}
			if want := []string{"agent get w1-2"}; !slices.Equal(f.calls, want) {
				t.Errorf("herdr calls = %q, want only the re-read %q", f.calls, want)
			}
		})
	}
}

func TestRestartPaneStopsAtTheFailingStep(t *testing.T) {
	tests := []struct {
		name      string
		failKey   string
		wantCalls []string // the calls made, the last of which fails
		wantStep  string   // a word the error must carry
	}{
		{
			name:      "check",
			failKey:   "agent get",
			wantCalls: []string{"agent get w1-2"},
			wantStep:  "check",
		},
		{
			name:      "close",
			failKey:   "pane close",
			wantCalls: []string{"agent get w1-2", "pane close w1-2"},
			wantStep:  "close",
		},
		{
			name:      "create tab",
			failKey:   "tab create",
			wantCalls: []string{"agent get w1-2", "pane close w1-2", "tab create --workspace w1 --cwd /srv/demo --label claude: demo --no-focus"},
			wantStep:  "create tab",
		},
		{
			name:      "resume",
			failKey:   "pane run",
			wantCalls: []string{"agent get w1-2", "pane close w1-2", "tab create --workspace w1 --cwd /srv/demo --label claude: demo --no-focus", "pane run w1-9 cd '/srv/demo' && claude --resume abc"},
			wantStep:  "resume",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeHerdr{
				out:  idlePaneAnswers("claude", "idle"),
				errs: map[string]error{tc.failKey: errors.New("boom")},
			}
			err := (Actions{Herdr: newFakeHerdr(f)}).RestartPane(t.Context(), restartTarget())
			if err == nil || !strings.Contains(err.Error(), tc.wantStep) || !strings.Contains(err.Error(), "boom") {
				t.Fatalf("err = %v, want the %q step and the cause", err, tc.wantStep)
			}
			if !slices.Equal(f.calls, tc.wantCalls) {
				t.Errorf("herdr calls = %q, want %q", f.calls, tc.wantCalls)
			}
		})
	}
}

func TestRestartPaneNeedsAResumeCommandAndHerdr(t *testing.T) {
	f := &fakeHerdr{out: idlePaneAnswers("claude", "idle")}
	target := restartTarget()
	target.Resume = "  "
	if err := (Actions{Herdr: newFakeHerdr(f)}).RestartPane(t.Context(), target); err == nil {
		t.Error("RestartPane without a resume command succeeded")
	}
	if len(f.calls) != 0 {
		t.Errorf("herdr was called without a resume command: %q", f.calls)
	}
	if err := (Actions{}).RestartPane(t.Context(), restartTarget()); !errors.Is(err, errNoHerdr) {
		t.Errorf("RestartPane without Herdr = %v, want errNoHerdr", err)
	}
}

func TestMagpieWritesRunTheCommands(t *testing.T) {
	f := &fakeMagpie{out: map[string]string{
		"/opt/magpie use example":                                "",
		"/opt/magpie save example":                               "",
		"/opt/magpie accounts switch claude person@example.test": "",
	}}
	a := Actions{Magpie: f, Bin: "/opt/magpie"}
	ctx := t.Context()
	if err := a.UseProfile(ctx, "example"); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveProfile(ctx, "example"); err != nil {
		t.Fatal(err)
	}
	if err := a.SwitchAccount(ctx, "claude", "person@example.test"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"/opt/magpie use example",
		"/opt/magpie save example",
		"/opt/magpie accounts switch claude person@example.test",
	}
	if !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

func TestOpenSessionFocusesAnOpenPane(t *testing.T) {
	f := &fakeHerdr{out: map[string]string{
		"agent list":      `{"id":"t","result":{"agents":[{"agent":"claude","agent_status":"idle","pane_id":"w2-1","workspace_id":"w2","cwd":"/srv/demo","agent_session":{"agent":"claude","kind":"id","source":"x","value":"s1"}}]}}`,
		"workspace focus": `{"id":"t","result":{"type":"ok"}}`,
		"agent focus":     `{"id":"t","result":{"type":"ok"}}`,
	}}
	s := magpie.Session{Agent: "claude", ID: "s1", CWD: "/srv/demo", Resume: "claude --resume s1"}
	notice, err := (Actions{Herdr: newFakeHerdr(f)}).OpenSession(t.Context(), s)
	if err != nil || notice != "" {
		t.Fatalf("OpenSession() = %q, %v", notice, err)
	}
	want := []string{"agent list", "workspace focus w2", "agent focus w2-1"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("herdr calls = %q, want %q", f.calls, want)
	}
}

func TestOpenSessionResumesInANewWorkspace(t *testing.T) {
	dir := t.TempDir()
	f := &fakeHerdr{out: idlePaneAnswers("claude", "idle")}
	s := magpie.Session{Agent: "claude", ID: "s1", CWD: dir, Resume: "claude --resume s1"}
	notice, err := (Actions{Herdr: newFakeHerdr(f)}).OpenSession(t.Context(), s)
	if err != nil || notice != "" {
		t.Fatalf("OpenSession() = %q, %v", notice, err)
	}
	want := []string{"agent list", "workspace create --cwd " + dir + " --focus", "pane run w5-1 claude --resume s1"}
	if !slices.Equal(f.calls, want) {
		t.Errorf("herdr calls = %q, want %q", f.calls, want)
	}
}

func TestOpenSessionRefusesWithoutAgentList(t *testing.T) {
	f := &fakeHerdr{errs: map[string]error{"agent list": errors.New("socket gone")}}
	s := magpie.Session{Agent: "claude", ID: "s1", CWD: t.TempDir(), Resume: "claude --resume s1"}
	_, err := (Actions{Herdr: newFakeHerdr(f)}).OpenSession(t.Context(), s)
	if err == nil || !strings.Contains(err.Error(), "read agent panes") {
		t.Fatalf("err = %v, want the agent read to fail", err)
	}
	if want := []string{"agent list"}; !slices.Equal(f.calls, want) {
		t.Errorf("herdr calls = %q, want only the agent read", f.calls)
	}
	if _, err := (Actions{}).OpenSession(t.Context(), s); !errors.Is(err, errNoHerdr) {
		t.Errorf("OpenSession without Herdr = %v, want errNoHerdr", err)
	}
}
