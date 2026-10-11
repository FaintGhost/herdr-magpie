package magpie

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// logBase is the fixed time of the synthetic usage rows. Rows are placed by
// their offset in minutes from it.
var logBase = time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)

// logRow is one synthetic usage.jsonl line, shaped like the gateway's rows, with
// a trailing newline.
func logRow(t *testing.T, minutes int, provider string, status int, errText string) string {
	t.Helper()
	at := logBase.Add(time.Duration(minutes) * time.Minute)
	fields := map[string]any{
		"t":        at.Format(time.RFC3339Nano),
		"agent":    "alpha",
		"provider": provider,
		"host":     "gw.example.test",
		"model":    "m-a",
		"served":   "m-a",
		"status":   status,
		"err":      errText,
		"req":      provider + "/m-a",
		"rid":      "rid-" + at.Format("150405"),
	}
	if errText != "" {
		fields["err_type"] = "RateLimit"
	}
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(data) + "\n"
}

// writeLog writes content to a new usage.jsonl in a temporary directory.
func writeLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// minutesOf returns each request's offset in minutes from logBase, in order.
func minutesOf(reqs []Request) []int {
	out := make([]int, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, int(r.Time.Sub(logBase)/time.Minute))
	}
	return out
}

func TestTailUsageStats(t *testing.T) {
	ok := func(min int) string { return logRow(t, min, "p1", 200, "") }
	all := ok(-30) + ok(-5) + ok(-1)
	tests := []struct {
		name        string
		content     string
		since       time.Time
		maxBytes    int64
		want        []int
		wantSkipped int
	}{
		{name: "small file, no limit", content: all, want: []int{-30, -5, -1}},
		{
			name: "since drops older rows", content: all,
			since: logBase.Add(-10 * time.Minute), want: []int{-5, -1},
		},
		{
			name: "file order is kept, not time order", content: ok(-1) + ok(-20) + ok(-2),
			since: logBase.Add(-10 * time.Minute), want: []int{-1, -2},
		},
		{
			name: "bad lines are skipped and counted",
			content: ok(-5) + "not json\n" + "[1,2]\n" + `{"t":"not a time","status":200}` + "\n" +
				`{"t":"2026-10-12T00:55:00Z","status":"oops"}` + "\n" + ok(-1),
			want: []int{-5, -1}, wantSkipped: 4,
		},
		{
			name: "blank lines are ignored", content: ok(-5) + "\n\n  \n" + ok(-1),
			want: []int{-5, -1},
		},
		{
			name: "unterminated last line is dropped without counting it", content: ok(-5) + `{"t":"2026-10-12T00:59:`,
			want: []int{-5},
		},
		{
			name:    "window cut inside a line drops the fragment",
			content: all, maxBytes: int64(len(ok(-5)) + len(ok(-1)) + 7),
			want: []int{-5, -1},
		},
		{
			name:    "window starting on a line boundary keeps its first line",
			content: all, maxBytes: int64(len(ok(-5)) + len(ok(-1))),
			want: []int{-5, -1},
		},
		{
			name:    "window smaller than one line yields nothing",
			content: all, maxBytes: 10,
		},
		{
			name: "newest rows survive a small limit",
			content: func() string {
				var b strings.Builder
				for m := -50; m <= -1; m++ {
					b.WriteString(ok(m))
				}
				return b.String()
			}(),
			maxBytes: int64(3*len(ok(-1)) + 10),
			want:     []int{-3, -2, -1},
		},
		{name: "non-positive limit reads the default window", content: ok(-30) + ok(-1), maxBytes: 0, want: []int{-30, -1}},
		{name: "empty file", content: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs, stats, err := TailUsageStats(writeLog(t, tt.content), tt.since, tt.maxBytes)
			if err != nil {
				t.Fatalf("TailUsageStats() error = %v", err)
			}
			if got := minutesOf(reqs); !slices.Equal(got, tt.want) {
				t.Errorf("rows at minutes %v, want %v", got, tt.want)
			}
			if stats.Rows != len(reqs) || stats.Skipped != tt.wantSkipped {
				t.Errorf("stats = %+v, want Rows %d Skipped %d", stats, len(reqs), tt.wantSkipped)
			}
		})
	}
}

func TestTailUsageMissingFile(t *testing.T) {
	_, _, err := TailUsageStats(filepath.Join(t.TempDir(), "usage.jsonl"), time.Time{}, 0)
	if !errors.Is(err, ErrNoUsageLog) {
		t.Errorf("error = %v, want ErrNoUsageLog", err)
	}
	if _, err := TailUsage(filepath.Join(t.TempDir(), "usage.jsonl"), time.Time{}, 0); !errors.Is(err, ErrNoUsageLog) {
		t.Errorf("TailUsage error = %v, want ErrNoUsageLog", err)
	}
}

// TestTailUsageBlockSizes checks that the block size only changes how the file
// is read: every block size gives the same rows and counts, including sizes
// that put line boundaries between blocks.
func TestTailUsageBlockSizes(t *testing.T) {
	var b strings.Builder
	for i := range 40 {
		provider := strings.Repeat("x", i%7+1)
		status := 200
		if i%5 == 0 {
			status = 429
		}
		b.WriteString(logRow(t, i-40, provider, status, ""))
		if i%9 == 4 {
			b.WriteString("not json\n")
		}
	}
	path := writeLog(t, b.String())
	size := int64(b.Len())
	for _, limit := range []int64{size, size / 2, size - 13, 100} {
		want, wantStats, err := tailUsage(path, time.Time{}, limit, usageBlockSize)
		if err != nil {
			t.Fatal(err)
		}
		for _, block := range []int64{1, 2, 3, 7, 64, 1000} {
			got, gotStats, err := tailUsage(path, time.Time{}, limit, block)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(minutesOf(got), minutesOf(want)) || gotStats != wantStats {
				t.Errorf("limit %d block %d: rows %v stats %+v; want rows %v stats %+v",
					limit, block, minutesOf(got), gotStats, minutesOf(want), wantStats)
			}
		}
	}
	whole, stats, err := tailUsage(path, time.Time{}, size, usageBlockSize)
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) != 40 || stats.Skipped != 4 {
		t.Errorf("whole file: %d rows, %d skipped; want 40 rows, 4 skipped", len(whole), stats.Skipped)
	}
}

