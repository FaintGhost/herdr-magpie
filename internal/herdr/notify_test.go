package herdr

import "testing"

func TestNotify(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		body    string
		sound   string
		want    []string // nil means herdr must not be invoked
		wantErr string
	}{
		{
			name:  "title, body and sound",
			title: "magpie: claude fell back",
			body:  "claude-sonnet → omp-large",
			sound: "request",
			want:  []string{"notification", "show", "magpie: claude fell back", "--body", "claude-sonnet → omp-large", "--sound", "request"},
		},
		{
			name:  "title only",
			title: "magpie gateway up",
			want:  []string{"notification", "show", "magpie gateway up"},
		},
		{
			name:  "dash-leading body is passed as a value",
			title: "t",
			body:  "-5 requests",
			want:  []string{"notification", "show", "t", "--body", "-5 requests"},
		},
		{
			name:    "invalid sound",
			title:   "t",
			sound:   "ding",
			wantErr: "invalid notification sound",
		},
		{
			name:    "empty title",
			title:   "",
			wantErr: "invalid notification title",
		},
		{
			name:    "dash-leading title is refused",
			title:   "--help",
			wantErr: "invalid notification title",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newFake("")
			err := c.Notify(t.Context(), tc.title, tc.body, tc.sound)
			checkErr(t, err, tc.wantErr)
			if tc.want == nil {
				wantNoCalls(t, f)
				return
			}
			wantOneCall(t, f, tc.want...)
		})
	}
}
