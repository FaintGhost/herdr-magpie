package herdr

import (
	"context"
	"fmt"
	"slices"
)

// notifySounds are the --sound values that `herdr notification show` accepts.
var notifySounds = []string{"none", "done", "request"}

// Notify shows a herdr notification. An empty body or sound omits that option.
func (c *Client) Notify(ctx context.Context, title, body, sound string) error {
	if err := checkArg("notification title", title); err != nil {
		return err
	}
	if sound != "" && !slices.Contains(notifySounds, sound) {
		return fmt.Errorf("herdr: invalid notification sound %q", sound)
	}
	args := []string{"notification", "show", title}
	if body != "" {
		args = append(args, "--body", body)
	}
	if sound != "" {
		args = append(args, "--sound", sound)
	}
	_, err := c.run(ctx, args...)
	return err
}
