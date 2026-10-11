package magpie

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

// agentIDs maps the names `magpie ls` prints to the ids `magpie <id>` takes. Each
// entry was checked by running `magpie <id>` and seeing that agent's row. An agent
// that is not listed has an empty ID.
var agentIDs = map[string]string{
	"Claude Code":      "claude",
	"Codex":            "codex",
	"omp":              "omp",
	"Hermes Agent":     "hermes",
	"Kimi Code":        "kimi",
	"DeepSeek Harness": "dsh",
}

const (
	hiddenSuffix = " hidden" // magpie appends it to the name of an agent its app hides
	addedStatus  = "magpie " // starts the values of an agent magpie has not been added to
	separator    = "·"       // between a row's values
)

// Agents runs `magpie ls` and parses the agents it lists.
func Agents(ctx context.Context, r Runner, bin string) ([]AgentInfo, error) {
	out, err := runCLI(ctx, r, bin, "ls")
	if err != nil {
		return nil, err
	}
	agents, err := ParseLS(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("magpie ls: %w", err)
	}
	return agents, nil
}

// ParseLS parses `magpie ls`. A row reads
//
//	<name>  <model>  ·  <label> <value>  ·  ...  <config path>
//
// with its columns padded to line up. Columns are separated by runs of two or
// more spaces, while a name or label may hold single spaces ("Claude Code",
// "subagent model"). The model comes first and has no label. A field's value is
// the last word of the field, and "—" is an unset value, as magpie prints it.
// An agent magpie has not been added to shows "magpie <id> add" or "magpie
// added" where its fields would be, so its row has no fields.
func ParseLS(in io.Reader) ([]AgentInfo, error) {
	lines, err := textLines(in)
	if err != nil {
		return nil, fmt.Errorf("magpie ls: %w", err)
	}
	var agents []AgentInfo
	for n, line := range lines {
		a, ok := parseLSRow(line)
		if !ok {
			return nil, fmt.Errorf("%w: ls line %d", ErrUnrecognized, n+1)
		}
		agents = append(agents, a)
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("%w: no agent rows", ErrUnrecognized)
	}
	return agents, nil
}

// parseLSRow parses one row of `magpie ls`. It reports false when the line is not a row.
func parseLSRow(line string) (AgentInfo, bool) {
	body, ok := strings.CutPrefix(line, "  ")
	if !ok {
		return AgentInfo{}, false
	}
	cols := columns(body)
	if len(cols) < 2 {
		return AgentInfo{}, false
	}
	name, hidden := strings.CutSuffix(cols[0], hiddenSuffix)
	a := AgentInfo{Name: name, ID: agentIDs[name], Hidden: hidden}
	cols = cols[1:]
	if last := cols[len(cols)-1]; isPath(last) {
		a.ConfigPath = last
		cols = cols[:len(cols)-1]
	}
	if len(cols) == 0 {
		return AgentInfo{}, false
	}
	if strings.HasPrefix(cols[0], addedStatus) {
		return a, true
	}
	model := cols[0]
	if strings.Contains(model, " ") || model == separator {
		return AgentInfo{}, false
	}
	a.Fields = []AgentField{{Name: "model", Value: model}}
	for _, col := range cols[1:] {
		switch {
		case col == separator:
		case strings.Contains(col, separator):
			// A note magpie adds to a row, such as the fail-over note on Codex. It is not a field.
		default:
			label, value, ok := cutLastWord(col)
			if !ok {
				return AgentInfo{}, false
			}
			a.Fields = append(a.Fields, AgentField{Name: label, Value: value})
		}
	}
	return a, true
}

// cutLastWord splits "label value" at its last space.
func cutLastWord(s string) (label, value string, ok bool) {
	i := strings.LastIndexByte(s, ' ')
	if i <= 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// isPath reports whether a column is a config path as magpie prints it: under
// the home folder (~) or absolute.
func isPath(s string) bool {
	return strings.HasPrefix(s, "~") || strings.HasPrefix(s, "/")
}
