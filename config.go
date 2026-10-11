package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	pluginID = "faintghost.magpie"
	version  = "0.1.0"

	// Files in the state directory. Each has one writer, so only the lock
	// shared by status and refresh needs coordination.
	statusFile  = "status.json"  // gateway debounce, discovery cache, last spawn, storm view and toast cooldowns; written by status
	refreshFile = "refresh.json" // token state, last run and last successful read; written under the lock by refresh
	refreshLog  = "refresh.log"  // output of detached refreshes
)

// defaultSessionDays is the session window used when the config does not set one.
const defaultSessionDays = 14

// config is the optional config.json. Keys that are absent keep their defaults.
type config struct {
	GatewayURL  string `json:"gateway_url"`
	MagpieBin   string `json:"magpie_bin"`
	HerdrBin    string `json:"herdr_bin"`
	Notify      bool   `json:"notify"`
	SessionDays int    `json:"session_days"`
	UsagePath   string `json:"usage_path"`
}

func defaultConfig() config {
	return config{Notify: true, SessionDays: defaultSessionDays}
}

// defaultPaths returns the absolute config file and state directory. Herdr's
// plugin variables win when they are set. Otherwise the XDG locations apply,
// because herdr runs `status` without plugin variables.
func defaultPaths() (configPath, stateDir string, err error) {
	configDir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	stateDir = os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if configDir == "" || stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		configRoot := cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
		stateRoot := cmp.Or(os.Getenv("XDG_STATE_HOME"), filepath.Join(home, ".local", "state"))
		configDir = cmp.Or(configDir, filepath.Join(configRoot, "herdr", "plugins", "config", pluginID))
		stateDir = cmp.Or(stateDir, filepath.Join(stateRoot, "herdr", "plugins", pluginID))
	}
	configPath, err = filepath.Abs(filepath.Join(configDir, "config.json"))
	if err != nil {
		return "", "", err
	}
	stateDir, err = filepath.Abs(stateDir)
	return configPath, stateDir, err
}

// usageLogPath returns the magpie usage log that storm alerts read: usage_path
// when it is set, otherwise magpie's own file under the XDG config home.
func usageLogPath(cfg config) (string, error) {
	if cfg.UsagePath != "" {
		return cfg.UsagePath, nil
	}
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".config")
	}
	return filepath.Join(root, "magpie", "usage.jsonl"), nil
}

// gatewayKeyFile is the gateway key's file name, in the plugin config directory
// beside config.json. The file holds one line and is mode 0600.
const gatewayKeyFile = "gateway-key"

// gatewayKeyPath returns the gateway key file that goes with the config file at configPath.
func gatewayKeyPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), gatewayKeyFile)
}

// loadConfig reads path. A missing file yields the defaults. Unknown fields,
// trailing data, a body that is not one JSON object, and invalid values are errors.
func loadConfig(path string) (config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read configuration: %w", err)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return cfg, errors.New("configuration must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("parse configuration: %w", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return cfg, errors.New("configuration must contain exactly one JSON object")
	}
	if cfg.SessionDays < 1 {
		return cfg, errors.New("session_days must be at least 1")
	}
	if err := checkGatewayURL(cfg.GatewayURL); err != nil {
		return cfg, err
	}
	if cfg.UsagePath != "" && !filepath.IsAbs(cfg.UsagePath) {
		return cfg, errors.New("usage_path must be an absolute path")
	}
	return cfg, nil
}

// checkGatewayURL accepts an empty value or an http(s) URL with a host, with or
// without a scheme. The error does not repeat the value, which may hold credentials.
func checkGatewayURL(raw string) error {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("gateway_url must be an http(s) URL with a host")
	}
	return nil
}

// redactURL returns raw without its userinfo, so credentials never reach output.
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable URL)"
	}
	u.User = nil
	return u.String()
}
