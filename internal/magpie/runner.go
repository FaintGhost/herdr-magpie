// Package magpie reads what the magpie gateway and CLI know: where the gateway
// is and whether it answers, plus the sessions and requests magpie has logged.
// It has no herdr knowledge. Every external command goes through Runner so
// tests can substitute a fake.
package magpie

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// magpieTimeout caps one magpie CLI call. Callers may pass a shorter deadline, and
// may raise the cap with WithCLITimeout.
const magpieTimeout = 5 * time.Second

// cliTimeoutKey is the context key that WithCLITimeout sets.
type cliTimeoutKey struct{}

// WithCLITimeout returns ctx with a cap of d for the magpie CLI calls made with it,
// in place of magpieTimeout. Only the caller that sets it is affected: refresh and
// doctor keep the default, while the panel's reads, which run beside each other,
// take up to 20 s.
func WithCLITimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, cliTimeoutKey{}, d)
}

// cliCap is the cap for one magpie CLI call made with ctx.
func cliCap(ctx context.Context) time.Duration {
	if d, ok := ctx.Value(cliTimeoutKey{}).(time.Duration); ok && d > 0 {
		return d
	}
	return magpieTimeout
}

// Runner runs an external command and returns its stdout.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner is the production Runner, backed by os/exec. It returns stdout
// only. When the command fails, the first line of its stderr is included in
// the error, and the rest of stderr is discarded.
type ExecRunner struct{}

const (
	waitDelay        = time.Second // bounds Wait if a grandchild keeps the pipes open
	stderrKeep       = 4 << 10     // stderr bytes retained for the summary
	stderrSummaryMax = 160         // bytes of the summary shown in an error
)

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stdout bytes.Buffer
	var stderr boundedBuffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay

	label := filepath.Base(name)
	if len(args) > 0 {
		label += " " + args[0]
	}
	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s: %w", label, ctxErr)
		}
		if summary := summarize(stderr.buf); summary != "" {
			return nil, fmt.Errorf("%s: %w: %s", label, err, summary)
		}
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return stdout.Bytes(), nil
}

// boundedBuffer keeps the first stderrKeep bytes written to it and drops the rest.
type boundedBuffer struct {
	buf []byte
}

// Write implements io.Writer. It never fails, so a chatty child cannot block on stderr.
func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := stderrKeep - len(b.buf); room > 0 {
		b.buf = append(b.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// summarize returns the first non-blank line of stderr, truncated for an error message.
func summarize(stderr []byte) string {
	line, _, _ := bytes.Cut(bytes.TrimSpace(stderr), []byte("\n"))
	s := strings.TrimSpace(string(line))
	if len(s) > stderrSummaryMax {
		s = strings.ToValidUTF8(s[:stderrSummaryMax], "") + "..."
	}
	return s
}
