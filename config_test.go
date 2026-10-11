package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		body    *string // nil means the file does not exist
		want    config
		wantErr bool
	}{
		{name: "missing file gives defaults", want: defaultConfig()},
		{name: "empty object keeps defaults", body: new("{}"), want: defaultConfig()},
		{
			name: "every key overrides",
			body: new(`{"gateway_url":"http://127.0.0.1:3425","magpie_bin":"/opt/magpie","herdr_bin":"/opt/herdr","notify":false,"session_days":7,"usage_path":"/opt/usage.jsonl"}`),
			want: config{GatewayURL: "http://127.0.0.1:3425", MagpieBin: "/opt/magpie", HerdrBin: "/opt/herdr", Notify: false, SessionDays: 7, UsagePath: "/opt/usage.jsonl"},
		},
		{name: "unknown field is an error", body: new(`{"disks":["/"]}`), wantErr: true},
		{name: "two objects are an error", body: new(`{} {}`), wantErr: true},
		{name: "array is not an object", body: new(`[]`), wantErr: true},
		{name: "empty file is not an object", body: new(""), wantErr: true},
		{name: "zero days is an error", body: new(`{"session_days":0}`), wantErr: true},
		{name: "relative usage path is an error", body: new(`{"usage_path":"usage.jsonl"}`), wantErr: true},
		{name: "negative days is an error", body: new(`{"session_days":-3}`), wantErr: true},
		{name: "non-http gateway is an error", body: new(`{"gateway_url":"ftp://gw.example.test"}`), wantErr: true},
		{name: "gateway without host is an error", body: new(`{"gateway_url":"http://"}`), wantErr: true},
		{
			name: "gateway without scheme is accepted as written",
			body: new(`{"gateway_url":"192.0.2.10:3425"}`),
			want: config{GatewayURL: "192.0.2.10:3425", Notify: true, SessionDays: defaultSessionDays},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if tt.body != nil {
				if err := os.WriteFile(path, []byte(*tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := loadConfig(path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("loadConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("loadConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCheckGatewayURL(t *testing.T) {
	tests := []struct {
		raw     string
		wantErr bool
	}{
		{raw: ""},
		{raw: "   "},
		{raw: "http://127.0.0.1:3425"},
		{raw: "https://gw.example.test/"},
		{raw: "192.0.2.10:3425"},
		{raw: "http://user:secret@gw.example.test:3425"},
		{raw: "ftp://gw.example.test", wantErr: true},
		{raw: "http://", wantErr: true},
		{raw: "://nohost", wantErr: true},
	}
	for _, tt := range tests {
		err := checkGatewayURL(tt.raw)
		if (err != nil) != tt.wantErr {
			t.Errorf("checkGatewayURL(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
		}
		if err != nil && strings.Contains(err.Error(), "secret") {
			t.Errorf("checkGatewayURL(%q) error %q repeats the value", tt.raw, err)
		}
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		raw, want string
	}{
		{raw: "http://127.0.0.1:3425", want: "http://127.0.0.1:3425"},
		{raw: "http://user:pw@gw.example.test:3425/", want: "http://gw.example.test:3425/"},
		{raw: "::bad", want: "(unparseable URL)"},
	}
	for _, tt := range tests {
		if got := redactURL(tt.raw); got != tt.want {
			t.Errorf("redactURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestDefaultPaths(t *testing.T) {
	tests := []struct {
		name       string
		home       string
		xdgConfig  string
		xdgState   string
		pluginCfg  string
		pluginData string
		wantConfig string
		wantState  string
	}{
		{
			name:       "plugin variables win",
			home:       "/home/u",
			xdgConfig:  "/xdg/config",
			xdgState:   "/xdg/state",
			pluginCfg:  "/plugin/config",
			pluginData: "/plugin/state",
			wantConfig: "/plugin/config/config.json",
			wantState:  "/plugin/state",
		},
		{
			name:       "plugin variables need no home directory",
			pluginCfg:  "/plugin/config",
			pluginData: "/plugin/state",
			wantConfig: "/plugin/config/config.json",
			wantState:  "/plugin/state",
		},
		{
			name:       "XDG fallback without plugin variables",
			home:       "/home/u",
			xdgConfig:  "/xdg/config",
			xdgState:   "/xdg/state",
			wantConfig: "/xdg/config/herdr/plugins/config/faintghost.magpie/config.json",
			wantState:  "/xdg/state/herdr/plugins/faintghost.magpie",
		},
		{
			name:       "home fallback without XDG",
			home:       "/home/u",
			wantConfig: "/home/u/.config/herdr/plugins/config/faintghost.magpie/config.json",
			wantState:  "/home/u/.local/state/herdr/plugins/faintghost.magpie",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", tt.home)
			t.Setenv("XDG_CONFIG_HOME", tt.xdgConfig)
			t.Setenv("XDG_STATE_HOME", tt.xdgState)
			t.Setenv("HERDR_PLUGIN_CONFIG_DIR", tt.pluginCfg)
			t.Setenv("HERDR_PLUGIN_STATE_DIR", tt.pluginData)
			gotConfig, gotState, err := defaultPaths()
			if err != nil {
				t.Fatal(err)
			}
			if gotConfig != tt.wantConfig || gotState != tt.wantState {
				t.Errorf("defaultPaths() = (%q, %q), want (%q, %q)", gotConfig, gotState, tt.wantConfig, tt.wantState)
			}
		})
	}
}

func TestUsageLogPath(t *testing.T) {
	t.Run("usage_path wins", func(t *testing.T) {
		got, err := usageLogPath(config{UsagePath: "/srv/usage.jsonl"})
		if err != nil || got != "/srv/usage.jsonl" {
			t.Errorf("usageLogPath() = %q, %v", got, err)
		}
	})
	t.Run("XDG config home", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "/xdg")
		got, err := usageLogPath(defaultConfig())
		if want := "/xdg/magpie/usage.jsonl"; err != nil || got != want {
			t.Errorf("usageLogPath() = %q, %v; want %q", got, err, want)
		}
	})
	t.Run("home fallback", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", "/home/u")
		got, err := usageLogPath(defaultConfig())
		if want := "/home/u/.config/magpie/usage.jsonl"; err != nil || got != want {
			t.Errorf("usageLogPath() = %q, %v; want %q", got, err, want)
		}
	})
}
