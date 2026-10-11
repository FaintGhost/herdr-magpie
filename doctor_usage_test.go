package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDoctorUsageLog checks the usage-log check. A missing log is a warning that
// says storm alerts are off, and it fails nothing. A present log reports the age
// of its newest row and the gateway rows of the storm window.
func TestDoctorUsageLog(t *testing.T) {
	var out bytes.Buffer
	r := &reporter{out: &out}
	doctorUsageLog(r, filepath.Join(t.TempDir(), "usage.jsonl"), testNow)
	if r.failures != 0 || !strings.HasPrefix(out.String(), "warn") ||
		!strings.Contains(out.String(), "usage log unavailable → storm alerts disabled") {
		t.Errorf("missing log: failures %d, output %q", r.failures, out.String())
	}

	path := writeUsageLog(t,
		usageJSON(t, testNow.Add(-time.Minute), "p1", 200, ""),
		usageJSON(t, testNow.Add(-3*time.Minute), "p1", 429, "rate limited"),
		usageJSON(t, testNow.Add(-30*time.Minute), "p1", 200, ""),
	)
	out.Reset()
	r = &reporter{out: &out}
	doctorUsageLog(r, path, testNow)
	if r.failures != 0 {
		t.Errorf("present log: failures = %d, want 0; output %q", r.failures, out.String())
	}
	for _, want := range []string{"last row 1m0s ago", "2 gateway rows in the last 10m"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q lacks %q", out.String(), want)
		}
	}
}
