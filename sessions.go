package main

import (
	"context"
	"os"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/launcher"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// cmdSessions runs the session launcher in the terminal herdr gives the popup pane.
func cmdSessions(ctx context.Context, e env, args []string) error {
	fs := newFlagSet("sessions")
	paths := addPathFlags(fs)
	if err := parseFlags(fs, e.stdout, args); err != nil {
		return err
	}
	configPath, _, err := resolvePaths(paths)
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
	return launcher.Run(ctx, launcher.Deps{
		Herdr:            &herdr.Client{Bin: herdrBin, Runner: e.herdrRunner()},
		MagpieBin:        magpieBin,
		Magpie:           magpie.ExecRunner{},
		Days:             cfg.SessionDays,
		CurrentWorkspace: os.Getenv("HERDR_WORKSPACE_ID"),
		In:               e.stdin,
		Out:              e.stdout,
	})
}
