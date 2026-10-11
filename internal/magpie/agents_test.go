package magpie

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseLSFixtures(t *testing.T) {
	for _, name := range []string{"ls_live", "ls_edge"} {
		t.Run(name, func(t *testing.T) {
			agents, err := ParseLS(bytes.NewReader(readFixture(t, name+".txt")))
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, name, agents)
		})
	}
}

func TestParseLSRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty output", ""},
		{"blank lines only", "\n   \n"},
		{"magpie's own message", "no supported agents found on this machine\n"},
		{"row not indented", "Claude Code  opus  ·  effort high  ~/.claude/settings.json\n"},
		{"name and path, no values", "  Claude Code  ~/.claude/settings.json\n"},
		{"model with a space", "  Claude Code  effort high  ~/.claude/settings.json\n"},
		{"field without a label", "  Claude Code  opus  ·  high  ~/.claude/settings.json\n"},
		{"one bad row among good ones", "  Codex  codex/example-gpt\n  Claude Code  opus  ·  high\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseLS(strings.NewReader(tt.in))
			if !errors.Is(err, ErrUnrecognized) {
				t.Errorf("err = %v, want ErrUnrecognized", err)
			}
		})
	}
}

func TestAgentsRunsLS(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"/opt/magpie ls": "  Codex  codex/example-gpt  ·  effort high  ~/.example/codex.toml\n",
	}}
	agents, err := Agents(t.Context(), r, "/opt/magpie")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, []string{"/opt/magpie ls"}) {
		t.Errorf("calls = %q, want only the ls call", r.calls)
	}
	if len(agents) != 1 || agents[0].Name != "Codex" || agents[0].ID != "codex" || agents[0].Model() != "codex/example-gpt" {
		t.Errorf("agents = %+v", agents)
	}
}

func TestAgentsRunnerError(t *testing.T) {
	r := &fakeRunner{errs: map[string]error{"magpie ls": errors.New("boom")}}
	if _, err := Agents(t.Context(), r, ""); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the runner's error", err)
	}
}
