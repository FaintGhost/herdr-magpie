package herdr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// testBin is the binary name the fake runner sees.
const testBin = "herdr-test"

// fakeRunner returns canned output and records every call. It never executes
// anything, so write commands can be checked safely.
type fakeRunner struct {
	out   []byte
	err   error
	calls []fakeCall
}

type fakeCall struct {
	name string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, fakeCall{name: name, args: slices.Clone(args)})
	return f.out, f.err
}

// newFake returns a client backed by a fake runner that answers with out.
func newFake(out string) (*Client, *fakeRunner) {
	f := &fakeRunner{out: []byte(out)}
	return &Client{Bin: testBin, Runner: f}, f
}

// wantOneCall fails unless exactly one call was made, to testBin with argv want.
func wantOneCall(t *testing.T, f *fakeRunner, want ...string) {
	t.Helper()
	if len(f.calls) != 1 {
		t.Fatalf("herdr calls = %d, want 1", len(f.calls))
	}
	if got := f.calls[0]; got.name != testBin || !slices.Equal(got.args, want) {
		t.Errorf("call = %s %q\nwant   %s %q", got.name, got.args, testBin, want)
	}
}

// wantNoCalls fails if the runner was invoked at all.
func wantNoCalls(t *testing.T, f *fakeRunner) {
	t.Helper()
	if len(f.calls) != 0 {
		t.Errorf("herdr was invoked %d time(s), want none: %q", len(f.calls), f.calls[0].args)
	}
}

// checkErr fails unless err matches want: nil when want is empty, otherwise an
// error whose text contains want.
func checkErr(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Fatalf("unexpected error: %v", err)
	case want != "" && err == nil:
		t.Fatalf("err = nil, want an error containing %q", want)
	case want != "" && !strings.Contains(err.Error(), want):
		t.Fatalf("err = %v, want it to contain %q", err, want)
	}
}

// readFixture returns a file from testdata.
func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// blockingRunner waits until its context ends, as a hung herdr would.
type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// deadlineRunner records the deadline it was called with.
type deadlineRunner struct {
	deadline time.Time
}

func (r *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	r.deadline, _ = ctx.Deadline()
	return nil, errors.New("stop")
}

func TestClientTimeout(t *testing.T) {
	t.Run("configured timeout cancels a hung call", func(t *testing.T) {
		c := &Client{Bin: testBin, Runner: blockingRunner{}, Timeout: 20 * time.Millisecond}
		_, err := c.Agents(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
	})
	t.Run("zero timeout means DefaultTimeout", func(t *testing.T) {
		r := &deadlineRunner{}
		c := &Client{Bin: testBin, Runner: r}
		if _, err := c.Workspaces(t.Context()); err == nil {
			t.Fatal("want the stub error to propagate")
		}
		left := time.Until(r.deadline)
		if left <= DefaultTimeout-time.Second || left > DefaultTimeout {
			t.Errorf("deadline is %v away, want about %v", left, DefaultTimeout)
		}
	})
}

func TestExecRunner(t *testing.T) {
	t.Run("returns stdout", func(t *testing.T) {
		out, err := ExecRunner{}.Run(t.Context(), "sh", "-c", "printf hi")
		if err != nil || string(out) != "hi" {
			t.Fatalf("out = %q, err = %v", out, err)
		}
	})
	t.Run("error carries the first stderr line", func(t *testing.T) {
		_, err := ExecRunner{}.Run(t.Context(), "sh", "-c", `printf 'first line\nsecond' >&2; exit 3`)
		if err == nil || !strings.HasSuffix(err.Error(), ": first line") {
			t.Fatalf("err = %v, want a suffix of %q", err, ": first line")
		}
	})
	t.Run("deadline is reported as DeadlineExceeded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		_, err := ExecRunner{}.Run(ctx, "sleep", "10")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want DeadlineExceeded", err)
		}
	})
}

func TestFindBin(t *testing.T) {
	root := t.TempDir()
	pathDir := filepath.Join(root, "path")
	emptyDir := filepath.Join(root, "empty")
	shimHome := filepath.Join(root, "home")
	shimDir := filepath.Join(shimHome, ".local", "share", "mise", "shims")
	for _, d := range []string{pathDir, emptyDir, shimDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	onPath := filepath.Join(pathDir, "herdr")
	shim := filepath.Join(shimDir, "herdr")
	for _, p := range []string{onPath, shim} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	envBin := filepath.Join(root, "env-herdr")

	tests := []struct {
		name       string
		configured string
		envBin     string
		path       string
		home       string
		want       string
		wantErr    bool
	}{
		{name: "configured wins", configured: "/cfg/herdr", envBin: envBin, path: pathDir, home: shimHome, want: "/cfg/herdr"},
		{name: "HERDR_BIN_PATH is next", envBin: envBin, path: pathDir, home: shimHome, want: envBin},
		{name: "PATH is next", path: pathDir, home: shimHome, want: onPath},
		{name: "mise shim is the last resort", path: emptyDir, home: shimHome, want: shim},
		{name: "nothing found is an error", path: emptyDir, home: emptyDir, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HERDR_BIN_PATH", tc.envBin)
			t.Setenv("PATH", tc.path)
			t.Setenv("HOME", tc.home)
			got, err := FindBin(tc.configured)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("FindBin = %q, want %q", got, tc.want)
			}
		})
	}
}
