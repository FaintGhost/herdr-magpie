package herdr

import (
	"context"
	"errors"
	"fmt"
)

// CreateTab creates a tab and returns the pane id of its root pane. Empty
// workspace, cwd or label keep the CLI defaults.
func (c *Client) CreateTab(ctx context.Context, workspace, cwd, label string, focus bool) (string, error) {
	args := []string{"tab", "create"}
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if label != "" {
		args = append(args, "--label", label)
	}
	if focus {
		args = append(args, "--focus")
	} else {
		args = append(args, "--no-focus")
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	// The tab_created result carries the new tab and its root pane.
	var body struct {
		RootPane struct {
			PaneID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	if err := decodeResult(out, &body); err != nil {
		return "", fmt.Errorf("tab create: %w", err)
	}
	if body.RootPane.PaneID == "" {
		return "", errors.New("tab create: result has no root_pane.pane_id")
	}
	return body.RootPane.PaneID, nil
}

// CreateWorkspace creates a workspace whose first tab's root pane starts in cwd,
// and returns the workspace id and that root pane's id. Empty cwd keeps the CLI
// default. No label is passed, so herdr names the workspace after cwd's directory.
func (c *Client) CreateWorkspace(ctx context.Context, cwd string, focus bool) (workspaceID, paneID string, err error) {
	args := []string{"workspace", "create"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	if focus {
		args = append(args, "--focus")
	} else {
		args = append(args, "--no-focus")
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", "", err
	}
	// The workspace_created result carries the new workspace and its root pane.
	var body struct {
		Workspace struct {
			WorkspaceID string `json:"workspace_id"`
		} `json:"workspace"`
		RootPane struct {
			PaneID string `json:"pane_id"`
		} `json:"root_pane"`
	}
	if err := decodeResult(out, &body); err != nil {
		return "", "", fmt.Errorf("workspace create: %w", err)
	}
	if body.Workspace.WorkspaceID == "" {
		return "", "", errors.New("workspace create: result has no workspace.workspace_id")
	}
	if body.RootPane.PaneID == "" {
		return "", "", errors.New("workspace create: result has no root_pane.pane_id")
	}
	return body.Workspace.WorkspaceID, body.RootPane.PaneID, nil
}

// RunInPane runs command in pane (herdr pane run). The command is passed as a
// single argument.
func (c *Client) RunInPane(ctx context.Context, pane, command string) error {
	if err := checkArg("pane", pane); err != nil {
		return err
	}
	if err := checkArg("command", command); err != nil {
		return err
	}
	_, err := c.run(ctx, "pane", "run", pane, command)
	return err
}

// FocusPane focuses the agent in pane (herdr agent focus). `herdr pane focus`
// only moves by direction, so it cannot target a pane id.
func (c *Client) FocusPane(ctx context.Context, pane string) error {
	if err := checkArg("pane", pane); err != nil {
		return err
	}
	_, err := c.run(ctx, "agent", "focus", pane)
	return err
}

// FocusWorkspace focuses a workspace (herdr workspace focus).
func (c *Client) FocusWorkspace(ctx context.Context, ws string) error {
	if err := checkArg("workspace", ws); err != nil {
		return err
	}
	_, err := c.run(ctx, "workspace", "focus", ws)
	return err
}
