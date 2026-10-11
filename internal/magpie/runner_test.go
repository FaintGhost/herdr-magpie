package magpie

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeRunner answers commands from tables keyed by "name arg ...". Unknown
// commands return empty output and no error. Every call is recorded in order.
type fakeRunner struct {
	out   map[string]string
	errs  map[string]error
	calls []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, key)
	if err := f.errs[key]; err != nil {
		return nil, err
	}
	return []byte(f.out[key]), nil
}

// deadlineRunner records how long the context of its last call runs for.
type deadlineRunner struct {
	left time.Duration
}

func (r *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	if d, ok := ctx.Deadline(); ok {
		r.left = time.Until(d)
	} else {
		r.left = -1
	}
	return []byte("[]"), nil
}

func TestCLICapIsFiveSecondsUnlessRaised(t *testing.T) {
	r := &deadlineRunner{}
	if _, err := Quotas(t.Context(), r, "magpie"); err != nil {
		t.Fatal(err)
	}
	if r.left <= 4*time.Second || r.left > magpieTimeout {
		t.Errorf("default cap leaves %v, want about %v", r.left, magpieTimeout)
	}

	raised := WithCLITimeout(t.Context(), 20*time.Second)
	if _, err := Quotas(raised, r, "magpie"); err != nil {
		t.Fatal(err)
	}
	if r.left <= 15*time.Second || r.left > 20*time.Second {
		t.Errorf("raised cap leaves %v, want about 20s", r.left)
	}
	if _, err := Sessions(raised, r, "magpie", 7); err != nil {
		t.Fatal(err)
	}
	if r.left <= 15*time.Second || r.left > 20*time.Second {
		t.Errorf("Sessions under the raised cap leaves %v, want about 20s", r.left)
	}

	shorter, cancel := context.WithTimeout(raised, time.Second)
	defer cancel()
	if _, err := Quotas(shorter, r, "magpie"); err != nil {
		t.Fatal(err)
	}
	if r.left > time.Second {
		t.Errorf("a caller's shorter deadline leaves %v, want at most 1s", r.left)
	}
}

func TestExecRunnerRun(t *testing.T) {
	tests := []struct {
		name        string
		bin         string
		args        []string
		timeout     time.Duration // 0 means no extra deadline
		wantOut     string
		wantErr     bool
		errContains []string
		errOmits    []string
	}{
		{
			name:    "returns stdout and drops stderr",
			bin:     "sh",
			args:    []string{"-c", "printf out; printf noise >&2"},
			wantOut: "out",
		},
		{
			name:        "failure carries only the first stderr line",
			bin:         "sh",
			args:        []string{"-c", "printf 'boom\nsecond line\n' >&2; exit 3"},
			wantErr:     true,
			errContains: []string{"exit status 3", "boom"},
			errOmits:    []string{"second line"},
		},
		{
			name:        "missing binary is an error",
			bin:         "magpie-test-no-such-binary",
			wantErr:     true,
			errContains: []string{"magpie-test-no-such-binary"},
		},
		{
			name:        "deadline stops the command",
			bin:         "sleep",
			args:        []string{"5"},
			timeout:     50 * time.Millisecond,
			wantErr:     true,
			errContains: []string{"context deadline exceeded"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			if tt.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.timeout)
				defer cancel()
			}
			out, err := ExecRunner{}.Run(ctx, tt.bin, tt.args...)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("Run() error = %v", err)
				}
				if got := string(out); got != tt.wantOut {
					t.Errorf("stdout = %q, want %q", got, tt.wantOut)
				}
				return
			}
			if err == nil {
				t.Fatalf("Run() returned stdout %q, want error", out)
			}
			if out != nil {
				t.Errorf("stdout = %q on failure, want nil", out)
			}
			for _, want := range tt.errContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			for _, bad := range tt.errOmits {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("error %q contains %q", err, bad)
				}
			}
		})
	}
}
