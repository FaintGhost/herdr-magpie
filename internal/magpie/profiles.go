package magpie

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

// Profiles runs `magpie profiles` and parses the saved profiles.
func Profiles(ctx context.Context, r Runner, bin string) ([]Profile, error) {
	out, err := runCLI(ctx, r, bin, "profiles")
	if err != nil {
		return nil, err
	}
	profiles, err := ParseProfiles(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("magpie profiles: %w", err)
	}
	return profiles, nil
}

// ParseProfiles parses `magpie profiles`, which prints one "  <name>  <summary>"
// line per profile. The summary lists the models the profile sets, as "<agent>
// <model> · ...", and may end in "· + <library>". With no profile saved, magpie
// prints "no profiles yet · magpie save <name>". That is an empty list, not an error.
func ParseProfiles(in io.Reader) ([]Profile, error) {
	lines, err := textLines(in)
	if err != nil {
		return nil, fmt.Errorf("magpie profiles: %w", err)
	}
	if len(lines) == 1 && strings.HasPrefix(lines[0], "no profiles yet") {
		return []Profile{}, nil
	}
	profiles := []Profile{}
	for n, line := range lines {
		body, ok := strings.CutPrefix(line, "  ")
		if !ok || strings.HasPrefix(body, " ") {
			return nil, fmt.Errorf("%w: profiles line %d", ErrUnrecognized, n+1)
		}
		name, summary := cutColumn(body)
		if name == "" {
			return nil, fmt.Errorf("%w: profiles line %d", ErrUnrecognized, n+1)
		}
		profiles = append(profiles, Profile{Name: name, Summary: summary})
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("%w: no profile rows", ErrUnrecognized)
	}
	return profiles, nil
}
