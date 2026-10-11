package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTools answers magpie and herdr calls from canned stdout and records every
// call as a quoted line. An unknown magpie call fails, so tests cannot reach the
// real CLIs. An unknown herdr write succeeds with an empty result.
type fakeTools struct {
	out   map[string]string // "name arg ..." -> stdout
	calls []string          // one entry per call, in order
}

// Run implements both magpie.Runner and herdr.Runner.
func (f *fakeTools) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, quoteArgv(name, args))
	key := strings.Join(append([]string{name}, args...), " ")
	if out, ok := f.out[key]; ok {
		return []byte(out), nil
	}
	if name == "herdr" && !isHerdrRead(args) {
		return []byte(`{"result":{}}`), nil
	}
	return nil, fmt.Errorf("unexpected call: %s", key)
}

// writes returns the recorded herdr writes: metadata reports and notifications.
func (f *fakeTools) writes() []string {
	var out []string
	for _, c := range f.calls {
		if strings.Contains(c, " report-metadata ") || strings.HasPrefix(c, "herdr notification show ") {
			out = append(out, c)
		}
	}
	return out
}

// usageJSON is one synthetic usage.jsonl line in the shape the gateway writes,
// with only the fields the plugin reads, and a trailing newline.
func usageJSON(t *testing.T, at time.Time, provider string, status int, errText string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"t": at.Format(time.RFC3339Nano), "agent": "alpha", "provider": provider, "host": "gw.example.test",
		"model": "m-a", "served": "m-a", "status": status, "err": errText, "req": "req-" + at.Format("150405"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

// writeUsageLog writes the lines to a new usage.jsonl in a temporary directory and returns its path.
func writeUsageLog(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
