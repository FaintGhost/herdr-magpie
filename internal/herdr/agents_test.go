package herdr

import (
	"errors"
	"reflect"
	"testing"
)

func TestAgentsFixture(t *testing.T) {
	c, f := newFake(readFixture(t, "agent_list.json"))
	agents, err := c.Agents(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantOneCall(t, f, "agent", "list")
	if len(agents) != 3 {
		t.Fatalf("agents = %d, want 3", len(agents))
	}
	want := []Agent{
		{
			Agent: "claude", Status: "working", CWD: "/srv/demo/project-a", ForegroundCWD: "/srv/demo/project-a",
			PaneID: "w1-1", TabID: "w1-t1", WorkspaceID: "w1", Title: "demo task", Focused: true,
			Session: new(AgentSession{Agent: "claude", Kind: "id", Source: "herdr:claude", Value: "0a0b0c0d-1111-4222-8333-444455556666"}),
		},
		{
			Agent: "omp", Status: "idle", CWD: "/srv/demo/project-b", ForegroundCWD: "/srv/demo/project-b",
			PaneID: "w2-1", TabID: "w2-t1", WorkspaceID: "w2", Title: "tidy imports", Focused: false,
			Session: new(AgentSession{Agent: "omp", Kind: "path", Source: "herdr:omp", Value: "/home/example/.omp/sessions/2026-01-01T00-00-00-000Z_aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee.jsonl"}),
		},
		{
			Status: "unknown", CWD: "/srv/demo/scratch", PaneID: "w2-2", TabID: "w2-t2", WorkspaceID: "w2",
		},
	}
	for i := range want {
		if !reflect.DeepEqual(agents[i], want[i]) {
			t.Errorf("agents[%d] = %+v\nwant %+v", i, agents[i], want[i])
		}
	}
}

func TestAgentsErrors(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		runErr  error
		wantErr string
	}{
		{name: "top-level error", out: `{"id":"t","error":{"code":"unavailable","message":"server is down"}}`, wantErr: "server is down"},
		{name: "error inside result", out: `{"id":"t","result":{"error":{"code":"bad_request","message":"nope"}}}`, wantErr: "nope"},
		{name: "no agents member", out: `{"id":"t","result":{"type":"agent_list"}}`, wantErr: "no agents"},
		{name: "no result", out: `{"id":"t"}`, wantErr: "no result"},
		{name: "not JSON", out: `<html>`, wantErr: "decode response"},
		{name: "empty output", out: ``, wantErr: "decode response"},
		{name: "runner failure", runErr: errors.New("exit status 1"), wantErr: "exit status 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{out: []byte(tc.out), err: tc.runErr}
			c := &Client{Bin: testBin, Runner: f}
			_, err := c.Agents(t.Context())
			checkErr(t, err, tc.wantErr)
		})
	}
}

func TestAgentGet(t *testing.T) {
	t.Run("fixture", func(t *testing.T) {
		c, f := newFake(readFixture(t, "agent_get.json"))
		got, err := c.Agent(t.Context(), "w1-1")
		if err != nil {
			t.Fatal(err)
		}
		wantOneCall(t, f, "agent", "get", "w1-1")
		if got.PaneID != "w1-1" || got.Status != "idle" || got.Title != "demo task" || got.Session == nil {
			t.Errorf("agent = %+v", got)
		}
	})
	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name    string
			pane    string
			out     string
			wantErr string
		}{
			{name: "missing agent", pane: "w1-1", out: `{"id":"t","result":{"type":"agent_info"}}`, wantErr: "no agent"},
			{name: "error envelope", pane: "w1-1", out: `{"id":"t","error":{"code":"not_found","message":"no such pane"}}`, wantErr: "no such pane"},
			{name: "dash-leading pane is refused", pane: "-w1", wantErr: "invalid pane"},
			{name: "empty pane is refused", pane: "", wantErr: "invalid pane"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				c, f := newFake(tc.out)
				_, err := c.Agent(t.Context(), tc.pane)
				checkErr(t, err, tc.wantErr)
				if tc.wantErr == "invalid pane" {
					wantNoCalls(t, f)
				}
			})
		}
	})
}

func TestWorkspaces(t *testing.T) {
	t.Run("fixture", func(t *testing.T) {
		c, f := newFake(readFixture(t, "workspace_list.json"))
		got, err := c.Workspaces(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		wantOneCall(t, f, "workspace", "list")
		want := []Workspace{
			{ID: "w1", Label: "demo", Focused: true},
			{ID: "w2", Label: "scratch", Focused: false},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("workspaces = %+v, want %+v", got, want)
		}
	})
	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name    string
			out     string
			wantErr string
		}{
			{name: "error envelope", out: `{"id":"t","error":{"code":"x","message":"socket closed"}}`, wantErr: "socket closed"},
			{name: "no workspaces member", out: `{"id":"t","result":{"type":"workspace_list"}}`, wantErr: "no workspaces"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				c, _ := newFake(tc.out)
				_, err := c.Workspaces(t.Context())
				checkErr(t, err, tc.wantErr)
			})
		}
	})
}

func TestSessionKey(t *testing.T) {
	tests := []struct {
		name string
		s    *AgentSession
		want string
	}{
		{name: "nil session", s: nil, want: ""},
		{name: "id kind is lowercased", s: &AgentSession{Kind: "id", Value: "0A0B0C0D-1111-4222-8333-444455556666"}, want: "0a0b0c0d-1111-4222-8333-444455556666"},
		{name: "path with a stamp prefix", s: &AgentSession{Kind: "path", Value: "/x/y/2026-01-01T00-00-00-000Z_AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE.jsonl"}, want: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"},
		{name: "path that is only a uuid", s: &AgentSession{Kind: "path", Value: "/x/11111111-2222-4333-8444-555555555555.jsonl"}, want: "11111111-2222-4333-8444-555555555555"},
		{name: "path without a uuid keeps the lowercased value", s: &AgentSession{Kind: "path", Value: "/x/Notes.JSONL"}, want: "/x/notes.jsonl"},
		{name: "unknown kind keeps the lowercased value", s: &AgentSession{Kind: "other", Value: "ABC"}, want: "abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SessionKey(tc.s); got != tc.want {
				t.Errorf("SessionKey = %q, want %q", got, tc.want)
			}
		})
	}
}
