package magpie

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// The write functions are tested with a fake Runner only. Their argv is checked
// here, and nothing is run.

func TestUseAndSaveArgv(t *testing.T) {
	r := &fakeRunner{}
	if err := UseProfile(t.Context(), r, "", "example"); err != nil {
		t.Fatal(err)
	}
	if err := SaveProfile(t.Context(), r, "/opt/magpie", "example"); err != nil {
		t.Fatal(err)
	}
	want := []string{"magpie use example", "/opt/magpie save example"}
	if !slices.Equal(r.calls, want) {
		t.Errorf("calls = %q, want %q", r.calls, want)
	}
}

func TestSwitchAccountArgv(t *testing.T) {
	r := &fakeRunner{}
	if err := SwitchAccount(t.Context(), r, "", "claude", "person@example.test"); err != nil {
		t.Fatal(err)
	}
	want := []string{"magpie accounts switch claude person@example.test"}
	if !slices.Equal(r.calls, want) {
		t.Errorf("calls = %q, want %q", r.calls, want)
	}
}

func TestActionsRejectBadArguments(t *testing.T) {
	bad := []string{"", "-x", "--all", "a\nb", "tab\tname", "esc\x1b[0m"}
	for _, name := range bad {
		r := &fakeRunner{}
		if UseProfile(t.Context(), r, "", name) == nil || SaveProfile(t.Context(), r, "", name) == nil {
			t.Errorf("profile name %q accepted", name)
		}
		if SwitchAccount(t.Context(), r, "", name, "person@example.test") == nil ||
			SwitchAccount(t.Context(), r, "", "claude", name) == nil {
			t.Errorf("switch argument %q accepted", name)
		}
		if len(r.calls) != 0 {
			t.Errorf("argument %q ran %q, want no call", name, r.calls)
		}
	}
}

func TestActionsReturnRunnerError(t *testing.T) {
	r := &fakeRunner{errs: map[string]error{"magpie use example": errors.New("boom")}}
	if err := UseProfile(t.Context(), r, "", "example"); err == nil {
		t.Error("UseProfile ignored the runner error")
	}
}

// callRunner records what the context looked like when the command ran.
type callRunner struct {
	cancelled   error     // ctx.Err() at the call
	deadline    time.Time // the context's deadline at the call
	hasDeadline bool
	calls       []string
}

func (r *callRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.cancelled = ctx.Err()
	r.deadline, r.hasDeadline = ctx.Deadline()
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return nil, nil
}

func TestWritesOutliveCancelAndGetLongTimeout(t *testing.T) {
	cancelled, cancel := context.WithCancel(t.Context())
	cancel() // the panel has quit while the write is pending
	r := &callRunner{}
	if err := SwitchAccount(cancelled, r, "/opt/magpie", "claude", "person@example.test"); err != nil {
		t.Fatal(err)
	}
	if r.cancelled != nil {
		t.Errorf("write ran with a cancelled context: %v", r.cancelled)
	}
	if !r.hasDeadline {
		t.Fatal("write ran without a deadline")
	}
	if left := time.Until(r.deadline); left < writeTimeout-5*time.Second || left > writeTimeout {
		t.Errorf("deadline in %v, want about %v", left, writeTimeout)
	}
	if want := []string{"/opt/magpie accounts switch claude person@example.test"}; !slices.Equal(r.calls, want) {
		t.Errorf("calls = %q, want %q", r.calls, want)
	}
}