// TestTailUsageReadsOnlyTheWindow checks that a limit keeps the read to its tail
// even when the file holds far more rows.
func TestTailUsageReadsOnlyTheWindow(t *testing.T) {
	var b strings.Builder
	for m := -999; m <= 0; m++ {
		b.WriteString(logRow(t, m, "p1", 200, ""))
	}
	path := writeLog(t, b.String())
	reqs, err := TailUsage(path, time.Time{}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) == 0 || len(reqs) > 4096/len(logRow(t, 0, "p1", 200, "")) {
		t.Fatalf("got %d rows from a 4096-byte window", len(reqs))
	}
	if last := reqs[len(reqs)-1]; last.Time.Sub(logBase) != 0 {
		t.Errorf("newest row at %v, want the file's last row", last.Time.Sub(logBase))
	}
	if got, want := minutesOf(reqs)[0], -(len(reqs) - 1); got != want {
		t.Errorf("oldest kept row at minute %d, want %d: the kept rows should be the newest ones", got, want)
	}
}

func TestTailUsageMapsRows(t *testing.T) {
	path := writeLog(t, `{"t":"2026-10-12T00:59:00.123+09:00","agent":"alpha","provider":"p9","host":"gw.example.test",`+
		`"model":"m-a","req":"p9/m-req","served":"m-b","status":429,"err":"boom","err_type":"RateLimit","rid":"r1",`+
		`"ttft_ms":1500,"ep":"/v1/x"}`+"\n"+
		`{"t":"2026-10-12T00:59:01+09:00","agent":"beta","provider":"p2","model":"m-c","status":200}`+"\n")
	reqs, err := TailUsage(path, time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 {
		t.Fatalf("got %d rows, want 2", len(reqs))
	}
	want := Request{
		Time:           time.Date(2026, 10, 11, 15, 59, 0, 123000000, time.UTC),
		Agent:          "alpha",
		RequestedModel: "p9/m-req",
		ServedModel:    "m-b",
		Swapped:        true,
		Provider:       "p9",
		Host:           "gw.example.test",
		Error:          true,
		Status:         429,
		ErrorType:      "RateLimit",
		ErrorMessage:   "boom",
		RequestID:      "r1",
		TTFT:           1500 * time.Millisecond,
	}
	got := reqs[0]
	if !got.Time.Equal(want.Time) {
		t.Errorf("time = %v, want %v", got.Time, want.Time)
	}
	got.Time, want.Time = time.Time{}, time.Time{}
	if got != want {
		t.Errorf("row = %+v\nwant %+v", got, want)
	}
	if ok := reqs[1]; ok.Swapped || ok.Error || ok.Status != 200 || ok.ServedModel != "" {
		t.Errorf("successful row = %+v; want no swap and no error", ok)
	}
}

// TestUsageLineMapping checks which fields a usage.jsonl row fills. The cases mirror
// the shapes seen in the real log: a "provider/model" request answered by the
// same provider model, a routing group, a true swap, and a row from before req
// was kept.
func TestUsageLineMapping(t *testing.T) {
	const at = `"t":"2026-10-12T00:59:00Z","agent":"alpha","provider":"p1",`
	tests := []struct {
		name          string
		line          string
		wantRequested string
		wantServed    string
		wantSwapped   bool
		wantTTFT      time.Duration
		wantRequestID string
	}{
		{
			name:          "provider prefix is not a swap",
			line:          `{` + at + `"req":"p1/m-a","model":"m-a","served":"m-a"}`,
			wantRequested: "p1/m-a", wantServed: "m-a",
		},
		{
			name:          "routing group answered by its provider model",
			line:          `{` + at + `"req":"group/auto","model":"glm-5.3","served":"glm-5.3","ttft_ms":420}`,
			wantRequested: "group/auto", wantServed: "glm-5.3", wantTTFT: 420 * time.Millisecond,
		},
		{
			name:          "answer differs from the provider model",
			line:          `{` + at + `"req":"x/x-latest","model":"x-latest","served":"x-1.13","rid":"abc"}`,
			wantRequested: "x/x-latest", wantServed: "x-1.13", wantSwapped: true, wantRequestID: "abc",
		},
		{
			name:          "record from before req was kept falls back to the provider model",
			line:          `{` + at + `"model":"m-c","status":200}`,
			wantRequested: "m-c",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeUsageLine([]byte(tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if got.RequestedModel != tc.wantRequested || got.ServedModel != tc.wantServed ||
				got.Swapped != tc.wantSwapped || got.TTFT != tc.wantTTFT || got.RequestID != tc.wantRequestID {
				t.Errorf("got requested %q served %q swapped %v ttft %v rid %q; want %q %q %v %v %q",
					got.RequestedModel, got.ServedModel, got.Swapped, got.TTFT, got.RequestID,
					tc.wantRequested, tc.wantServed, tc.wantSwapped, tc.wantTTFT, tc.wantRequestID)
			}
		})
	}
}
