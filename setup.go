package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
)

// setupInfo holds the absolute paths that the setup text refers to.
type setupInfo struct {
	executable  string // this binary
	herdrBin    string // the herdr CLI, or "herdr" when it cannot be found
	herdrConfig string // herdr's config.toml
	configPath  string // optional config.json
	stateDir    string // plugin state directory
}

// cmdSetup prints the config snippets for the user to merge. It writes nothing,
// and it never edits herdr's configuration.
func cmdSetup(ctx context.Context, e env, args []string) error {
	fs := newFlagSet("setup")
	wait := fs.Bool("wait", false, "keep the pane open until Enter (for overlays)")
	paths := addPathFlags(fs)
	if err := parseFlags(fs, e.stdout, args); err != nil {
		return err
	}
	configPath, stateDir, err := resolvePaths(paths)
	if err != nil {
		return err
	}
	if configPath, err = filepath.Abs(configPath); err != nil {
		return err
	}
	if stateDir, err = filepath.Abs(stateDir); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	herdrBin, err := herdr.FindBin("")
	if err != nil {
		herdrBin = "herdr"
	}
	info := setupInfo{
		executable:  exe,
		herdrBin:    herdrBin,
		herdrConfig: herdrConfigPath(),
		configPath:  configPath,
		stateDir:    stateDir,
	}
	fmt.Fprint(e.stdout, setupText(info))
	if *wait {
		return waitForEnter(ctx, e)
	}
	return nil
}

// herdrConfigPath is where herdr reads config.toml: HERDR_CONFIG_PATH, or the
// config.toml under the XDG configuration directory.
func herdrConfigPath() string {
	if p := os.Getenv("HERDR_CONFIG_PATH"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "(unknown: no configuration directory)"
	}
	return filepath.Join(dir, "herdr", "config.toml")
}

// statusCommand is the tab-bar shell line. Like system-stats, it prints nothing
// when the binary is missing, instead of an error in the bar.
func statusCommand(info setupInfo) string {
	return "binary=" + shellQuote(info.executable) +
		`; if [ -x "$binary" ]; then "$binary" status --config ` + shellQuote(info.configPath) +
		" --state-dir " + shellQuote(info.stateDir) + "; fi"
}

// setupText is the text that setup prints.
func setupText(info setupInfo) string {
	var b strings.Builder
	b.WriteString("magpie setup\n\n")
	fmt.Fprintf(&b, "Herdr config:  %s\n", info.herdrConfig)
	fmt.Fprintf(&b, "Plugin config: %s (optional)\n", info.configPath)
	fmt.Fprintf(&b, "Saved state:   %s\n\n", info.stateDir)
	b.WriteString("Nothing is written to your configuration. Merge these entries yourself.\n\n")

	b.WriteString("1. Top bar. Add the entry to your existing [ui].tab_bar_right array. Keep the\n")
	b.WriteString("   entries you have, and do not add a second [ui] table.\n\n")
	b.WriteString("[ui]\n")
	b.WriteString("tab_bar_right = [\n")
	fmt.Fprintf(&b, "  { type = \"command\", command = %s, interval_seconds = 10, timeout_seconds = 3 },\n", tomlString(statusCommand(info)))
	b.WriteString("]\n\n")

	b.WriteString("2. Sidebar tokens. Sidebar layout and keybindings come from the CLIENT's config:\n")
	b.WriteString("   with `herdr --remote`, put sections 2 and 3 in the config.toml of the machine you\n")
	b.WriteString("   attach from, not on this server. Keep the built-in items you already show.\n\n")
	b.WriteString("[ui.sidebar.agents]\n")
	b.WriteString(`rows = [["state_icon", "machine", "workspace", "tab"], ["agent"], ["$magpie_model", "$magpie_cost"]]` + "\n\n")
	b.WriteString("[ui.sidebar.spaces]\n")
	b.WriteString(`rows = [["state_icon", "workspace"], ["branch", "git_status"], ["$magpie_cost"]]` + "\n\n")

	b.WriteString("3. Session launcher (optional). Pick another key if prefix+m is already bound.\n\n")
	b.WriteString("[[keys.command]]\n")
	b.WriteString(`key = "prefix+m"` + "\n")
	b.WriteString(`type = "plugin_action"` + "\n")
	b.WriteString(`command = "faintghost.magpie.open-sessions"` + "\n")
	b.WriteString(`description = "Open magpie sessions"` + "\n\n")

	b.WriteString("Then run `herdr config check`, and reload the config from Herdr's menu or with\n")
	b.WriteString("`herdr server reload-config` (a remote client also reloads its local config from the\n")
	b.WriteString("menu's `reload config`). Gateway and storm notifications need no setup.\n")
	return b.String()
}

// shellQuote single-quotes s for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tomlString quotes s as a TOML basic string. Quotes, backslashes and control
// characters are escaped; other text is copied as UTF-8.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// waitForEnter keeps an overlay pane open until Enter or cancellation. It waits
// only on a terminal, so piped runs return at once.
func waitForEnter(ctx context.Context, e env) error {
	if !isTerminal(e.stdin) {
		return nil
	}
	fmt.Fprint(e.stdout, "\nPress Enter to close.\n")
	done := make(chan struct{})
	go func() {
		_, _ = bufio.NewReader(e.stdin).ReadString('\n')
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isTerminal reports whether f is a character device such as a terminal.
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
