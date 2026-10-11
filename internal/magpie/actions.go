package magpie

import (
	"context"
	"fmt"
	"time"
)

// writeTimeout caps one write command. A profile apply or an account switch
// rewrites magpie's own files, so it gets more time than a read.
const writeTimeout = 60 * time.Second

// runWrite runs a write command under writeTimeout. The command is not cancelled
// when ctx is: quitting the panel must not stop magpie halfway through a write,
// so only the timeout bounds it. An empty bin means "magpie".
func runWrite(ctx context.Context, r Runner, bin string, args ...string) ([]byte, error) {
	if bin == "" {
		bin = "magpie"
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	return r.Run(ctx, bin, args...)
}

// UseProfile applies a saved profile: `magpie use <name>`.
func UseProfile(ctx context.Context, r Runner, bin, name string) error {
	if err := checkArg(name); err != nil {
		return fmt.Errorf("magpie use: profile name: %w", err)
	}
	_, err := runWrite(ctx, r, bin, "use", name)
	return err
}

// SaveProfile saves the current setup as a profile: `magpie save <name>`.
func SaveProfile(ctx context.Context, r Runner, bin, name string) error {
	if err := checkArg(name); err != nil {
		return fmt.Errorf("magpie save: profile name: %w", err)
	}
	_, err := runWrite(ctx, r, bin, "save", name)
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
	_, err := runWrite(ctx, r, bin, "accounts", "switch", agent, user)
	return err
}
