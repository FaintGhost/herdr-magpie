package magpie

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultSessionDays is the window used when Sessions is given days <= 0.
	defaultSessionDays = 14
	// sessionsLimit asks magpie for every session. Its --limit defaults to 20.
	sessionsLimit = 100000
)

// Session is one agent session from `magpie sessions --json`.
type Session struct {
	Agent      string
	ID         string
	CWD        string
	Title      string
	Start      time.Time
	Last       time.Time
	Models     []ModelUsage
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Cost       float64
	Unpriced   int
	Resume     string
	Path       string
	Transcript bool
}

// ModelUsage is one model's share of a Session.
type ModelUsage struct {
	Model      string
	Input      int64
	Output     int64
	CacheRead  int64
	CacheWrite int64
	Cost       float64
	Priced     bool
}

// rawSession mirrors magpie's JSON. Unknown fields are ignored. Timestamps stay
// strings so that an empty one means "no time" instead of failing the list.
type rawSession struct {
	Agent      string     `json:"agent,omitempty"`
	ID         string     `json:"id,omitempty"`
	CWD        string     `json:"cwd,omitempty"`
	Title      string     `json:"title,omitempty"`
	Start      string     `json:"start,omitempty"`
	Last       string     `json:"last,omitempty"`
	Models     []rawModel `json:"models,omitempty"`
	Input      int64      `json:"input,omitzero"`
	Output     int64      `json:"output,omitzero"`
	CacheRead  int64      `json:"cache_read,omitzero"`
	CacheWrite int64      `json:"cache_write,omitzero"`
	Cost       float64    `json:"cost,omitzero"`
	Unpriced   int        `json:"unpriced,omitzero"`
	Resume     string     `json:"resume,omitempty"`
	Path       string     `json:"path,omitempty"`
	Transcript bool       `json:"transcript,omitzero"`
}

type rawModel struct {
	Model      string  `json:"model,omitempty"`
	Input      int64   `json:"input,omitzero"`
	Output     int64   `json:"output,omitzero"`
	CacheRead  int64   `json:"cache_read,omitzero"`
	CacheWrite int64   `json:"cache_write,omitzero"`
	Cost       float64 `json:"cost,omitzero"`
	Priced     bool    `json:"priced,omitzero"`
}

// Sessions returns the sessions whose last activity is within the last days
// calendar days (14 when days <= 0), most recent first. It runs
// `magpie sessions --json --limit N` and applies the window itself, because
// with --days, --json prints a summary rather than a list.
func Sessions(ctx context.Context, r Runner, bin string, days int) ([]Session, error) {
	return sessionsSince(ctx, r, bin, days, time.Now())
}

// sessionsSince is Sessions with the clock injected for tests.
func sessionsSince(ctx context.Context, r Runner, bin string, days int, now time.Time) ([]Session, error) {
	if days <= 0 {
		days = defaultSessionDays
	}
	if bin == "" {
		bin = "magpie"
	}
	ctx, cancel := context.WithTimeout(ctx, cliCap(ctx))
	defer cancel()
	out, err := r.Run(ctx, bin, "sessions", "--json", "--limit", strconv.Itoa(sessionsLimit))
	if err != nil {
		// ExecRunner errors already start with "magpie sessions:", so do not repeat it.
		return nil, err
	}
	var raw []rawSession
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("magpie sessions: decode: %w", err)
	}
	cutoff := now.AddDate(0, 0, -days)
	sessions := make([]Session, 0, len(raw))
	for _, rs := range raw {
		s, err := rs.session()
		if err != nil {
			return nil, fmt.Errorf("magpie sessions: %w", err)
		}
		if s.Last.Before(cutoff) {
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// session converts the wire form and parses its timestamps.
func (rs rawSession) session() (Session, error) {
	start, err := parseRFC3339(rs.Start)
	if err != nil {
		return Session{}, fmt.Errorf("start: %w", err)
	}
	last, err := parseRFC3339(rs.Last)
	if err != nil {
		return Session{}, fmt.Errorf("last: %w", err)
	}
	models := make([]ModelUsage, 0, len(rs.Models))
	for _, m := range rs.Models {
		models = append(models, ModelUsage{
			Model:      m.Model,
			Input:      m.Input,
			Output:     m.Output,
			CacheRead:  m.CacheRead,
			CacheWrite: m.CacheWrite,
			Cost:       m.Cost,
			Priced:     m.Priced,
		})
	}
	return Session{
		Agent:      rs.Agent,
		ID:         rs.ID,
		CWD:        rs.CWD,
		Title:      rs.Title,
		Start:      start,
		Last:       last,
		Models:     models,
		Input:      rs.Input,
		Output:     rs.Output,
		CacheRead:  rs.CacheRead,
		CacheWrite: rs.CacheWrite,
		Cost:       rs.Cost,
		Unpriced:   rs.Unpriced,
		Resume:     rs.Resume,
		Path:       rs.Path,
		Transcript: rs.Transcript,
	}, nil
}

// parseRFC3339 parses a magpie timestamp. An empty value is the zero time.
func parseRFC3339(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

// SessionsByKey indexes sessions by lower-cased ID, the form herdr's SessionKey
// produces, so that the two can be compared case-insensitively.
func SessionsByKey(sessions []Session) map[string]Session {
	m := make(map[string]Session, len(sessions))
	for _, s := range sessions {
		if s.ID == "" {
			continue
		}
		m[strings.ToLower(s.ID)] = s
	}
	return m
}
