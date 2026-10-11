package magpie

import (
	"errors"
	"slices"
	"testing"
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
