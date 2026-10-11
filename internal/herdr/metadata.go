package herdr

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
)

// maxTokens is the per-call limit on set and clear entries together, from the
// tokens schema of pane.report_metadata and workspace.report_metadata.
const maxTokens = 16

// tokenName is the token-name rule from the same schema.
var tokenName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ReportPaneTokens sets display-only tokens on a pane under source and clears
// the tokens named in unset. It runs nothing when both are empty.
func (c *Client) ReportPaneTokens(ctx context.Context, pane, source string, set map[string]string, unset []string) error {
	return c.reportTokens(ctx, []string{"pane", "report-metadata"}, pane, source, set, unset)
}

// ReportWorkspaceTokens is ReportPaneTokens for a workspace.
func (c *Client) ReportWorkspaceTokens(ctx context.Context, ws, source string, set map[string]string, unset []string) error {
	return c.reportTokens(ctx, []string{"workspace", "report-metadata"}, ws, source, set, unset)
}

func (c *Client) reportTokens(ctx context.Context, verb []string, id, source string, set map[string]string, unset []string) error {
	if err := checkArg("target", id); err != nil {
		return err
	}
	if err := checkArg("source", source); err != nil {
		return err
	}
	tokens, err := tokenArgs(set, unset)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return nil
	}
	// The CLI wants the positional id before the options.
	args := make([]string, 0, len(verb)+len(tokens)+3)
	args = append(args, verb...)
	args = append(args, id, "--source", source)
	args = append(args, tokens...)
	_, err = c.run(ctx, args...)
	return err
}

// tokenArgs validates the tokens and renders them as --token and --clear-token
// arguments. Names are sorted, so the argv is deterministic.
func tokenArgs(set map[string]string, unset []string) ([]string, error) {
	names := slices.Sorted(maps.Keys(set))
	clears := slices.Compact(slices.Sorted(slices.Values(unset)))
	if n := len(names) + len(clears); n > maxTokens {
		return nil, fmt.Errorf("herdr: %d tokens exceed the limit of %d", n, maxTokens)
	}
	args := make([]string, 0, 2*(len(names)+len(clears)))
	for _, name := range names {
		if !tokenName.MatchString(name) {
			return nil, fmt.Errorf("herdr: invalid token name %q", name)
		}
		args = append(args, "--token", name+"="+set[name])
	}
	for _, name := range clears {
		if !tokenName.MatchString(name) {
			return nil, fmt.Errorf("herdr: invalid token name %q", name)
		}
		if _, dup := set[name]; dup {
			return nil, fmt.Errorf("herdr: token %q is both set and cleared", name)
		}
		args = append(args, "--clear-token", name)
	}
	return args, nil
}
