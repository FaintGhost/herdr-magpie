package main

import (
	"strings"
	"testing"
)

func TestStatusCommandGuardsBinary(t *testing.T) {
	info := setupInfo{
		executable: "/opt/plugins/herdr-magpie",
		configPath: "/cfg/config.json",
		stateDir:   "/state/it's here",
	}
	want := `binary='/opt/plugins/herdr-magpie'; if [ -x "$binary" ]; then "$binary" status --config '/cfg/config.json' --state-dir '/state/it'\''s here'; fi`
	if got := statusCommand(info); got != want {
		t.Errorf("statusCommand() =\n%s\nwant\n%s", got, want)
	}
}

func TestTOMLString(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "plain", want: `"plain"`},
		{in: `say "hi"`, want: `"say \"hi\""`},
		{in: `C:\x`, want: `"C:\\x"`},
		{in: "tab\there", want: `"tab\u0009here"`},
		{in: "\x7f", want: `"\u007F"`},
		{in: "中文", want: `"中文"`},
	}
	for _, tt := range tests {
		if got := tomlString(tt.in); got != tt.want {
			t.Errorf("tomlString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestSetupTextSnippets(t *testing.T) {
	text := setupText(setupInfo{
		executable:  "/opt/herdr-magpie",
		herdrBin:    "/opt/herdr",
		herdrConfig: "/home/u/.config/herdr/config.toml",
		configPath:  "/cfg/config.json",
		stateDir:    "/state",
	})
	wantLines := []string{
		`command = "binary='/opt/herdr-magpie'; if [ -x \"$binary\" ]; then \"$binary\" status --config '/cfg/config.json' --state-dir '/state'; fi", interval_seconds = 10, timeout_seconds = 3 },`,
		`rows = [["state_icon", "machine", "workspace", "tab"], ["agent"], ["$magpie_model", "$magpie_cost"]]`,
		`rows = [["state_icon", "workspace"], ["branch", "git_status"], ["$magpie_cost"]]`,
		`key = "prefix+m"`,
		`type = "plugin_action"`,
		`command = "faintghost.magpie.open-panel"`,
		"herdr config check",
		"Nothing is written to your configuration",
	}
	for _, want := range wantLines {
		if !strings.Contains(text, want) {
			t.Errorf("setup text is missing %q", want)
		}
	}
	for _, retired := range []string{"magpie_route", "magpie_alert"} {
		if strings.Contains(text, retired) {
			t.Errorf("setup text mentions the retired token %q", retired)
		}
	}
}
