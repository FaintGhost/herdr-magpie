package launcher

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
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
		{name: "emoji is two columns", s: "🚀", want: 2},
		{name: "combining mark adds nothing", s: "é", want: 1},
		{name: "zero-width joiner adds nothing", s: "a‍b", want: 2},
		{name: "ambiguous width counts as one", s: "●…", want: 2},
		{name: "VS16 emoji is two columns", s: "🖥️ VPS", want: 6},
		{name: "emoji and space", s: "📝 a", want: 4},
		{name: "Chinese pair", s: "中文", want: 4},
		{name: "progress, dot, ellipsis and arrow are one each", s: "▰·…→", want: 4},
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
		s    string
		w    int
		want string
	}{
		{s: "abc", w: 5, want: "abc  "},
		{s: "abc", w: 3, want: "abc"},
		{s: "abcdef", w: 4, want: "abc…"},
		{s: "中文测试", w: 5, want: "中文…"},
		{s: "中文测试", w: 4, want: "中… "}, // a wide character that would straddle the cut is dropped
		{s: "🚀🚀🚀", w: 4, want: "🚀… "},
		{s: "abc", w: 1, want: "…"},
		{s: "abc", w: 0, want: ""},
		{s: "", w: 3, want: "   "},
	}
	for _, tc := range tests {
		if got := fit(tc.s, tc.w); got != tc.want {
			t.Errorf("fit(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
		}
	}
}

