package panel

import (
	"errors"
	"strings"
	"testing"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// profilesModel is the panel with the fixtures, a pane view and actions, on the Agents tab.
func profilesModel(t *testing.T, src *fakeSource, h *fakeHerdr, act *fakeActions) *Model {
	t.Helper()
	return panelModel(t, src, h, act, 120, 32)
}

func TestProfilesListShowsNamesAndSummaries(t *testing.T) {
	m := profilesModel(t, samplePanelSource(), nil, &fakeActions{})
	press(m, "p")
	if !m.prof.open {
		t.Fatal("p did not open the profile list")
	}
	view := m.View()
	for _, want := range []string{"Profiles", "work", "claude x · codex y", "cheap", "claude z"} {
		if !strings.Contains(view, want) {
			t.Errorf("profile list lacks %q:\n%s", want, view)
		}
	}
	if !strings.Contains(m.hintLine(120), "Enter apply") {
		t.Errorf("hint = %q", m.hintLine(120))
	}
}

func TestApplyAsksThenWrites(t *testing.T) {
	act := &fakeActions{}
	m := profilesModel(t, samplePanelSource(), nil, act)
	press(m, "p")
	press(m, "down")
	press(m, "enter")
	if m.ask == nil || m.ask.text != "Apply profile cheap? Enter/Esc" {
		t.Fatalf("confirm line = %+v", m.ask)
	}
	if len(act.used) != 0 {
		t.Fatal("applied before the confirm")
	}
	press(m, "enter")
	if len(act.used) != 1 || act.used[0] != "cheap" {
		t.Fatalf("used = %v, want [cheap]", act.used)
	}
	if m.prof.open {
		t.Error("the list should close when the apply starts")
	}
	if m.noticeKind != noticeOK || m.notice != "applied profile cheap" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}
	if m.writing {
		t.Error("still writing after the result landed")
	}
}

func TestApplyEscKeepsTheList(t *testing.T) {
	act := &fakeActions{}
	m := profilesModel(t, samplePanelSource(), nil, act)
	press(m, "p")
	press(m, "enter")
	press(m, "esc")
	if m.ask != nil || !m.prof.open || len(act.used) != 0 {
		t.Errorf("after esc: ask %+v, list open %v, used %v", m.ask, m.prof.open, act.used)
	}
	press(m, "esc")
	if m.prof.open {
		t.Error("esc on the list should close it")
	}
}

func TestNoProfilesSaysHowToSave(t *testing.T) {
	src := samplePanelSource()
	src.profiles = nil
	m := profilesModel(t, src, nil, &fakeActions{})
	press(m, "p")
	if view := m.View(); !strings.Contains(view, "no profiles yet — press S to save the current setup") {
		t.Errorf("empty list:\n%s", view)
	}
	press(m, "enter")
	if m.notice != noProfilesText || m.ask != nil {
		t.Errorf("enter on an empty list: notice %q, ask %+v", m.notice, m.ask)
	}
}

func TestSaveNamesThenWrites(t *testing.T) {
	act := &fakeActions{}
	m := profilesModel(t, samplePanelSource(), nil, act)
	press(m, "S")
	if !m.prof.naming {
		t.Fatal("S did not open the name prompt")
	}
	if got := ansiFree(m.statusLine(120)); !strings.Contains(got, "Save current setup as: ▏") {
		t.Errorf("status line = %q", got)
	}
	press(m, "q")
	press(m, "a")
	if m.tab != tabAgents {
		t.Errorf("typing a name switched tabs to %v", m.tab)
	}
	press(m, "enter")
	if m.ask == nil || m.ask.text != "Save current setup as qa? Enter/Esc" {
		t.Fatalf("confirm line = %+v", m.ask)
	}
	if len(act.saved) != 0 {
		t.Fatal("saved before the confirm")
	}
	press(m, "enter")
	if len(act.saved) != 1 || act.saved[0] != "qa" {
		t.Fatalf("saved = %v, want [qa]", act.saved)
	}
	if m.noticeKind != noticeOK || m.notice != "saved profile qa" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}
}

func TestNameThatCannotBeSavedIsRefused(t *testing.T) {
	act := &fakeActions{}
	m := profilesModel(t, samplePanelSource(), nil, act)
	press(m, "S")
	press(m, "-")
	press(m, "x")
	press(m, "enter")
	if m.ask != nil || m.notice != "a profile name cannot start with -" {
		t.Errorf("a name starting with - : ask %+v, notice %q", m.ask, m.notice)
	}
	press(m, "backspace")
	press(m, "backspace")
	press(m, "enter")
	if m.ask != nil || m.notice != "a profile needs a name" {
		t.Errorf("an empty name: ask %+v, notice %q", m.ask, m.notice)
	}
	press(m, "esc")
	if m.prof.naming || !m.prof.open {
		t.Errorf("esc in the prompt should return to the list: naming %v, open %v", m.prof.naming, m.prof.open)
	}
	if len(act.saved) != 0 {
		t.Errorf("saved %v", act.saved)
	}
}

