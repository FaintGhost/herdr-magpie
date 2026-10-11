package herdr

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// AgentSession identifies an agent's resumable session. Kind is "id" (Value is
// the session id) or "path" (Value is a transcript file path).
type AgentSession struct {
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	Value  string `json:"value"`
}

// Agent is one agent pane as reported by herdr. Title is the terminal title
// with control sequences stripped. Session is nil when the pane has none.
type Agent struct {
	Agent         string        `json:"agent"`
	Status        string        `json:"agent_status"`
	CWD           string        `json:"cwd"`
	ForegroundCWD string        `json:"foreground_cwd"`
	PaneID        string        `json:"pane_id"`
	TabID         string        `json:"tab_id"`
	WorkspaceID   string        `json:"workspace_id"`
	Title         string        `json:"terminal_title_stripped"`
	Focused       bool          `json:"focused"`
	Session       *AgentSession `json:"agent_session"`
}

// Workspace is one herdr workspace.
type Workspace struct {
	ID      string `json:"workspace_id"`
	Label   string `json:"label"`
	Focused bool   `json:"focused"`
}

// Agents lists the agent panes (herdr agent list).
func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	out, err := c.run(ctx, "agent", "list")
	if err != nil {
		return nil, err
	}
	var body struct {
		Agents []Agent `json:"agents"`
	}
	if err := decodeResult(out, &body); err != nil {
		return nil, fmt.Errorf("agent list: %w", err)
	}
	if body.Agents == nil {
		return nil, errors.New("agent list: result has no agents")
	}
	return body.Agents, nil
}

// Agent shows one agent pane (herdr agent get).
func (c *Client) Agent(ctx context.Context, pane string) (Agent, error) {
	if err := checkArg("pane", pane); err != nil {
		return Agent{}, err
	}
	out, err := c.run(ctx, "agent", "get", pane)
	if err != nil {
		return Agent{}, err
	}
	var body struct {
		Agent *Agent `json:"agent"`
	}
	if err := decodeResult(out, &body); err != nil {
		return Agent{}, fmt.Errorf("agent get: %w", err)
	}
	if body.Agent == nil {
		return Agent{}, errors.New("agent get: result has no agent")
	}
	return *body.Agent, nil
}

// Workspaces lists the workspaces (herdr workspace list).
func (c *Client) Workspaces(ctx context.Context) ([]Workspace, error) {
	out, err := c.run(ctx, "workspace", "list")
	if err != nil {
		return nil, err
	}
	var body struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if err := decodeResult(out, &body); err != nil {
		return nil, fmt.Errorf("workspace list: %w", err)
	}
	if body.Workspaces == nil {
		return nil, errors.New("workspace list: result has no workspaces")
	}
	return body.Workspaces, nil
}

var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// SessionKey returns the lowercase key that magpie session ids are compared
// with. For kind "id" it is the value. For kind "path" it is the last UUID in
// the file name, which covers both "<stamp>_<uuid>.jsonl" and "<uuid>.jsonl".
// Without a UUID, and for any other kind, it is the value itself. A nil session
// gives "".
func SessionKey(s *AgentSession) string {
	if s == nil {
		return ""
	}
	key := s.Value
	if s.Kind == "path" {
		if found := uuidPattern.FindAllString(filepath.Base(s.Value), -1); len(found) > 0 {
			key = found[len(found)-1]
		}
	}
	return strings.ToLower(key)
}
