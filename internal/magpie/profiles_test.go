package magpie

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseProfilesFixture(t *testing.T) {
	profiles, err := ParseProfiles(bytes.NewReader(readFixture(t, "profiles_edge.txt")))
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "profiles_edge", profiles)
}

func TestParseProfilesNone(t *testing.T) {
	profiles, err := ParseProfiles(bytes.NewReader(readFixture(t, "profiles_none.txt")))
	if err != nil {
		t.Fatal(err)
	}
	if profiles == nil || len(profiles) != 0 {
		t.Errorf("profiles = %v, want an empty non-nil list", profiles)
	}
}

func TestParseProfilesRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty output", ""},
		{"blank lines only", "\n  \n"},
		{"row not indented", "work  claude opus\n"},
		{"row indented too far", "   work  claude opus\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseProfiles(strings.NewReader(tt.in))
			if !errors.Is(err, ErrUnrecognized) {
				t.Errorf("err = %v, want ErrUnrecognized", err)
			}
		})
	}
}

func TestProfilesRunsProfiles(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"magpie profiles": "no profiles yet · magpie save <name>\n"}}
	profiles, err := Profiles(t.Context(), r, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.calls, []string{"magpie profiles"}) || len(profiles) != 0 {
		t.Errorf("calls = %q, profiles = %v", r.calls, profiles)
	}
}
