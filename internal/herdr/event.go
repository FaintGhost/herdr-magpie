package herdr

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Event is a herdr plugin event (HERDR_PLUGIN_EVENT_JSON). Name uses the dotted
// form that manifests and subscriptions use, such as "pane.agent_status_changed",
// whichever spelling the payload used. Fields the payload lacks stay empty.
type Event struct {
	Name        string
	PaneID      string
	WorkspaceID string
	Agent       string
	Status      string
	Raw         json.RawMessage
}

// ParseEvent decodes an event envelope {"event": ..., "data": {...}}. Status is
// data.agent_status, or data.final_status for detection events. Agent is
// data.agent, falling back to data.display_agent.
func ParseEvent(raw string) (Event, error) {
	if strings.TrimSpace(raw) == "" {
		return Event{}, errors.New("herdr: empty event JSON")
	}
	var env struct {
		Event string `json:"event"`
		Data  struct {
			Type         string `json:"type"`
			PaneID       string `json:"pane_id"`
			WorkspaceID  string `json:"workspace_id"`
			Agent        string `json:"agent"`
			DisplayAgent string `json:"display_agent"`
			AgentStatus  string `json:"agent_status"`
			FinalStatus  string `json:"final_status"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return Event{}, fmt.Errorf("herdr: decode event: %w", err)
	}
	return Event{
		Name:        canonicalName(cmp.Or(env.Event, env.Data.Type)),
		PaneID:      env.Data.PaneID,
		WorkspaceID: env.Data.WorkspaceID,
		Agent:       cmp.Or(env.Data.Agent, env.Data.DisplayAgent),
		Status:      cmp.Or(env.Data.AgentStatus, env.Data.FinalStatus),
		Raw:         json.RawMessage(raw),
	}, nil
}

// canonicalName maps the payload spelling "pane_agent_status_changed" to the
// dotted "pane.agent_status_changed". Names that already contain a dot are
// returned unchanged.
func canonicalName(name string) string {
	if strings.Contains(name, ".") {
		return name
	}
	prefix, rest, ok := strings.Cut(name, "_")
	if !ok {
		return name
	}
	return prefix + "." + rest
}
