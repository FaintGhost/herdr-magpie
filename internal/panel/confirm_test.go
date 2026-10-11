package panel

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirmLineAnswers(t *testing.T) {
	var yes, no, skip int
	newAsk := func(withSkip bool) *confirmLine {
		a := &confirmLine{
			text: "Q? Enter/Esc",
			yes:  func() tea.Cmd { yes++; return nil },
			no:   func() { no++ },
		}
		if withSkip {
			a.skip = func() { skip++ }
		}
		return a
	}

	m := testModel(t, Deps{Source: sampleSource()})
	m.ask = newAsk(false)
	if cmd := m.keyAsk("x"); cmd != nil || m.ask == nil {
		t.Fatalf("an unused key should be ignored: ask %v", m.ask)
	}
	if cmd := m.keyAsk("s"); cmd != nil || m.ask == nil || skip != 0 {
		t.Fatal("s without a skip should be ignored")
	}
	m.keyAsk("esc")
	if no != 1 || m.ask != nil {
		t.Errorf("esc: no = %d, ask %v", no, m.ask)
	}

	m.ask = newAsk(true)
	m.keyAsk("s")
	if skip != 1 || m.ask != nil {
		t.Errorf("s: skip = %d, ask %v", skip, m.ask)
	}

	m.ask = newAsk(false)
	m.keyAsk("enter")
	if yes != 1 || m.ask != nil {
		t.Errorf("enter: yes = %d, ask %v", yes, m.ask)
	}

	m.ask = newAsk(false)
	if cmd := m.keyAsk("q"); cmd == nil {
		t.Error("q should quit from a question")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q returned %T", cmd())
	}
}

func TestQuestionThatAnswersAOpensTheNext(t *testing.T) {
	m := testModel(t, Deps{Source: sampleSource()})
	m.ask = &confirmLine{text: "first", yes: func() tea.Cmd {
		m.ask = &confirmLine{text: "second"}
		return nil
	}}
	m.keyAsk("enter")
	if m.ask == nil || m.ask.text != "second" {
		t.Errorf("the action should be able to open the next question: %+v", m.ask)
	}
}

func TestTypedRune(t *testing.T) {
	tests := []struct {
		key  string
		want rune
		ok   bool
	}{
		{"a", 'a', true},
		{" ", ' ', true},
		{"中", '中', true},
		{"é", 'é', true},
		{"enter", 0, false},
		{"ctrl+u", 0, false},
		{"tab", 0, false},
		{"\x07", 0, false},
		{"", 0, false},
		{"ab", 0, false},
	}
	for _, tc := range tests {
		got, ok := typedRune(tc.key)
		if got != tc.want || ok != tc.ok {
			t.Errorf("typedRune(%q) = %q, %v; want %q, %v", tc.key, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDropLastRune(t *testing.T) {
	for in, want := range map[string]string{"中文": "中", "ab": "a", "": "", "x": ""} {
		if got := dropLastRune(in); got != want {
			t.Errorf("dropLastRune(%q) = %q, want %q", in, got, want)
		}
	}
}
