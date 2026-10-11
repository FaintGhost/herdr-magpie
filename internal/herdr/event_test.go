package herdr

import (
	"encoding/json"
	"testing"
)

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Event
		wantErr bool
	}{
		{
			name: "status changed uses the payload spelling",
			raw: `{"event":"pane_agent_status_changed","data":{"type":"pane_agent_status_changed",` +
				`"pane_id":"w1-1","workspace_id":"w1","agent":"claude","display_agent":"Claude",` +
				`"agent_status":"done","title":"demo task","state_labels":{}}}`,
			want: Event{Name: "pane.agent_status_changed", PaneID: "w1-1", WorkspaceID: "w1", Agent: "claude", Status: "done"},
		},
		{
			name: "detection with null status falls back to display_agent",
			raw: `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected",` +
				`"pane_id":"w2-1","workspace_id":"w2","agent":null,"display_agent":"omp",` +
				`"final_status":null,"released":false}}`,
			want: Event{Name: "pane.agent_detected", PaneID: "w2-1", WorkspaceID: "w2", Agent: "omp"},
		},
		{
			name: "detection reports final_status when agent_status is absent",
			raw:  `{"event":"pane_agent_detected","data":{"pane_id":"w2-1","agent":"omp","final_status":"blocked"}}`,
			want: Event{Name: "pane.agent_detected", PaneID: "w2-1", Agent: "omp", Status: "blocked"},
		},
		{
			name: "dotted name is kept",
			raw:  `{"event":"pane.agent_status_changed","data":{"pane_id":"w1-2","agent_status":"blocked"}}`,
			want: Event{Name: "pane.agent_status_changed", PaneID: "w1-2", Status: "blocked"},
		},
		{
			name: "name falls back to data.type",
			raw:  `{"data":{"type":"tab_created"}}`,
			want: Event{Name: "tab.created"},
		},
		{
			name: "only the first underscore becomes a dot",
			raw:  `{"event":"workspace_metadata_updated","data":{"workspace_id":"w3"}}`,
			want: Event{Name: "workspace.metadata_updated", WorkspaceID: "w3"},
		},
		{
			name: "missing data is tolerated",
			raw:  `{"event":"layout_updated"}`,
			want: Event{Name: "layout.updated"},
		},
		{
			name: "empty object gives an empty event",
			raw:  `{}`,
			want: Event{},
		},
		{
			name:    "blank input",
			raw:     "  \n",
			wantErr: true,
		},
		{
			name:    "truncated JSON",
			raw:     `{"event":`,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEvent(tc.raw)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got.Name != tc.want.Name || got.PaneID != tc.want.PaneID || got.WorkspaceID != tc.want.WorkspaceID ||
				got.Agent != tc.want.Agent || got.Status != tc.want.Status {
				t.Errorf("event = %+v, want %+v", got, tc.want)
			}
			if string(got.Raw) != tc.raw || !json.Valid(got.Raw) {
				t.Errorf("Raw = %q, want the input unchanged", got.Raw)
			}
		})
	}
}
