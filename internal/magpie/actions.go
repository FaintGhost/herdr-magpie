package magpie

import (
	"context"
	"fmt"
)

// UseProfile applies a saved profile: `magpie use <name>`.
func UseProfile(ctx context.Context, r Runner, bin, name string) error {
	if err := checkArg(name); err != nil {
		return fmt.Errorf("magpie use: profile name: %w", err)
	}
	_, err := runCLI(ctx, r, bin, "use", name)
	return err
}

// SaveProfile saves the current setup as a profile: `magpie save <name>`.
func SaveProfile(ctx context.Context, r Runner, bin, name string) error {
	if err := checkArg(name); err != nil {
		return fmt.Errorf("magpie save: profile name: %w", err)
	}
	_, err := runCLI(ctx, r, bin, "save", name)
	return err
}

// SwitchAccount makes user the account agent uses: `magpie accounts switch <agent> <user>`.
func SwitchAccount(ctx context.Context, r Runner, bin, agent, user string) error {
	if err := checkArg(agent); err != nil {
		return fmt.Errorf("magpie accounts switch: agent: %w", err)
	}
	if err := checkArg(user); err != nil {
		return fmt.Errorf("magpie accounts switch: user: %w", err)
	}
	_, err := runCLI(ctx, r, bin, "accounts", "switch", agent, user)
	return err
}
