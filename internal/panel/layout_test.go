package panel

import (
	"strings"
	"testing"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// bodyLines is the view's lines between the two chrome lines at the top and the two
// at the bottom: the body.
func bodyLines(view string) []string {
	lines := strings.Split(view, "\n")
	return lines[2 : len(lines)-2]
}

func TestSessionsDirStaysInItsColumnWithVS16Titles(t *testing.T) {
	src := samplePanelSource()
	src.sessions[0].Title = "🖥️ VPS 控制台的会话，名字很长很长很长很长很长很长很长很长很长"
	src.sessions[1].Title = "📝 EXAM: 使用当前项目解这套题"
	m := panelModel(t, src, &fakeHerdr{panes: samplePanes()}, nil, 136, 36)
	press(m, "5")
	lines := strings.Split(m.View(), "\n")
	header := lineWith(lines, "DIR")
	if header < 0 {
		t.Fatalf("no DIR title:\n%s", m.View())
	}
	dirAt := displayWidth(ansiFree(lines[header])[:strings.Index(ansiFree(lines[header]), "DIR")])
	rows := 0
	for _, l := range lines {
		plain := ansiFree(l)
		if displayWidth(plain) != 136 {
			t.Errorf("line is %d columns, want 136: %q", displayWidth(plain), plain)
		}
		if i := strings.Index(plain, "~/pp/"); i >= 0 {
			rows++
			if got := displayWidth(plain[:i]); got != dirAt {
				t.Errorf("directory starts at column %d, the DIR title at %d: %q", got, dirAt, plain)
			}
		}
	}
	if rows != len(src.sessions) {
		t.Errorf("%d rows show a directory, want %d:\n%s", rows, len(src.sessions), m.View())
	}
}

func TestContentIsMarginedAndCappedAt120(t *testing.T) {
	for ti := range numTabs {
		m := panelModel(t, samplePanelSource(), &fakeHerdr{panes: samplePanes()}, nil, 200, 40)
		m.tab = ti
		for i, l := range bodyLines(m.View()) {
			plain := ansiFree(l)
			if displayWidth(plain) != 200 {
				t.Errorf("tab %v line %d is %d columns, want 200", ti, i, displayWidth(plain))
			}
			if right := displayWidth(strings.TrimRight(plain, " ")); right > gutterW+maxContent {
				t.Errorf("tab %v line %d runs to column %d, past the %d-column content area: %q", ti, i, right, gutterW+maxContent, plain)
			}
		}
	}
	m := panelModel(t, samplePanelSource(), &fakeHerdr{panes: samplePanes()}, nil, 200, 40)
	press(m, "1")
	if header := lineWith(strings.Split(ansiFree(m.View()), "\n"), "AGENT"); header < 0 || !strings.HasPrefix(strings.Split(ansiFree(m.View()), "\n")[header], "  AGENT") {
		t.Errorf("the column titles should start two columns in, after the gutter")
	}
}

func TestContentWidthRule(t *testing.T) {
	for _, tc := range []struct{ w, content, body int }{
		{200, 120, 122}, {136, 120, 122}, {80, 76, 78}, {58, 54, 56}, {1, 0, 1},
	} {
		if got := contentWidth(tc.w); got != tc.content {
			t.Errorf("contentWidth(%d) = %d, want %d", tc.w, got, tc.content)
		}
		if got := bodyWidth(tc.w); got != tc.body {
			t.Errorf("bodyWidth(%d) = %d, want %d", tc.w, got, tc.body)
		}
	}
}

func TestSwappedIgnoresProviderPrefixAndEffort(t *testing.T) {
	tests := []struct {
		name    string
		r       magpie.Request
		swapped bool
		route   string
	}{
		{"provider prefix is not a swap", magpie.Request{RequestedModel: "codex/gpt-6-astra", ServedModel: "gpt-6-astra"}, false, "codex/gpt-6-astra"},
		{"a real model change is a swap", magpie.Request{RequestedModel: "deepseek-flash", ServedModel: "deepseek-v4-flash"}, true, "deepseek-flash → deepseek-v4-flash"},
		{"effort suffix is not a swap", magpie.Request{RequestedModel: "gpt-6-astra:high", ServedModel: "codex/gpt-6-astra"}, false, "gpt-6-astra:high"},
		{"agreeing names beat the usage flag", magpie.Request{RequestedModel: "x-1.13", ServedModel: "x-1.13", Swapped: true}, false, "x-1.13"},
		{"the flag decides when a name is missing", magpie.Request{RequestedModel: "x", Swapped: true}, true, "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := swapped(tc.r); got != tc.swapped {
				t.Errorf("swapped = %v, want %v", got, tc.swapped)
			}
			if got := routeText(tc.r); got != tc.route {
				t.Errorf("routeText = %q, want %q", got, tc.route)
			}
		})
	}
}

func TestSizedColOmitsAnEmptyColumnAndKeepsItsTitle(t *testing.T) {
	if c := sizedCol("CONFIG", []string{"", ""}, 10, 36, false); c != (column{}) {
		t.Errorf("an all-empty column = %+v, want it left out", c)
	}
	c := sizedCol("CONFIG", []string{"a"}, 10, 36, false)
	if c.want != len("CONFIG") || c.min != len("CONFIG") {
		t.Errorf("a short cell under a longer title = %+v, want the title's width", c)
	}
}
