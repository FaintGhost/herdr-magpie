// Package herdr wraps the herdr CLI. It knows nothing about magpie.
package herdr

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout bounds each herdr call when Client.Timeout is zero.
const DefaultTimeout = 3 * time.Second

// Runner runs a command and returns its stdout. Tests substitute a fake.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner is the default Runner. Its errors carry a one-line summary of
// stderr, or of stdout when stderr is empty.
type ExecRunner struct{}

// Run executes name with args and returns its stdout.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), nil
	}
	what := describe(name, args)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %w", what, ctxErr)
	}
	msg := summarize(stderr.Bytes())
	if msg == "" {
		msg = summarize(stdout.Bytes())
	}
	if msg == "" {
		return stdout.Bytes(), fmt.Errorf("%s: %w", what, err)
	}
	return stdout.Bytes(), fmt.Errorf("%s: %w: %s", what, err, msg)
}

// FindBin locates the herdr binary: the configured path, then $HERDR_BIN_PATH,
// then PATH (executable files only), then the mise shim under $HOME. The
// configured and env values are returned unchecked, so a bad one surfaces when
// herdr is first called.
func FindBin(configured string) (string, error) {
	if p := cmp.Or(configured, os.Getenv("HERDR_BIN_PATH")); p != "" {
		return p, nil
	}
	if p, err := exec.LookPath("herdr"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		shim := filepath.Join(home, ".local", "share", "mise", "shims", "herdr")
		if fi, err := os.Stat(shim); err == nil && !fi.IsDir() {
			return shim, nil
		}
	}
	return "", errors.New("herdr binary not found: set herdr_bin in config or HERDR_BIN_PATH")
}

// Client runs the herdr CLI. The zero value runs "herdr" from PATH through
// ExecRunner with DefaultTimeout.
type Client struct {
	Bin     string
	Runner  Runner
	Timeout time.Duration
}

// run executes one herdr call under the client timeout.
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	runner := c.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return runner.Run(ctx, cmp.Or(c.Bin, "herdr"), args...)
}

// decodeResult checks an API response envelope ({"id":..,"result":..}) and
// decodes its result into v. An error member, at the top level or inside the
// result, fails the call.
func decodeResult(out []byte, v any) error {
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if err := apiError(env.Error); err != nil {
		return err
	}
	if len(env.Result) == 0 || bytes.Equal(env.Result, []byte("null")) {
		return errors.New("response has no result")
	}
	var inner struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(env.Result, &inner); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	if err := apiError(inner.Error); err != nil {
		return err
	}
	if err := json.Unmarshal(env.Result, v); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	return nil
}

// apiError turns an API error member ({"code","message"}) into an error. A
// missing or null member means no error.
func apiError(raw json.RawMessage) error {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.Message == "" {
		return fmt.Errorf("herdr API error: %s", truncate(string(raw), 200))
	}
	if body.Code != "" {
		return fmt.Errorf("herdr API error %s: %s", body.Code, body.Message)
	}
	return fmt.Errorf("herdr API error: %s", body.Message)
}

// checkArg rejects values the CLI would read as options or as nothing at all.
// It applies to positional arguments only.
func checkArg(what, v string) error {
	if v == "" || strings.HasPrefix(v, "-") {
		return fmt.Errorf("herdr: invalid %s %q", what, v)
	}
	return nil
}

// describe names a call for error messages: the binary base name and at most
// two arguments. Those are subcommand words, so pane ids and commands stay out.
func describe(name string, args []string) string {
	words := append([]string{filepath.Base(name)}, args[:min(len(args), 2)]...)
	return strings.Join(words, " ")
}

// summarize returns the first line of b, shortened for an error message.
func summarize(b []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	return truncate(line, 200)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
