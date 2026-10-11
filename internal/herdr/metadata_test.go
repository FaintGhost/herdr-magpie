package herdr

import (
	"fmt"
	"strings"
	"testing"
)

const testSource = "faintghost.magpie"

func TestReportPaneTokens(t *testing.T) {
	tests := []struct {
		name    string
		pane    string
		set     map[string]string
		unset   []string
		want    []string // nil means herdr must not be invoked
		wantErr string
	}{
		{
			name:  "set and clear, sorted",
			pane:  "p-1",
			set:   map[string]string{"magpie_model": "gpt-x", "magpie_cost": "$1.20"},
			unset: []string{"magpie_route", "magpie_alert"},
			want: []string{"pane", "report-metadata", "p-1", "--source", testSource,
				"--token", "magpie_cost=$1.20", "--token", "magpie_model=gpt-x",
				"--clear-token", "magpie_alert", "--clear-token", "magpie_route"},
		},
		{
			name:  "duplicate clear names collapse",
			pane:  "p-1",
			unset: []string{"a", "a"},
			want:  []string{"pane", "report-metadata", "p-1", "--source", testSource, "--clear-token", "a"},
		},
		{
			name: "empty value is kept",
			pane: "p-1",
			set:  map[string]string{"magpie_route": ""},
			want: []string{"pane", "report-metadata", "p-1", "--source", testSource, "--token", "magpie_route="},
		},
		{
			name: "nothing to report runs nothing",
			pane: "p-1",
			want: nil,
		},
		{
			name:    "bad token name",
			pane:    "p-1",
			set:     map[string]string{"bad name": "x"},
			wantErr: "invalid token name",
		},
		{
			name:    "token name over 32 characters",
			pane:    "p-1",
			set:     map[string]string{strings.Repeat("a", 33): "x"},
			wantErr: "invalid token name",
		},
		{
			name:    "bad clear name",
			pane:    "p-1",
			unset:   []string{"dot.name"},
			wantErr: "invalid token name",
		},
		{
			name:    "set and clear the same name",
			pane:    "p-1",
			set:     map[string]string{"a": "1"},
			unset:   []string{"a"},
			wantErr: "both set and cleared",
		},
		{
			name:    "dash-leading pane is refused",
			pane:    "-p",
			set:     map[string]string{"a": "1"},
			wantErr: "invalid target",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newFake("")
			err := c.ReportPaneTokens(t.Context(), tc.pane, testSource, tc.set, tc.unset)
			checkErr(t, err, tc.wantErr)
			if tc.want == nil {
				wantNoCalls(t, f)
				return
			}
			wantOneCall(t, f, tc.want...)
		})
	}
}

func TestReportTokenLimit(t *testing.T) {
	makeSet := func(n int) map[string]string {
		m := make(map[string]string, n)
		for i := range n {
			m[fmt.Sprintf("k%02d", i)] = "v"
		}
		return m
	}
	t.Run("16 tokens are allowed", func(t *testing.T) {
		c, f := newFake("")
		if err := c.ReportPaneTokens(t.Context(), "p-1", testSource, makeSet(16), nil); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) != 1 {
			t.Fatalf("calls = %d, want 1", len(f.calls))
		}
	})
	t.Run("17 tokens across set and clear are refused", func(t *testing.T) {
		c, f := newFake("")
		err := c.ReportPaneTokens(t.Context(), "p-1", testSource, makeSet(12), []string{"c0", "c1", "c2", "c3", "c4"})
		checkErr(t, err, "exceed the limit")
		wantNoCalls(t, f)
	})
}

func TestReportWorkspaceTokens(t *testing.T) {
	tests := []struct {
		name    string
		ws      string
		set     map[string]string
		unset   []string
		want    []string
		wantErr string
	}{
		{
			name:  "set and clear",
			ws:    "w1",
			set:   map[string]string{"magpie_cost": "$3.00 today"},
			unset: []string{"magpie_old"},
			want: []string{"workspace", "report-metadata", "w1", "--source", testSource,
				"--token", "magpie_cost=$3.00 today", "--clear-token", "magpie_old"},
		},
		{name: "nothing to report runs nothing", ws: "w1", want: nil},
		{name: "bad token name", ws: "w1", set: map[string]string{"a/b": "x"}, wantErr: "invalid token name"},
		{name: "empty workspace is refused", ws: "", set: map[string]string{"a": "1"}, wantErr: "invalid target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newFake("")
			err := c.ReportWorkspaceTokens(t.Context(), tc.ws, testSource, tc.set, tc.unset)
			checkErr(t, err, tc.wantErr)
			if tc.want == nil {
				wantNoCalls(t, f)
				return
			}
			wantOneCall(t, f, tc.want...)
		})
	}
}