func TestTailFitKeepsTheEnd(t *testing.T) {
	tests := []struct {
		s    string
		w    int
		want string
	}{
		{s: "/root/pp/demo-app", w: 8, want: "…emo-app"},
		{s: "abc", w: 5, want: "abc  "},
		{s: "中文测试", w: 5, want: "…测试"},
		{s: "中文测试", w: 4, want: "…试 "}, // the wide character at the cut is dropped, not split
		{s: "🖥️ab", w: 3, want: "…ab"},
		{s: "abcdef", w: 1, want: "…"},
		{s: "abc", w: 0, want: ""},
	}
	for _, tc := range tests {
		if got := tailFit(tc.s, tc.w); got != tc.want {
			t.Errorf("tailFit(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
		}
	}
}

func TestPrefixAndRjust(t *testing.T) {
	if got := prefix("标题标题标题", 4); got != "标题" {
		t.Errorf("prefix(4 columns) = %q, want 标题", got)
	}
	if got := prefix("a中", 2); got != "a" {
		t.Errorf("prefix(a中, 2) = %q, want a", got)
	}
	if got := prefix("abc", 10); got != "abc" {
		t.Errorf("prefix(abc, 10) = %q, want abc", got)
	}
	if got := rjust("3h", 3); got != " 3h" {
		t.Errorf("rjust(3h, 3) = %q, want \" 3h\"", got)
	}
	if got := rjust("123456", 3); got != "12…" {
		t.Errorf("rjust(123456, 3) = %q, want 12…", got)
	}
}

func TestSanitizeRemovesControlsAndFormatting(t *testing.T) {
	got := sanitize("a\x1b[31mb\nc\td‮e")
	if want := "a [31mb c de"; got != want {
		t.Errorf("sanitize = %q, want %q", got, want)
	}
	for _, r := range got {
		if unicode.IsControl(r) {
			t.Errorf("sanitize left control character %U", r)
		}
	}
}

func TestRelTime(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{name: "now", t: testNow, want: "now"},
		{name: "just under a minute", t: testNow.Add(-59 * time.Second), want: "now"},
		{name: "one minute", t: testNow.Add(-time.Minute), want: "1m"},
		{name: "just under an hour", t: testNow.Add(-59*time.Minute - 59*time.Second), want: "59m"},
		{name: "one hour", t: testNow.Add(-time.Hour), want: "1h"},
		{name: "just under a day", t: testNow.Add(-23*time.Hour - 59*time.Minute), want: "23h"},
		{name: "one day", t: testNow.Add(-24 * time.Hour), want: "1d"},
		{name: "three days", t: testNow.Add(-72 * time.Hour), want: "3d"},
		{name: "future is now", t: testNow.Add(5 * time.Minute), want: "now"},
		{name: "zero time", t: time.Time{}, want: "-"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := relTime(testNow, tc.t); got != tc.want {
				t.Errorf("relTime = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := map[float64]string{
		0:      "$0.00",
		-1:     "$0.00",
		0.0049: "<$0.01",
		0.006:  "$0.01",
		1.234:  "$1.23",
		1234.5: "$1234.50",
	}
	for c, want := range tests {
		if got := formatCost(c); got != want {
			t.Errorf("formatCost(%v) = %q, want %q", c, got, want)
		}
	}
}

// layoutItems is a synthetic list with the awkward cases: wide and emoji titles,
// a long directory, no title, no time, control characters, and a large cost.
func layoutItems() []item {
	return []item{
		{session: magpie.Session{Agent: "claude", Title: "修复登录页面的 🚀 回归问题, a rather long title", CWD: "/home/u/pp/demo-app", Last: testNow.Add(-3 * time.Hour), Cost: 1.234}, open: new(openPane{paneID: "p1"})},
		{session: magpie.Session{Agent: "omp", CWD: "/home/u/Work/Notes/deeply/nested/directory/name", Last: testNow.Add(-40 * time.Minute)}},
		{session: magpie.Session{Agent: "hermes-with-a-very-long-name", Cost: 12345.678}},
		{session: magpie.Session{Agent: "codex", Title: "tab\tand\x1b[31mesc\nnewline", CWD: "~/pp/x/", Last: testNow.Add(-50 * time.Hour), Cost: 0.004}},
		{session: magpie.Session{Title: "中文中文中文中文中文中文中文中文中文中文中文中文", CWD: "/srv/中文目录/子目录", Cost: 3}},
	}
}

func TestRowsAreExactlyTheScreenWidth(t *testing.T) {
	items := layoutItems()
	for _, width := range []int{1, 2, 3, 5, 10, 20, 26, 27, 28, 30, 40, 45, 60, 80, 100, 120, 200} {
		lay := newLayout(width, items, "/home/u")
		for _, it := range items {
			row := lay.row(it, testNow)
			if got := displayWidth(row); got != width {
				t.Errorf("width %d: row width = %d, want %d: %q", width, got, width, row)
			}
			if strings.ContainsFunc(row, unicode.IsControl) {
				t.Errorf("width %d: row has a control character: %q", width, row)
			}
			if width < 3 {
				continue
			}
			wantMark := "  "
			if it.open != nil {
				wantMark = "● "
			}
			if !strings.HasPrefix(row, wantMark) {
				t.Errorf("width %d: row %q does not start with mark %q", width, row, wantMark)
			}
		}
	}
}

func TestRowShowsColumns(t *testing.T) {
	it := item{
		session: magpie.Session{Agent: "claude", Title: "修复登录", CWD: "/home/u/pp/app", Last: testNow.Add(-3 * time.Hour), Cost: 1.234},
		open:    new(openPane{paneID: "p1"}),
	}
	row := newLayout(80, []item{it}, "/home/u").row(it, testNow)
	for _, want := range []string{"● claude", "3h", "$1.23", "修复登录"} {
		if !strings.Contains(row, want) {
			t.Errorf("row %q does not contain %q", row, want)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "~/pp/app") {
		t.Errorf("row %q does not end with the directory in ~ form", row)
	}
}

func TestRowTitleFallsBackToDirectory(t *testing.T) {
	it := item{session: magpie.Session{Agent: "omp", CWD: "/home/u/pp/demo-app/", Last: testNow}}
	row := newLayout(80, []item{it}, "/home/u").row(it, testNow)
	if !strings.Contains(row, "demo-app") {
		t.Errorf("untitled row %q does not show the directory name", row)
	}
}

// checkLines fails unless lines has h rows and each is exactly w columns wide.
func checkLines(t *testing.T, lines []line, w, h int) {
	t.Helper()
	if len(lines) != h {
		t.Fatalf("lines = %d, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := displayWidth(l.text); got != w {
			t.Errorf("line %d width = %d, want %d: %q", i, got, w, l.text)
		}
	}
}

func TestFrameLoading(t *testing.T) {
	m := newModel("/home/u", 14)
	m.resize(40, 6)
	lines := m.frame(testNow)
	checkLines(t, lines, 40, 6)
	if lines[0].kind != kindHeader || !strings.Contains(lines[0].text, "loading") {
		t.Errorf("header = %+v, want the loading header", lines[0])
	}
	if !strings.HasPrefix(lines[1].text, "loading…") {
		t.Errorf("body = %q, want the loading placeholder", lines[1].text)
	}
	if lines[5].kind != kindHint {
		t.Errorf("footer kind = %v, want hints", lines[5].kind)
	}
}

func TestFrameRowsAndSelection(t *testing.T) {
	var sessions []magpie.Session
	for i := range 6 {
		sessions = append(sessions, magpie.Session{Agent: "a", ID: string(rune('a' + i)), Last: testNow.Add(-time.Duration(i) * time.Hour)})
	}
	m := newModel("/home/u", 14)
	m.resize(60, 6) // four list rows
	m.setLoaded(sessions, nil, true)
	for range 5 {
		m.press(key{kind: keyDown})
	}
	lines := m.frame(testNow)
	checkLines(t, lines, 60, 6)

	var selected []int
	for i, l := range lines {
		if l.kind == kindSelected {
			selected = append(selected, i)
		}
	}
	// Cursor 5 with four rows on screen puts the window at rows 2..5, so the
	// selected line is the last list row, line 4.
	if !slices.Equal(selected, []int{4}) {
		t.Errorf("selected lines = %v, want [4]", selected)
	}
	if !strings.Contains(lines[0].text, "6 of 6") {
		t.Errorf("header %q does not show the counts", lines[0].text)
	}
}

func TestFrameNoticeReplacesHints(t *testing.T) {
	m := newModel("/home/u", 14)
	m.resize(60, 4)
	m.setLoaded(nil, nil, true)
	m.message = "herdr: tab create failed"
	lines := m.frame(testNow)
	checkLines(t, lines, 60, 4)
	footer := lines[3]
	if footer.kind != kindNotice || !strings.HasPrefix(footer.text, "herdr: tab create failed") {
		t.Errorf("footer = %+v, want the notice", footer)
	}
	if !strings.Contains(lines[1].text, "no sessions in the last 14 days") {
		t.Errorf("empty list text = %q", lines[1].text)
	}
}

func TestFrameNoMatches(t *testing.T) {
	m := newModel("/home/u", 14)
	m.resize(60, 4)
	m.setLoaded([]magpie.Session{{Agent: "claude"}}, nil, true)
	m.query = "zzz"
	m.refilter()
	lines := m.frame(testNow)
	if !strings.Contains(lines[1].text, `no session matches "zzz"`) {
		t.Errorf("list text = %q, want the no-match placeholder", lines[1].text)
	}
}

func TestFrameTinyHeights(t *testing.T) {
	m := newModel("/home/u", 14)
	m.resize(40, 1)
	m.setLoaded([]magpie.Session{{Agent: "claude"}}, nil, true)
	checkLines(t, m.frame(testNow), 40, 1)

	m.resize(40, 2)
	lines := m.frame(testNow)
	checkLines(t, lines, 40, 2)
	if lines[1].kind != kindHint {
		t.Errorf("height 2: footer kind = %v, want hints; body must not overwrite it", lines[1].kind)
	}

	m.resize(40, 0)
	if lines := m.frame(testNow); lines != nil {
		t.Errorf("height 0: frame = %v, want nil", lines)
	}
}

func TestHeaderKeepsTheTypedEnd(t *testing.T) {
	m := newModel("/home/u", 14)
	m.resize(60, 4)
	m.setLoaded([]magpie.Session{{Agent: "claude"}}, nil, true)
	m.query = strings.Repeat("a", 40) + "ZZZ"
	m.refilter()
	header := m.frame(testNow)[0].text
	if !strings.Contains(header, "ZZZ") || !strings.Contains(header, "0 of 1") {
		t.Errorf("header %q should keep the end of the filter and the counts", header)
	}
	checkLines(t, m.frame(testNow), 60, 4)
}

func TestVS16RowIsExactWidthAndKeepsTheDirectory(t *testing.T) {
	it := item{session: magpie.Session{Agent: "claude", Title: "🖥️ VPS 修复登录", CWD: "/home/u/pp/app", Last: testNow.Add(-time.Hour)}}
	row := newLayout(136, []item{it}, "/home/u").row(it, testNow)
	if got := displayWidth(row); got != 136 {
		t.Errorf("row is %d columns, want 136: %q", got, row)
	}
	if !strings.HasSuffix(strings.TrimRight(row, " "), "~/pp/app") {
		t.Errorf("row %q does not end with the directory", row)
	}
}
