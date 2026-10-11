package panel

import (
	"slices"
	"strings"
)

// tab is one page of the panel. The order is the order of the tab bar and of
// the Tab key.
type tab int

const (
	tabAgents tab = iota
	tabAllowances
	tabUsage
	tabRouting
	tabSessions
	numTabs
)

var tabNames = [numTabs]string{"Agents", "Allowances", "Usage", "Routing", "Sessions"}

// String is the tab's name as the tab bar shows it.
func (t tab) String() string {
	if t < 0 || t >= numTabs {
		return "?"
	}
	return tabNames[t]
}

// nextTab is the tab after t, wrapping after the last.
func nextTab(t tab) tab {
	return (t + 1) % numTabs
}

// prevTab is the tab before t, wrapping before the first.
func prevTab(t tab) tab {
	return (t + numTabs - 1) % numTabs
}

// tabForKey maps the digit keys 1 to 5 to their tabs.
func tabForKey(key string) (tab, bool) {
	if len(key) == 1 && '1' <= key[0] && key[0] < '1'+byte(numTabs) {
		return tab(key[0] - '1'), true
	}
	return tabAgents, false
}

// parseTab reads a tab from its name, in any case, or from a digit 1 to 5. It
// reads Deps.StartTab. When s is neither, it returns tabAgents and false.
func parseTab(s string) (tab, bool) {
	s = strings.TrimSpace(s)
	if t, ok := tabForKey(s); ok {
		return t, true
	}
	if i := slices.IndexFunc(tabNames[:], func(name string) bool {
		return strings.EqualFold(name, s)
	}); i >= 0 {
		return tab(i), true
	}
	return tabAgents, false
}
