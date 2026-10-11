package panel

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// screenSizes are the terminals the layout must fit: the minimum, the target,
// and a few in between and below it.
var screenSizes = [][2]int{{80, 24}, {120, 32}, {137, 36}, {100, 30}, {60, 20}, {58, 20}, {40, 12}, {20, 6}, {10, 3}, {1, 1}}

// wideSource is sampleSource with a Chinese subscription name and account, and a
// storm table too long for a 24-line screen.
func wideSource() *fakeSource {
	src := sampleSource()
	src.quotas[0].Name = "中文订阅名称很长很长"
	src.quotas[0].User = "用户甲乙丙丁戊己庚辛壬癸@example.test"
	for i := range 9 {
		src.requests = append(src.requests, magpie.Request{
			Time: testNow, Provider: "provider-" + string(rune('a'+i)), Agent: "中文代理",
			Status: 402, Error: true, ErrorType: "balance",
		})
	}
	return src
}

func TestViewFitsEverySizeAndTab(t *testing.T) {
	for _, sz := range screenSizes {
		w, h := sz[0], sz[1]
		for ti := range numTabs {
			m := sized(t, Deps{Source: wideSource()}, w, h)
			m.tab = ti
			lines := strings.Split(m.View(), "\n")
			if len(lines) != h {
				t.Errorf("%dx%d %v: %d lines, want %d", w, h, ti, len(lines), h)
			}
			assertLines(t, lines, w)
		}
	}
}

func TestViewIsExactWidthAtTheTargetSize(t *testing.T) {
	m := sized(t, Deps{Source: wideSource()}, 120, 32)
	for ti := range numTabs {
		m.tab = ti
		for i, l := range strings.Split(m.View(), "\n") {
			if dw := displayWidth(l); dw != 120 {
				t.Errorf("tab %v line %d is %d columns, want 120: %q", ti, i, dw, l)
			}
		}
	}
}

func TestViewChrome(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource()}, 80, 24)
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[0], "[Agents] · Allowances · Usage · Routing · Sessions") {
		t.Errorf("tab bar = %q", lines[0])
	}
	if !strings.Contains(lines[len(lines)-1], "q quit") {
		t.Errorf("hint line = %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[1], "updated just now") {
		t.Errorf("age line = %q", lines[1])
	}
}

func TestPlainTabBarBracketsTheActiveTab(t *testing.T) {
	m := sized(t, Deps{Source: sampleSource()}, 80, 24)
	press(m, "2")
	if got := strings.Split(m.View(), "\n")[0]; !strings.Contains(got, "[Allowances]") {
		t.Errorf("plain tab bar = %q", got)
	}
}

func TestColouredViewUsesSGR(t *testing.T) {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI)
	m := newModel(t.Context(), Deps{Source: sampleSource(), Now: func() time.Time { return testNow }}, r)
	drain(m, m.Init())
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	press(m, "2")
	view := m.View()
	if !strings.Contains(view, "\x1b[") {
		t.Error("coloured view has no SGR sequences")
	}
	assertLines(t, strings.Split(view, "\n"), 80)
}

func TestNoColourViewHasNoEscapes(t *testing.T) {
	m := sized(t, Deps{Source: wideSource()}, 80, 24)
	for ti := range numTabs {
		m.tab = ti
		if view := m.View(); strings.Contains(view, "\x1b") {
			t.Errorf("tab %v: plain view contains an escape sequence", ti)
		}
	}
}

func TestUnsizedViewUsesTheDefaults(t *testing.T) {
	m := testModel(t, Deps{Source: sampleSource()})
	lines := strings.Split(m.View(), "\n")
	if len(lines) != defaultHeight {
		t.Errorf("unsized view has %d lines, want %d", len(lines), defaultHeight)
	}
	assertLines(t, lines, defaultWidth)
}
