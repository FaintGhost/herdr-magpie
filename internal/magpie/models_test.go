package magpie

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseModelsFixtures(t *testing.T) {
	for _, name := range []string{"models_claude_live", "models_edge"} {
		t.Run(name, func(t *testing.T) {
			models, err := ParseModels(bytes.NewReader(readFixture(t, name+".txt")))
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, name, models)
		})
	}
}

func TestParseModelsNoModels(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"no models at all", "no models yet · add a provider first: magpie provider add deepseek sk-…\n"},
		{"every model hidden", "! example is shown none of them: nothing is in Example Vendor · magpie visible example all shows it every model\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			models, err := ParseModels(strings.NewReader(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if models == nil || len(models) != 0 {
				t.Errorf("models = %v, want an empty non-nil list", models)
			}
		})
	}
}

func TestParseModelsRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty output", ""},
		{"text with no rows", "  just some text\n"},
		{"row not indented", "example/a  Name\n"},
		{"odd indentation", "   example/a  Name\n"},
		{"hint without magpie", "    something else\n"},
		{"efforts slot is not efforts", "  example/a  Name  Not Efforts\n"},
		{"four columns", "  example/a  one  two  three\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseModels(strings.NewReader(tt.in))
			if !errors.Is(err, ErrUnrecognized) {
				t.Errorf("err = %v, want ErrUnrecognized", err)
			}
		})
	}
}

func TestModelsRunsModels(t *testing.T) {
	r := &fakeRunner{out: map[string]string{
		"magpie models claude": "  Claude Code\n  claude/example-opus  Example Opus  low/high\n",
	}}
	models, err := Models(t.Context(), r, "", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, []string{"magpie models claude"}) {
		t.Errorf("calls = %q", r.calls)
	}
	if len(models) != 1 || models[0].ID != "claude/example-opus" || models[0].Group != "Claude Code" || models[0].DisplayName != "Example Opus" {
		t.Errorf("models = %+v", models)
	}
}

func TestModelsRejectsBadAgentID(t *testing.T) {
	for _, id := range []string{"", "-x", "claude\nrm", "cl\x00aude"} {
		r := &fakeRunner{}
		if _, err := Models(t.Context(), r, "", id); err == nil {
			t.Errorf("Models(%q) succeeded, want an error", id)
		}
		if len(r.calls) != 0 {
			t.Errorf("Models(%q) ran %q, want no call", id, r.calls)
		}
	}
}
