package launcher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

func TestRunValidatesDeps(t *testing.T) {
	if err := Run(t.Context(), Deps{}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("Run with empty Deps: err = %v, want a required-field error", err)
	}
}

func TestRunNeedsATerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	d := Deps{Herdr: herdrClient(&fakeRunner{}), In: r, Out: io.Discard}
	if err := Run(t.Context(), d); err == nil || !strings.Contains(err.Error(), "not a terminal") {
		t.Errorf("Run on a pipe: err = %v, want a not-a-terminal error", err)
	}
}

// startAndCollect runs startLoads and returns both results, failing the test if
// they do not arrive promptly.
func startAndCollect(t *testing.T, d Deps) (sessionsLoaded, agentsLoaded) {
	t.Helper()
	out := make(chan any, 2)
	startLoads(t.Context(), d, out)
	var s sessionsLoaded
	var a agentsLoaded
	for range 2 {
		select {
		case v := <-out:
			switch v := v.(type) {
			case sessionsLoaded:
				s = v
			case agentsLoaded:
				a = v
			default:
				t.Fatalf("unexpected message %T", v)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("loads did not finish")
		}
	}
	return s, a
}

func TestStartLoadsJoinsSessionsWithPanes(t *testing.T) {
	const uuid = "0a1b2c3d-1111-4222-8333-444455556666"
	last := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	sessionsJSON := fmt.Sprintf(
		`[{"agent":"claude","id":%q,"cwd":"/srv/app","title":"fix","last":%q,"resume":"cd '/srv/app' && claude --resume %s"}]`,
		uuid, last, uuid)
	agentsJSON := fmt.Sprintf(
		`{"result":{"agents":[{"agent":"claude","agent_session":{"agent":"claude","kind":"id","source":"test","value":%q},`+
			`"pane_id":"w1-2","workspace_id":"w1","cwd":"/srv/app","terminal_title_stripped":"fix","agent_status":"idle","focused":false}]}}`,
		strings.ToUpper(uuid))

	magpieFake := &fakeRunner{respond: func([]string) (string, error) { return sessionsJSON, nil }}
	herdrFake := &fakeRunner{respond: func([]string) (string, error) { return agentsJSON, nil }}
	d := Deps{
		MagpieBin: "magpie-test",
		Magpie:    magpieFake,
		Herdr:     &herdr.Client{Bin: "herdr-test", Runner: herdrFake},
		Days:      14,
	}
	s, a := startAndCollect(t, d)
	if s.err != nil || a.err != nil {
		t.Fatalf("errors: sessions = %v, agents = %v", s.err, a.err)
	}
	items := buildItems(s.sessions, a.agents)
	if len(items) != 1 || items[0].open == nil || items[0].open.paneID != "w1-2" {
		t.Fatalf("items = %+v, want one session open in pane w1-2", items)
	}

	wantMagpie := []string{"magpie-test", "sessions", "--json", "--limit", "100000"}
	if len(magpieFake.calls) != 1 || !slices.Equal(magpieFake.calls[0], wantMagpie) {
		t.Errorf("magpie calls = %q, want [%q]", magpieFake.calls, wantMagpie)
	}
	wantHerdr := []string{"herdr-test", "agent", "list"}
	if len(herdrFake.calls) != 1 || !slices.Equal(herdrFake.calls[0], wantHerdr) {
		t.Errorf("herdr calls = %q, want [%q]", herdrFake.calls, wantHerdr)
	}
}

func TestStartLoadsTurnsPanicIntoError(t *testing.T) {
	d := Deps{
		Magpie: &fakeRunner{respond: func([]string) (string, error) { panic("synthetic") }},
		Herdr: &herdr.Client{Runner: &fakeRunner{respond: func([]string) (string, error) {
			return `{"result":{"agents":[]}}`, nil
		}}},
	}
	s, a := startAndCollect(t, d)
	if s.err == nil || !strings.Contains(s.err.Error(), "panic") {
		t.Errorf("sessions err = %v, want a panic error", s.err)
	}
	if a.err != nil {
		t.Errorf("agents err = %v, want nil", a.err)
	}
}

// pickFixture returns a launcher with session s loaded and selected. Herdr answers
// every tab create with pane w9-7 and every other call with nothing.
func pickFixture(t *testing.T, s magpie.Session, agents []herdr.Agent, current string) (*app, *fakeRunner) {
	t.Helper()
	f := &fakeRunner{respond: func(args []string) (string, error) {
		if args[0] == "tab" {
			return tabCreated("w9-7"), nil
		}
		return "", nil
	}}
	a := &app{d: Deps{Herdr: herdrClient(f), CurrentWorkspace: current, Out: io.Discard}, m: newModel("/home/u", 14)}
	a.m.resize(80, 10)
	a.m.setLoaded([]magpie.Session{s}, agents, true)
	return a, f
}

func TestPickNoticeKeepsLauncherOpen(t *testing.T) {
	// No directory, so the session falls back to the current workspace.
	a, f := pickFixture(t, magpie.Session{ID: "s1", Agent: "claude", Title: "fix", Resume: testResume}, nil, "w9")

	done, err := a.pick(t.Context())
	if done || err != nil {
		t.Fatalf("first pick: done = %v, err = %v; want the launcher to stay open", done, err)
	}
	if a.m.message != noticeMissingDir {
		t.Errorf("message = %q, want %q", a.m.message, noticeMissingDir)
	}
	checkCalls(t, f, [][]string{
		{"herdr", "tab", "create", "--workspace", "w9", "--label", "claude: fix", "--focus"},
		{"herdr", "pane", "run", "w9-7", testResume},
	})
	if got := a.m.shown[0].open; got == nil || *got != (openPane{paneID: "w9-7", workspaceID: "w9"}) {
		t.Fatalf("open = %+v, want pane w9-7 in workspace w9", got)
	}

	// Enter on the same session focuses its pane instead of resuming it again.
	f.calls = nil
	if done, err := a.pick(t.Context()); !done || err != nil {
		t.Fatalf("second pick: done = %v, err = %v; want the launcher to close", done, err)
	}
	checkCalls(t, f, [][]string{
		{"herdr", "workspace", "focus", "w9"},
		{"herdr", "agent", "focus", "w9-7"},
	})
}

func TestPickClosesOnPlainSuccess(t *testing.T) {
	// A pane in the session's directory takes the tab, and the launcher closes.
	dir := t.TempDir()
	s := magpie.Session{ID: "s1", Agent: "claude", Title: "fix", CWD: dir, Resume: testResume}
	a, f := pickFixture(t, s, []herdr.Agent{{CWD: dir, WorkspaceID: "w2", PaneID: "w2-1"}}, "w9")

	done, err := a.pick(t.Context())
	if !done || err != nil {
		t.Fatalf("done = %v, err = %v; want the launcher to close", done, err)
	}
	checkCalls(t, f, [][]string{
		{"herdr", "tab", "create", "--workspace", "w2", "--cwd", dir, "--label", "claude: fix", "--focus"},
		{"herdr", "pane", "run", "w9-7", testResume},
	})
}

func TestErrorLine(t *testing.T) {
	if got := errorLine(); got != "" {
		t.Errorf("errorLine() = %q, want empty", got)
	}
	got := errorLine(errors.New("magpie sessions: boom"), errors.New("herdr agent list: bad\nline"))
	if want := "magpie sessions: boom; herdr agent list: bad; line"; got != want {
		t.Errorf("errorLine = %q, want %q", got, want)
	}
}
