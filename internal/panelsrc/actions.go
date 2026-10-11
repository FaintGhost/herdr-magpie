package panelsrc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/launcher"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/panel"
)

// errNoHerdr refuses an action that needs Herdr when no Herdr client is set.
var errNoHerdr = errors.New("herdr client not configured")

// Actions changes magpie's or Herdr's state for the panel. The panel asks the user
// to confirm each call before it makes one.
type Actions struct {
	Magpie  magpie.Runner
	Bin     string        // magpie CLI path; empty means "magpie"
	Herdr   *herdr.Client // needed by OpenSession and RestartPane
	Current string        // workspace the panel runs in; a session opened from it lands there when it can
	Home    string        // the user's home directory, for the launcher's directory rules
}

// UseProfile applies a saved profile: `magpie use <name>`.
func (a Actions) UseProfile(ctx context.Context, name string) error {
	return magpie.UseProfile(ctx, a.Magpie, a.Bin, name)
}

// SaveProfile saves the current setup as a profile: `magpie save <name>`.
func (a Actions) SaveProfile(ctx context.Context, name string) error {
	return magpie.SaveProfile(ctx, a.Magpie, a.Bin, name)
}

// SwitchAccount makes user the account agent uses: `magpie accounts switch <agent> <user>`.
func (a Actions) SwitchAccount(ctx context.Context, agent, user string) error {
	return magpie.SwitchAccount(ctx, a.Magpie, a.Bin, agent, user)
}

// OpenSession focuses the pane that already runs s, or resumes s the way the
// launcher does. The agent panes are read first, because the launcher must not
// resume a session that a pane already holds, and a failed read is an error. The
// notice is the launcher's fallback notice, or "" when s opened as usual.
func (a Actions) OpenSession(ctx context.Context, s magpie.Session) (string, error) {
	if a.Herdr == nil {
		return "", errNoHerdr
	}
	agents, err := a.Herdr.Agents(ctx)
	if err != nil {
		return "", fmt.Errorf("open session: read agent panes: %w", err)
	}
	notice, err := launcher.Open(ctx, a.Herdr, s, agents, a.Current, a.Home)
	if err != nil {
		return "", fmt.Errorf("open session: %w", err)
	}
	return notice, nil
}

// RestartPane closes an idle agent pane and resumes its session in a new tab of
// the same workspace and directory. Before it closes anything, it reads the pane
// again: the pane must still run t.Agent and be idle or done, or nothing changes.
// Each step that fails returns an error that names the step. A failure after the
// close leaves the old pane closed.
func (a Actions) RestartPane(ctx context.Context, t panel.RestartTarget) error {
	if a.Herdr == nil {
		return errNoHerdr
	}
	if strings.TrimSpace(t.Resume) == "" {
		return fmt.Errorf("restart pane %s: no resume command; the pane was not closed", t.PaneID)
	}
	pane, err := a.Herdr.Agent(ctx, t.PaneID)
	if err != nil {
		return fmt.Errorf("restart pane %s: check: %w", t.PaneID, err)
	}
	if pane.Agent != t.Agent {
		return fmt.Errorf("restart pane %s: check: the pane runs %q, not %q; nothing was closed", t.PaneID, pane.Agent, t.Agent)
	}
	if !isRestartable(pane.Status) {
		return fmt.Errorf("restart pane %s: check: the pane is %q, not idle or done; nothing was closed", t.PaneID, pane.Status)
	}
	if err := a.Herdr.ClosePane(ctx, t.PaneID); err != nil {
		return fmt.Errorf("restart pane %s: close: %w", t.PaneID, err)
	}
	tab, err := a.Herdr.CreateTab(ctx, t.WorkspaceID, t.CWD, t.Label, false)
	if err != nil {
		return fmt.Errorf("restart pane %s: create tab (the old pane is closed): %w", t.PaneID, err)
	}
	if err := a.Herdr.RunInPane(ctx, tab, t.Resume); err != nil {
		return fmt.Errorf("restart pane %s: resume in pane %s: %w", t.PaneID, tab, err)
	}
	return nil
}

// isRestartable reports whether an agent status allows a restart: the agent is
// idle or done, so it is not in the middle of work.
func isRestartable(status string) bool {
	return status == "idle" || status == "done"
}
