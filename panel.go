package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/panel"
	"github.com/FaintGhost/herdr-magpie/internal/panelsrc"
	"github.com/FaintGhost/herdr-magpie/internal/state"
)

// cmdPanel shows the magpie panel in the popup pane herdr opens for it. --tab
// names the tab to show first; anything unknown shows the Agents tab.
func cmdPanel(ctx context.Context, e env, args []string) error {
	fs := newFlagSet("panel")
	startTab := fs.String("tab", "", "tab to show first: agents, allowances, usage, routing, sessions, or a number 1 to 5")
	paths := addPathFlags(fs)
	if err := parseFlags(fs, e.stdout, args); err != nil {
		return err
	}
	configPath, stateDir, err := resolvePaths(paths)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	herdrBin, err := herdr.FindBin(cfg.HerdrBin)
	if err != nil {
		return err
	}
	magpieBin, err := magpie.FindBin(cfg.MagpieBin)
	if err != nil {
		return err
	}
	usagePath, err := usageLogPath(cfg)
	if err != nil {
		return err
	}
	store, err := state.Open(stateDir)
	if err != nil {
		return err
	}
	gatewayURL, _ := magpie.Discover(ctx, magpie.DiscoverOptions{ConfigURL: cfg.GatewayURL})
	home, _ := os.UserHomeDir()
	herdrClient := &herdr.Client{Bin: herdrBin, Runner: e.herdrRunner()}
	api := magpie.APIClient{BaseURL: gatewayURL, KeyFile: gatewayKeyPath(configPath)}

	return panel.Run(ctx, panel.Deps{
		Source: panelsrc.Source{
			Magpie:    e.magpieRunner(),
			Bin:       magpieBin,
			UsagePath: usagePath,
			Days:      cfg.SessionDays,
			API:       api,
		},
		Actions: panelsrc.Actions{
			Magpie:  e.magpieRunner(),
			Bin:     magpieBin,
			Herdr:   herdrClient,
			Current: os.Getenv("HERDR_WORKSPACE_ID"),
			Home:    home,
		},
		Herdr:      herdrClient,
		Cache:      panelsrc.Cache{Store: store},
		In:         e.stdin,
		Out:        e.stdout,
		StartTab:   *startTab,
		FocusAgent: focusedPaneAgent(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")),
	})
}

// focusedPaneAgent returns the agent label of the pane the panel was opened from,
// read from HERDR_PLUGIN_CONTEXT_JSON. It is "" when the context is unset, is not
// JSON, or names no agent for the focused pane.
func focusedPaneAgent(contextJSON string) string {
	var ctx struct {
		FocusedPaneAgent string `json:"focused_pane_agent"`
	}
	if err := json.Unmarshal([]byte(contextJSON), &ctx); err != nil {
		return ""
	}
	return strings.TrimSpace(ctx.FocusedPaneAgent)
}
