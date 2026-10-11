package main

import "testing"

func TestFocusedPaneAgent(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "unset", raw: "", want: ""},
		{name: "not JSON", raw: "claude", want: ""},
		{name: "agent named", raw: `{"focused_pane_agent":"claude","focused_pane_id":"w1-2"}`, want: "claude"},
		{name: "no agent in the focused pane", raw: `{"focused_pane_agent":null,"focused_pane_id":"w1-2"}`, want: ""},
		{name: "padded label", raw: `{"focused_pane_agent":"  codex "}`, want: "codex"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := focusedPaneAgent(tt.raw); got != tt.want {
				t.Errorf("focusedPaneAgent(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
