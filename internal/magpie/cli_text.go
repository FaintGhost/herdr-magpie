package magpie

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"unicode"
)

// ErrUnrecognized reports magpie output that the parsers do not understand. They
// return it rather than guess, so the panel can say so.
var ErrUnrecognized = errors.New("unrecognized magpie output")

// ansiEscape matches the CSI sequences magpie's CLI styles its text with. A
// pipe gets plain text, but a forced-colour environment would add them.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// runCLI runs the magpie CLI with args under the call's cap (see cliCap). An empty bin means "magpie".
func runCLI(ctx context.Context, r Runner, bin string, args ...string) ([]byte, error) {
	if bin == "" {
		bin = "magpie"
	}
	ctx, cancel := context.WithTimeout(ctx, cliCap(ctx))
	defer cancel()
	return r.Run(ctx, bin, args...)
}

// textLines reads magpie's text output as lines. Styling escapes and trailing
// spaces are removed, and blank lines are dropped. Leading spaces stay, since
// they mark indentation.
func textLines(in io.Reader) ([]string, error) {
	data, err := io.ReadAll(in)
	if err != nil {
		return nil, err
	}
	var lines []string
	for line := range strings.SplitSeq(ansiEscape.ReplaceAllString(string(data), ""), "\n") {
		if line = strings.TrimRight(line, " \t\r"); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// columns splits a line at runs of two or more spaces. Magpie pads its columns
// with spaces and joins one row's values with "  ·  ", so a single space stays
// inside a column, as in "Claude Code" or "effort high".
func columns(s string) []string {
	var cols []string
	for part := range strings.SplitSeq(s, "  ") {
		if part = strings.TrimSpace(part); part != "" {
			cols = append(cols, part)
		}
	}
	return cols
}

// cutColumn splits s at its first run of two or more spaces and trims both
// halves. It suits a line whose last column is free text.
func cutColumn(s string) (string, string) {
	head, tail, _ := strings.Cut(s, "  ")
	return strings.TrimSpace(head), strings.TrimSpace(tail)
}

// checkArg rejects an argument the CLI could misread: an empty one, one that
// starts with "-" (the CLI would take it for a flag), or one with a control character.
func checkArg(s string) error {
	switch {
	case s == "":
		return errors.New("empty")
	case strings.HasPrefix(s, "-"):
		return errors.New("starts with -")
	case strings.IndexFunc(s, unicode.IsControl) >= 0:
		return errors.New("has a control character")
	}
	return nil
}
