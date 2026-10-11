package herdr

import (
	"errors"
	"strings"
	"testing"
)

func TestCreateTab(t *testing.T) {
	t.Run("returns the root pane of the new tab", func(t *testing.T) {
		c, f := newFake(readFixture(t, "tab_create.json"))
		pane, err := c.CreateTab(t.Context(), "w1", "/srv/demo/project-a", "claude: demo task", true)
		if err != nil {
			t.Fatal(err)
		}
		if pane != "w1-3" {
			t.Errorf("pane = %q, want w1-3", pane)
		}
		wantOneCall(t, f, "tab", "create", "--workspace", "w1", "--cwd", "/srv/demo/project-a",
			"--label", "claude: demo task", "--focus")
	})
	t.Run("empty options keep CLI defaults", func(t *testing.T) {
		c, f := newFake(readFixture(t, "tab_create.json"))
		if _, err := c.CreateTab(t.Context(), "", "", "", false); err != nil {
			t.Fatal(err)
		}
		wantOneCall(t, f, "tab", "create", "--no-focus")
	})
	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name    string
			out     string
			wantErr string
		}{
			{name: "no root pane", out: `{"id":"t","result":{"type":"tab_created","tab":{"tab_id":"w1-t2"}}}`, wantErr: "root_pane.pane_id"},
			{name: "error envelope", out: `{"id":"t","error":{"code":"x","message":"no such workspace"}}`, wantErr: "no such workspace"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				c, _ := newFake(tc.out)
				_, err := c.CreateTab(t.Context(), "w9", "", "", false)
				checkErr(t, err, tc.wantErr)
			})
		}
	})
}

func TestCreateWorkspace(t *testing.T) {
	t.Run("returns the workspace and its root pane", func(t *testing.T) {
		c, f := newFake(readFixture(t, "workspace_create.json"))
		ws, pane, err := c.CreateWorkspace(t.Context(), "/srv/demo/project-a", true)
		if err != nil {
			t.Fatal(err)
		}
		if ws != "w3" || pane != "w3-1" {
			t.Errorf("workspace, pane = %q, %q; want w3, w3-1", ws, pane)
		}
		wantOneCall(t, f, "workspace", "create", "--cwd", "/srv/demo/project-a", "--focus")
	})
	t.Run("empty options keep CLI defaults and pass no label", func(t *testing.T) {
		c, f := newFake(readFixture(t, "workspace_create.json"))
		if _, _, err := c.CreateWorkspace(t.Context(), "", false); err != nil {
			t.Fatal(err)
		}
		wantOneCall(t, f, "workspace", "create", "--no-focus")
	})
	t.Run("errors", func(t *testing.T) {
		tests := []struct {
			name    string
			out     string
			wantErr string
		}{
			{name: "no root pane", out: `{"id":"t","result":{"type":"workspace_created","workspace":{"workspace_id":"w3"}}}`, wantErr: "root_pane.pane_id"},
			{name: "no workspace id", out: `{"id":"t","result":{"type":"workspace_created","root_pane":{"pane_id":"w3-1"}}}`, wantErr: "workspace.workspace_id"},
			{name: "error envelope", out: `{"id":"t","error":{"code":"x","message":"no such directory"}}`, wantErr: "no such directory"},
			{name: "not JSON", out: `workspace created`, wantErr: "decode response"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				c, _ := newFake(tc.out)
				_, _, err := c.CreateWorkspace(t.Context(), "/srv/demo/project-a", true)
				checkErr(t, err, tc.wantErr)
			})
		}
	})
	t.Run("runner failure is returned", func(t *testing.T) {
		c := &Client{Bin: testBin, Runner: &fakeRunner{err: errors.New("socket closed")}}
		_, _, err := c.CreateWorkspace(t.Context(), "/srv/demo/project-a", true)
		checkErr(t, err, "socket closed")
	})
}

func TestRunInPane(t *testing.T) {
	const resume = "cd '/srv/demo/project-a' && claude --resume 0a0b0c0d-1111-4222-8333-444455556666"
	tests := []struct {
		name    string
		pane    string
		command string
		wantErr string
	}{
		{name: "command is one argument", pane: "w1-3", command: resume},
		{name: "empty command is refused", pane: "w1-3", command: "", wantErr: "invalid command"},
		{name: "dash-leading command is refused", pane: "w1-3", command: "-rf /", wantErr: "invalid command"},
		{name: "empty pane is refused", pane: "", command: resume, wantErr: "invalid pane"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newFake("")
			err := c.RunInPane(t.Context(), tc.pane, tc.command)
			checkErr(t, err, tc.wantErr)
			if tc.wantErr != "" {
				wantNoCalls(t, f)
				return
			}
			wantOneCall(t, f, "pane", "run", tc.pane, tc.command)
		})
	}
}

func TestFocusCalls(t *testing.T) {
	t.Run("FocusPane uses agent focus", func(t *testing.T) {
		c, f := newFake("")
		if err := c.FocusPane(t.Context(), "w1-1"); err != nil {
			t.Fatal(err)
		}
		wantOneCall(t, f, "agent", "focus", "w1-1")
	})
	t.Run("FocusWorkspace uses workspace focus", func(t *testing.T) {
		c, f := newFake("")
		if err := c.FocusWorkspace(t.Context(), "w2"); err != nil {
			t.Fatal(err)
		}
		wantOneCall(t, f, "workspace", "focus", "w2")
	})
	t.Run("empty ids are refused without a call", func(t *testing.T) {
		c, f := newFake("")
		for _, err := range []error{c.FocusPane(t.Context(), ""), c.FocusWorkspace(t.Context(), "-w")} {
			if err == nil || !strings.Contains(err.Error(), "invalid") {
				t.Errorf("err = %v, want an invalid-argument error", err)
			}
		}
		wantNoCalls(t, f)
	})
}
