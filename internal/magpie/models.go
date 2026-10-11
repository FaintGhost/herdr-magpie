package magpie

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"strings"
)

// routingGroupsTitle is the title magpie prints above its routing groups.
const routingGroupsTitle = "Routing groups"

// Models runs `magpie models <agentID>` and parses the models the agent can use.
func Models(ctx context.Context, r Runner, bin, agentID string) ([]ModelOption, error) {
	if err := checkArg(agentID); err != nil {
		return nil, fmt.Errorf("magpie models: agent id: %w", err)
	}
	out, err := runCLI(ctx, r, bin, "models", agentID)
	if err != nil {
		return nil, err
	}
	models, err := ParseModels(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("magpie models: %w", err)
	}
	return models, nil
}

// ParseModels parses `magpie models <agent>`. Rows sit under a title such as
// "  DeepSeek" or "  Routing groups":
//
//	<id>  <display name>  <effort>/<effort>/...
//
// magpie leaves out the display name when it equals the id, and the efforts
// when the model has none. Notes and hints surround the rows: lines with ":" or
// "·", indented hints, and the gateway URL that ends the listing. They are
// skipped. A title applies only to the row right after it.
func ParseModels(in io.Reader) ([]ModelOption, error) {
	lines, err := textLines(in)
	if err != nil {
		return nil, fmt.Errorf("magpie models: %w", err)
	}
	var (
		models  []ModelOption
		group   string // the title above the current rows
		title   string // a candidate title, kept only while the next line is a row
		sawNone bool   // magpie said the agent has no models to show
	)
	for n, line := range lines {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		body := line[indent:]
		switch {
		case indent == 0:
			if !isNoModelsLine(line) {
				return nil, fmt.Errorf("%w: models line %d", ErrUnrecognized, n+1)
			}
			sawNone, title = true, ""
		case indent >= 4:
			// A hint under a note, such as "    magpie provider models <id> +<model> exposes one".
			if !strings.HasPrefix(body, "magpie ") {
				return nil, fmt.Errorf("%w: models line %d", ErrUnrecognized, n+1)
			}
			title = ""
		case indent == 2:
			if strings.Contains(body, "://") {
				title = "" // the gateway URL that closes the listing
				continue
			}
			cols := columns(body)
			if isModelID(cols[0]) {
				m, ok := parseModelRow(cols)
				if !ok {
					return nil, fmt.Errorf("%w: models line %d", ErrUnrecognized, n+1)
				}
				if title != "" {
					group, title = title, ""
				}
				m.Group = group
				m.IsRoutingGroup = group == routingGroupsTitle
				models = append(models, m)
				continue
			}
			title = ""
			if len(cols) == 1 && !strings.ContainsAny(body, "/:·") {
				title = body
				continue
			}
			if !strings.ContainsAny(body, ":·") {
				return nil, fmt.Errorf("%w: models line %d", ErrUnrecognized, n+1)
			}
			// Otherwise a note, such as "  OpenCode Go: 36 more models in its list, ...".
		default:
			return nil, fmt.Errorf("%w: models line %d", ErrUnrecognized, n+1)
		}
	}
	if len(models) == 0 {
		if sawNone {
			return []ModelOption{}, nil
		}
		return nil, fmt.Errorf("%w: no model rows", ErrUnrecognized)
	}
	return models, nil
}

// isModelID reports whether a column is a model id: one word containing "/".
// The gateway URL is not one, since it is excluded before this check.
func isModelID(s string) bool {
	return !strings.Contains(s, " ") && strings.Contains(s, "/")
}

// parseModelRow parses the columns of a model row after the id: at most a
// display name and an effort list, in that order.
func parseModelRow(cols []string) (ModelOption, bool) {
	m := ModelOption{ID: cols[0]}
	switch rest := cols[1:]; len(rest) {
	case 0:
	case 1:
		if looksLikeEfforts(rest[0]) {
			m.Efforts = efforts(rest[0])
		} else {
			m.DisplayName = rest[0]
		}
	case 2:
		if !looksLikeEfforts(rest[1]) {
			return ModelOption{}, false
		}
		m.DisplayName, m.Efforts = rest[0], efforts(rest[1])
	default:
		return ModelOption{}, false
	}
	return m, true
}

// looksLikeEfforts reports whether s reads as an effort list: lower-case words
// joined by "/", as in "low/high/max". A single word is not counted, so a
// one-word display name is not mistaken for efforts.
func looksLikeEfforts(s string) bool {
	if !strings.Contains(s, "/") {
		return false
	}
	for part := range strings.SplitSeq(s, "/") {
		if part == "" {
			return false
		}
		for _, c := range part {
			if c < 'a' || c > 'z' {
				return false
			}
		}
	}
	return true
}

func efforts(s string) []string {
	return slices.Collect(strings.SplitSeq(s, "/"))
}

// isNoModelsLine reports whether an unindented line is magpie's message for an
// agent with no models to show.
func isNoModelsLine(line string) bool {
	return strings.HasPrefix(line, "no models yet") ||
		(strings.HasPrefix(line, "! ") && strings.Contains(line, "is shown none of them"))
}
