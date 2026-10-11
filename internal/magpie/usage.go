package magpie

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Request is one row of `magpie usage --csv`.
type Request struct {
	Time            time.Time
	Agent           string
	RequestedModel  string
	Provider        string
	Host            string
	Model           string
	ServedModel     string
	Swapped         bool
	Error           bool
	Rejected        bool
	CostUSD         float64
	Status          int
	Session         string
	ProviderAccount string
	RequestID       string
	ErrorMessage    string
	ErrorType       string
	Source          string
	TTFT            time.Duration // time to first token; 0 when unknown (usage log ttft_ms)
}

// UsageStats counts the rows of one usage CSV.
type UsageStats struct {
	Rows    int // rows decoded into a Request
	Skipped int // malformed rows dropped: wrong field count, bad typed value, or CSV syntax error
}

// Usage runs `magpie usage --csv <window>` (window defaults to "today") and parses the result.
func Usage(ctx context.Context, r Runner, bin, window string) ([]Request, UsageStats, error) {
	if window == "" {
		window = "today"
	}
	if strings.HasPrefix(window, "-") {
		return nil, UsageStats{}, fmt.Errorf("magpie usage: invalid window %q", window)
	}
	if bin == "" {
		bin = "magpie"
	}
	ctx, cancel := context.WithTimeout(ctx, cliCap(ctx))
	defer cancel()
	out, err := r.Run(ctx, bin, "usage", "--csv", window)
	if err != nil {
		// ExecRunner errors already start with "magpie usage:", so do not repeat it.
		return nil, UsageStats{}, err
	}
	reqs, stats, err := ParseUsageCSV(bytes.NewReader(out))
	if err != nil {
		return nil, stats, fmt.Errorf("magpie usage: %w", err)
	}
	return reqs, stats, nil
}

// ParseUsageCSV decodes `magpie usage --csv` output. Fields are found by header
// name, so column order does not matter and unknown columns are ignored. A
// missing column leaves its field zero. A row is skipped and counted when its
// field count differs from the header, a non-empty typed value does not parse,
// or the CSV is malformed. Empty input has no rows and no error.
func ParseUsageCSV(in io.Reader) ([]Request, UsageStats, error) {
	cr := csv.NewReader(in)
	cr.FieldsPerRecord = -1 // checked per row below, so one short row does not abort the file
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return nil, UsageStats{}, nil
	}
	if err != nil {
		return nil, UsageStats{}, fmt.Errorf("usage csv header: %w", err)
	}
	idx := make(map[string]int, len(header))
	for i, name := range header {
		key := strings.TrimSpace(name)
		if _, seen := idx[key]; !seen {
			idx[key] = i
		}
	}

	var reqs []Request
	var stats UsageStats
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if _, syntax := errors.AsType[*csv.ParseError](err); syntax {
			stats.Skipped++
			continue
		}
		if err != nil {
			return nil, stats, fmt.Errorf("usage csv: %w", err)
		}
		if len(rec) != len(header) {
			stats.Skipped++
			continue
		}
		req, err := decodeRequest(rec, idx)
		if err != nil {
			stats.Skipped++
			continue
		}
		reqs = append(reqs, req)
	}
	stats.Rows = len(reqs)
	return reqs, stats, nil
}

// decodeRequest builds a Request from one record. It fails only when a
// non-empty typed value does not parse.
func decodeRequest(rec []string, idx map[string]int) (Request, error) {
	field := func(name string) string {
		if i, ok := idx[name]; ok {
			return rec[i]
		}
		return ""
	}
	var (
		req Request
		err error
	)
	if req.Time, err = parseRFC3339(field("time")); err != nil {
		return Request{}, fmt.Errorf("time: %w", err)
	}
	req.Agent = field("agent")
	req.RequestedModel = field("requested_model")
	req.Provider = field("provider")
	req.Host = field("host")
	req.Model = field("model")
	req.ServedModel = field("served_model")
	if req.Swapped, err = parseBool(field("swapped")); err != nil {
		return Request{}, fmt.Errorf("swapped: %w", err)
	}
	if req.Error, err = parseBool(field("error")); err != nil {
		return Request{}, fmt.Errorf("error: %w", err)
	}
	if req.Rejected, err = parseBool(field("rejected")); err != nil {
		return Request{}, fmt.Errorf("rejected: %w", err)
	}
	if req.CostUSD, err = parseFloat(field("cost_usd")); err != nil {
		return Request{}, fmt.Errorf("cost_usd: %w", err)
	}
	if req.Status, err = parseInt(field("status")); err != nil {
		return Request{}, fmt.Errorf("status: %w", err)
	}
	req.Session = field("session")
	req.ProviderAccount = field("provider_account")
	req.RequestID = field("request_id")
	req.ErrorMessage = field("error_message")
	req.ErrorType = field("error_type")
	req.Source = field("source")
	return req, nil
}

// parseBool, parseInt and parseFloat treat an empty string as the zero value.
func parseBool(s string) (bool, error) {
	if s == "" {
		return false, nil
	}
	return strconv.ParseBool(s)
}

func parseInt(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.Atoi(s)
}

func parseFloat(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

// LatestBySession returns the newest request of each session, keyed by
// lower-cased session ID. Requests without a session are ignored.
func LatestBySession(reqs []Request) map[string]Request {
	latest := make(map[string]Request)
	for _, r := range reqs {
		if r.Session == "" {
			continue
		}
		key := strings.ToLower(r.Session)
		if cur, ok := latest[key]; !ok || r.Time.After(cur.Time) {
			latest[key] = r
		}
	}
	return latest
}
