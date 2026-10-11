package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestReporterCountsFailures(t *testing.T) {
	var out bytes.Buffer
	r := &reporter{out: &out}
	r.check(true, "config", "defaults")
	r.result(errors.New("magpie: not found"), "magpie CLI", "")
	r.result(nil, "herdr CLI", "/usr/bin/herdr")
	if r.failures != 1 {
		t.Errorf("failures = %d, want 1", r.failures)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %q", len(lines), out.String())
	}
	if !strings.HasPrefix(lines[0], "ok ") || !strings.Contains(lines[0], "defaults") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "FAIL") || !strings.Contains(lines[1], "magpie: not found") {
		t.Errorf("second line = %q", lines[1])
	}
	if !strings.Contains(lines[2], "/usr/bin/herdr") {
		t.Errorf("third line = %q", lines[2])
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "herdr 0.9.3\n", want: "herdr 0.9.3"},
		{in: "  a  \nb", want: "a"},
		{in: "", want: ""},
	}
	for _, tt := range tests {
		if got := firstLine([]byte(tt.in)); got != tt.want {
			t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
