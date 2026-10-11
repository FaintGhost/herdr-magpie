package panel

import (
	"math"
	"slices"
	"testing"
	"time"
)

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want int
	}{
		{name: "empty", s: "", want: 0},
		{name: "ASCII", s: "abc", want: 3},
		{name: "Chinese is two columns", s: "中文", want: 4},
		{name: "mixed", s: "x中y", want: 4},
		{name: "combining mark adds nothing", s: "é", want: 1},
		{name: "zero-width joiner adds nothing", s: "a‍b", want: 2},
		{name: "progress cells are one column", s: "▰▰▱", want: 3},
		{name: "ellipsis and dot are one column", s: "…·", want: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := displayWidth(tc.s); got != tc.want {
				t.Errorf("displayWidth(%q) = %d, want %d", tc.s, got, tc.want)
			}
		})
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		name string
		s    string
		w    int
		want string
	}{
		{name: "pads short text", s: "abc", w: 5, want: "abc  "},
		{name: "exact width", s: "abcde", w: 5, want: "abcde"},
		{name: "cuts with ellipsis", s: "abcdefgh", w: 5, want: "abcd…"},
		{name: "never splits a wide character", s: "中文字", w: 4, want: "中… "},
		{name: "zero width is empty", s: "abc", w: 0, want: ""},
		{name: "width one keeps only the ellipsis", s: "中文", w: 1, want: "…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fit(tc.s, tc.w)
			if got != tc.want {
				t.Errorf("fit(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
			}
			if dw := displayWidth(got); dw != max(tc.w, 0) {
				t.Errorf("fit(%q, %d) is %d columns, want %d", tc.s, tc.w, dw, max(tc.w, 0))
			}
		})
	}
}

func TestRjust(t *testing.T) {
	if got := rjust("42", 5); got != "   42" {
		t.Errorf("rjust(42, 5) = %q", got)
	}
	if got := rjust("123456", 3); displayWidth(got) != 3 {
		t.Errorf("rjust cut to %q, want 3 columns", got)
	}
}

func TestClean(t *testing.T) {
	tests := map[string]string{
		"plain":             "plain",
		"two\nlines":        "two lines",
		"tab\tand  spaces":  "tab and spaces",
		"escape\x1b[31mred": "escape [31mred",
		"bidi‮override":     "bidioverride",
		"  padded  ":        "padded",
		"para separator":    "para separator",
		"":                  "",
	}
	for in, want := range tests {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	tests := []struct {
		used float64
		want string
	}{
		{0, "▱▱▱▱▱▱▱▱▱▱"},
		{-5, "▱▱▱▱▱▱▱▱▱▱"},
		{math.NaN(), "▱▱▱▱▱▱▱▱▱▱"},
		{5, "▰▱▱▱▱▱▱▱▱▱"},
		{62, "▰▰▰▰▰▰▱▱▱▱"},
		{70, "▰▰▰▰▰▰▰▱▱▱"},
		{90, "▰▰▰▰▰▰▰▰▰▱"},
		{100, "▰▰▰▰▰▰▰▰▰▰"},
		{150, "▰▰▰▰▰▰▰▰▰▰"},
	}
	for _, tc := range tests {
		if got := progressBar(tc.used, 10); got != tc.want {
			t.Errorf("progressBar(%v) = %q, want %q", tc.used, got, tc.want)
		}
	}
}

func TestUsageLevel(t *testing.T) {
	tests := []struct {
		used float64
		want level
	}{
		{0, levelOK},
		{69.9, levelOK},
		{70, levelWarn},
		{90, levelWarn},
		{90.1, levelBad},
		{100, levelBad},
	}
	for _, tc := range tests {
		if got := usageLevel(tc.used); got != tc.want {
			t.Errorf("usageLevel(%v) = %v, want %v", tc.used, got, tc.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "<1m"},
		{59 * time.Second, "<1m"},
		{time.Minute, "1m"},
		{45 * time.Minute, "45m"},
		{2*time.Hour + 14*time.Minute, "2h14m"},
		{2*time.Hour + 5*time.Minute, "2h05m"},
		{23*time.Hour + 59*time.Minute, "23h59m"},
		{24 * time.Hour, "1d 0h"},
		{76 * time.Hour, "3d 4h"},
	}
	for _, tc := range tests {
		if got := formatDuration(tc.d); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestResetText(t *testing.T) {
	now := testNow
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "unknown", at: time.Time{}, want: ""},
		{name: "in two hours", at: now.Add(134 * time.Minute), want: "resets in 2h14m"},
		{name: "already passed", at: now.Add(-time.Minute), want: "reset passed"},
		{name: "now", at: now, want: "resets in <1m"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resetText(now, tc.at); got != tc.want {
				t.Errorf("resetText = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAgo(t *testing.T) {
	tests := map[time.Duration]string{
		0:                              "just now",
		42 * time.Second:               "42s ago",
		3*time.Minute + 10*time.Second: "3m ago",
		2 * time.Hour:                  "2h ago",
	}
	for d, want := range tests {
		if got := ago(d); got != want {
			t.Errorf("ago(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestFormatCost(t *testing.T) {
	tests := map[float64]string{
		0:     "$0.00",
		0.001: "<$0.01",
		3.1:   "$3.10",
	}
	for usd, want := range tests {
		if got := formatCost(usd); got != want {
			t.Errorf("formatCost(%v) = %q, want %q", usd, got, want)
		}
	}
}

func TestPercentile(t *testing.T) {
	if _, ok := percentile(nil, 50); ok {
		t.Error("percentile of nothing reported ok")
	}
	ten := []float64{10, 1, 9, 2, 8, 3, 7, 4, 6, 5}
	tests := []struct {
		name string
		vals []float64
		p    float64
		want float64
	}{
		{name: "single value", vals: []float64{5}, p: 50, want: 5},
		{name: "median of even count", vals: []float64{1, 2, 3, 4}, p: 50, want: 2.5},
		{name: "minimum", vals: ten, p: 0, want: 1},
		{name: "maximum", vals: ten, p: 100, want: 10},
		{name: "interpolated 90th", vals: ten, p: 90, want: 9.1},
		{name: "clamped above 100", vals: ten, p: 150, want: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := percentile(tc.vals, tc.p)
			if !ok || math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("percentile(p=%v) = %v, %v; want %v", tc.p, got, ok, tc.want)
			}
		})
	}
	if !slices.Equal(ten, []float64{10, 1, 9, 2, 8, 3, 7, 4, 6, 5}) {
		t.Error("percentile changed its input")
	}
}
