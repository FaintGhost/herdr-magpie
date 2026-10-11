package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEnv(stdout, stderr *bytes.Buffer) env {
	return env{stdout: stdout, stderr: stderr, now: time.Now}
}

func TestRunDispatch(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantOut   string // substring of stdout
		wantExact string // full stdout, when set
		wantErr   error  // sentinel expected from run, nil for success
	}{
		{name: "no arguments prints usage", wantOut: "Usage: herdr-magpie <command>"},
		{name: "help", args: []string{"--help"}, wantOut: "Commands:"},
		{name: "version", args: []string{"version"}, wantExact: "herdr-magpie 0.1.0\n"},
		{name: "unknown command", args: []string{"frobnicate"}, wantErr: errUsage},
		{name: "help for one command succeeds", args: []string{"setup", "--help"}, wantOut: "Usage: herdr-magpie setup [options]"},
		{name: "stray argument is a usage error", args: []string{"setup", "extra"}, wantErr: errUsage},
		{name: "unknown flag is a usage error", args: []string{"doctor", "--nope"}, wantErr: errUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(t.Context(), testEnv(&stdout, &stderr), tt.args)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("run() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if tt.wantExact != "" && stdout.String() != tt.wantExact {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantExact)
			}
			if tt.wantOut != "" && !strings.Contains(stdout.String(), tt.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.wantOut)
			}
		})
	}
}

// TestSetupWritesNothing checks that setup only prints: it creates neither the
// state directory nor the config file.
func TestSetupWritesNothing(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	configPath := filepath.Join(dir, "config.json")
	var stdout, stderr bytes.Buffer
	args := []string{"setup", "--config", configPath, "--state-dir", stateDir}
	if err := run(t.Context(), testEnv(&stdout, &stderr), args); err != nil {
		t.Fatalf("run(setup) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "tab_bar_right") {
		t.Errorf("setup printed no snippet: %q", stdout.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("setup created %d entries in %s", len(entries), dir)
	}
}

func TestDryRunner(t *testing.T) {
	fake := &fakeTools{out: map[string]string{"herdr agent list": `{"result":{"agents":[]}}`}}
	var out bytes.Buffer
	d := dryRunner{out: &out, inner: fake}

	if _, err := d.Run(t.Context(), "herdr", "agent", "list"); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("read was not passed through: calls = %q", fake.calls)
	}

	if _, err := d.Run(t.Context(), "herdr", "notification", "show", "magpie: x", "--sound", "request"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Errorf("write reached the real runner: calls = %q", fake.calls)
	}
	want := `dry-run: would run herdr notification show "magpie: x" --sound request` + "\n"
	if out.String() != want {
		t.Errorf("dry-run output = %q, want %q", out.String(), want)
	}

	stub, err := d.Run(t.Context(), "herdr", "tab", "create")
	if err != nil {
		t.Fatalf("tab create: %v", err)
	}
	if !strings.Contains(string(stub), "dry-run-pane") {
		t.Errorf("tab create stub = %s, want a root pane", stub)
	}
}

func TestIsHerdrRead(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{args: []string{"agent", "list"}, want: true},
		{args: []string{"agent", "get", "p1"}, want: true},
		{args: []string{"workspace", "list"}, want: true},
		{args: []string{"agent", "focus", "p1"}},
		{args: []string{"pane", "report-metadata", "p1"}},
		{args: []string{"notification", "show", "t"}},
		{args: []string{"agent"}},
		{},
	}
	for _, tt := range tests {
		if got := isHerdrRead(tt.args); got != tt.want {
			t.Errorf("isHerdrRead(%q) = %t, want %t", tt.args, got, tt.want)
		}
	}
}

func TestQuoteArgv(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "herdr", args: []string{"pane", "run", "p1"}, want: "herdr pane run p1"},
		{name: "herdr", args: []string{"pane", "run", "p1", "cd x"}, want: `herdr pane run p1 "cd x"`},
		{name: "herdr", args: []string{"a\"b"}, want: `herdr "a\"b"`},
	}
	for _, tt := range tests {
		if got := quoteArgv(tt.name, tt.args); got != tt.want {
			t.Errorf("quoteArgv(%q, %q) = %q, want %q", tt.name, tt.args, got, tt.want)
		}
	}
}

func TestDryRunEnabled(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "1", want: true},
		{value: "true", want: true},
		{value: "0", want: false},
		{value: "", want: false},
		{value: "yes", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			t.Setenv("HERDR_MAGPIE_DRY_RUN", tt.value)
			if got := dryRunEnabled(); got != tt.want {
				t.Errorf("dryRunEnabled() with %q = %t, want %t", tt.value, got, tt.want)
			}
		})
	}
}
