package panel

import "testing"

func TestTabCycleWraps(t *testing.T) {
	if got := nextTab(tabSessions); got != tabAgents {
		t.Errorf("nextTab(Sessions) = %v, want Agents", got)
	}
	if got := prevTab(tabAgents); got != tabSessions {
		t.Errorf("prevTab(Agents) = %v, want Sessions", got)
	}
	for ti := range numTabs {
		if got := prevTab(nextTab(ti)); got != ti {
			t.Errorf("prevTab(nextTab(%v)) = %v", ti, got)
		}
	}
}

func TestTabForKey(t *testing.T) {
	for i, k := range []string{"1", "2", "3", "4", "5"} {
		if got, ok := tabForKey(k); !ok || got != tab(i) {
			t.Errorf("tabForKey(%q) = %v, %v", k, got, ok)
		}
	}
	for _, k := range []string{"0", "6", "", "12", "r", "tab"} {
		if _, ok := tabForKey(k); ok {
			t.Errorf("tabForKey(%q) matched", k)
		}
	}
}

func TestParseTab(t *testing.T) {
	tests := []struct {
		in   string
		want tab
		ok   bool
	}{
		{"Agents", tabAgents, true},
		{"routing", tabRouting, true},
		{" Sessions ", tabSessions, true},
		{"3", tabUsage, true},
		{"", tabAgents, false},
		{"nope", tabAgents, false},
	}
	for _, tc := range tests {
		got, ok := parseTab(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("parseTab(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestTabNames(t *testing.T) {
	want := []string{"Agents", "Allowances", "Usage", "Routing", "Sessions"}
	for i, name := range want {
		if got := tab(i).String(); got != name {
			t.Errorf("tab %d = %q, want %q", i, got, name)
		}
	}
	if got := tab(9).String(); got != "?" {
		t.Errorf("out-of-range tab = %q", got)
	}
}