func TestNamingTakesDigitsAndQAsText(t *testing.T) {
	m := profilesModel(t, samplePanelSource(), nil, &fakeActions{})
	press(m, "S")
	press(m, "2")
	press(m, "q")
	press(m, "1")
	if m.prof.name != "2q1" || m.tab != tabAgents || !m.prof.naming {
		t.Errorf("name = %q, tab %v", m.prof.name, m.tab)
	}
}

func TestValidProfileName(t *testing.T) {
	if got := validProfileName(""); got != "a profile needs a name" {
		t.Errorf("empty = %q", got)
	}
	if got := validProfileName("-rf"); got != "a profile name cannot start with -" {
		t.Errorf("flag-like = %q", got)
	}
	if got := validProfileName("tab\x07"); got != "a profile name cannot hold control characters" {
		t.Errorf("control = %q", got)
	}
	if got := validProfileName(strings.Repeat("n", maxProfileName+1)); got == "" {
		t.Error("an over-long name should be refused")
	}
	if got := validProfileName("工作 profile"); got != "" {
		t.Errorf("a plain name was refused: %q", got)
	}
}

func TestApplyWithNoModelChangeSaysSo(t *testing.T) {
	act := &fakeActions{}
	h := &fakeHerdr{panes: samplePanes()}
	m := profilesModel(t, samplePanelSource(), h, act)
	press(m, "p")
	press(m, "enter")
	press(m, "enter")
	if m.notice != "applied profile work; no agent's model changed" {
		t.Errorf("notice = %q", m.notice)
	}
	if m.walk.active || m.ask != nil {
		t.Errorf("nothing should restart: walk %+v, ask %+v", m.walk, m.ask)
	}
}

func TestApplyWithoutHerdrKeepsItsNotice(t *testing.T) {
	act := &fakeActions{}
	m := profilesModel(t, samplePanelSource(), nil, act)
	press(m, "p")
	press(m, "enter")
	press(m, "enter")
	if m.notice != "applied profile work" || m.walk.active {
		t.Errorf("without herdr: notice %q, walk %+v", m.notice, m.walk)
	}
}

func TestApplyFailureIsReported(t *testing.T) {
	act := &fakeActions{err: errors.New("profile locked")}
	m := profilesModel(t, samplePanelSource(), nil, act)
	press(m, "p")
	press(m, "enter")
	press(m, "enter")
	if m.noticeKind != noticeBad || m.notice != "apply failed: profile locked" {
		t.Errorf("notice = %q (%v)", m.notice, m.noticeKind)
	}
}

func TestProfileWritesWithoutActionsSayTheyAreNotConfigured(t *testing.T) {
	m := profilesModel(t, samplePanelSource(), nil, nil)
	press(m, "p")
	press(m, "enter")
	press(m, "enter")
	if m.noticeKind != noticeBad || m.notice != "apply failed: profiles is not configured" {
		t.Errorf("notice = %q", m.notice)
	}
}

func TestModelChangesListOnlyChangedAgents(t *testing.T) {
	before := []magpie.AgentInfo{
		{ID: "claude", Fields: []magpie.AgentField{{Name: "model", Value: "a"}}},
		{ID: "codex", Fields: []magpie.AgentField{{Name: "model", Value: "b"}}},
		{ID: "", Fields: []magpie.AgentField{{Name: "model", Value: "c"}}},
	}
	after := []magpie.AgentInfo{
		{ID: "claude", Fields: []magpie.AgentField{{Name: "model", Value: "z"}}},
		{ID: "codex", Fields: []magpie.AgentField{{Name: "model", Value: "b"}}},
		{ID: "omp", Fields: []magpie.AgentField{{Name: "model", Value: "new"}}},
		{ID: "", Fields: []magpie.AgentField{{Name: "model", Value: "d"}}},
	}
	if got := modelChanges(before, after); len(got) != 1 || got[0] != "claude" {
		t.Errorf("modelChanges = %v, want [claude]", got)
	}
	if got := modelChanges(nil, after); got != nil {
		t.Errorf("with nothing before, no agent changed: %v", got)
	}
}
