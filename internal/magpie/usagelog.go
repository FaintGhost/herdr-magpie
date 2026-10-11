package magpie

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// DefaultUsageTail is how many bytes of the usage log TailUsage reads from its
// end when the caller passes no limit. At the row sizes magpie writes, that is
// far more than the ten-minute storm window needs.
const DefaultUsageTail = 4 << 20

// usageBlockSize is how much of the log one read takes, counted from the end.
const usageBlockSize = 64 << 10

// ErrNoUsageLog means the usage log does not exist. Callers treat it as storm
// alerts being off rather than as an error to report on every tick.
var ErrNoUsageLog = errors.New("usage log not found")

// TailStats counts what one tail read decoded.
type TailStats struct {
	Rows    int // rows at or after since
	Skipped int // complete lines that do not decode as usage rows
}

// usageLine holds the fields of one usage.jsonl line that the plugin reads. The
// log is written by magpie's gateway and is not a stable contract, so fields are
// matched by name and every other field is ignored.
//
// The meanings follow magpie's usage record (internal/usage.Record) and were
// checked against the log on 2026-10-12: req is the model the agent asked for,
// often in the form "provider/model"; model is the provider's model id, and
// served differs from it only on the 11 swapped rows; rid is the request id, and
// only a few rows carry it.
type usageLine struct {
	T        string `json:"t"` // RFC 3339 with offset
	Agent    string `json:"agent"`
	Provider string `json:"provider"`
	Host     string `json:"host"`
	Model    string `json:"model"`  // the provider's model id
	Req      string `json:"req"`    // the model the agent asked for; missing in records from before it was kept
	Served   string `json:"served"` // the model the vendor's reply named, when it named one
	Status   int    `json:"status"`
	Err      string `json:"err"` // provider error message; empty on success
	ErrType  string `json:"err_type"`
	RID      string `json:"rid"`     // request id; empty on most rows
	TTFTMS   int64  `json:"ttft_ms"` // ms from the request to its first content; 0 when not streamed
}

// TailUsage returns the usage rows in the log at path whose time is at or after
// since, in file order. It reads at most maxBytes from the end of the file, or
// DefaultUsageTail when maxBytes is not positive. See TailUsageStats.
func TailUsage(path string, since time.Time, maxBytes int64) ([]Request, error) {
	reqs, _, err := TailUsageStats(path, since, maxBytes)
	return reqs, err
}

// TailUsageStats is TailUsage that also reports how many lines decoded and how
// many complete lines did not. The file is read in blocks from its end, so the
// cost is bounded by maxBytes however large the log grows.
//
// Rows are not in strict time order: magpie writes a row when its request
// finishes, while the row carries the request's own time, so an older row can
// follow a newer one. The whole window is therefore decoded and filtered, not
// read backwards until the first row older than since.
//
// The first line of a window that starts inside a line is dropped, and so is an
// unterminated last line, because magpie may still be writing it. Blank lines
// are ignored.
func TailUsageStats(path string, since time.Time, maxBytes int64) ([]Request, TailStats, error) {
	return tailUsage(path, since, maxBytes, usageBlockSize)
}

// tailUsage is TailUsageStats with the read block size as a parameter, so tests
// can exercise line boundaries that fall between blocks.
func tailUsage(path string, since time.Time, maxBytes, block int64) ([]Request, TailStats, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultUsageTail
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, TailStats{}, fmt.Errorf("%w: %s", ErrNoUsageLog, path)
	}
	if err != nil {
		return nil, TailStats{}, fmt.Errorf("usage log: %w", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, TailStats{}, fmt.Errorf("usage log: %w", err)
	}

	size := fi.Size()
	start := max(0, size-maxBytes)
	window := make([]byte, size-start)
	for end := size; end > start; {
		from := max(start, end-block)
		if _, err := f.ReadAt(window[from-start:end-start], from); err != nil {
			return nil, TailStats{}, fmt.Errorf("usage log: %w", err)
		}
		end = from
	}

	data := window
	if start > 0 {
		var before [1]byte
		if _, err := f.ReadAt(before[:], start-1); err != nil {
			return nil, TailStats{}, fmt.Errorf("usage log: %w", err)
		}
		if before[0] != '\n' {
			// The window begins inside a line, so the text before its first newline is a fragment.
			_, rest, found := bytes.Cut(data, []byte{'\n'})
			if !found {
				return nil, TailStats{}, nil
			}
			data = rest
		}
	}
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		return nil, TailStats{}, nil
	}
	data = data[:last+1] // anything after the last newline is unterminated

	var reqs []Request
	var stats TailStats
	for line := range bytes.SplitSeq(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		req, err := decodeUsageLine(line)
		if err != nil {
			stats.Skipped++
			continue
		}
		if req.Time.Before(since) {
			continue
		}
		reqs = append(reqs, req)
		stats.Rows++
	}
	return reqs, stats, nil
}

// decodeUsageLine maps one usage.jsonl line to a Request. Every row in the log
// comes from the gateway, so Source stays empty: `magpie usage --csv` leaves the
// source column empty for gateway rows, and "log" marks rows from agent logs.
// Session, cost and account fields are not in the log and are left zero.
//
// Swapped compares the vendor's answer with the provider model the gateway sent,
// not with the requested model: a routing group or a "provider/" prefix makes the
// request differ from its provider id, and that is not a swap. A record from
// before req was kept names only the provider model, which then stands for the request.
func decodeUsageLine(line []byte) (Request, error) {
	var u usageLine
	if err := json.Unmarshal(line, &u); err != nil {
		return Request{}, err
	}
	at, err := time.Parse(time.RFC3339Nano, u.T)
	if err != nil {
		return Request{}, fmt.Errorf("time: %w", err)
	}
	return Request{
		Time:           at,
		Agent:          u.Agent,
		RequestedModel: cmp.Or(u.Req, u.Model),
		ServedModel:    u.Served,
		Swapped:        u.Served != "" && u.Served != u.Model,
		Provider:       u.Provider,
		Host:           u.Host,
		Error:          u.Err != "",
		Status:         u.Status,
		ErrorType:      u.ErrType,
		ErrorMessage:   u.Err,
		RequestID:      u.RID,
		TTFT:           time.Duration(u.TTFTMS) * time.Millisecond,
	}, nil
}
