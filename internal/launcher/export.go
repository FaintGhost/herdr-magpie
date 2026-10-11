package launcher

import (
	"context"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// The names below let other packages show the launcher's session list and open a
// session the launcher's way. They wrap the unexported logic, which the launcher's
// own tests still exercise directly. The Herdr panel's Sessions tab is built on them.

// Item is one row of the sessions list: a magpie session and the herdr pane that
// holds it, if any. It is the same type the launcher lists.
type Item = item

// OpenPane is the herdr pane that holds a session, and that pane's workspace.
type OpenPane = openPane

// Layout sizes the rows of one list. Build it with NewLayout.
type Layout = layout

// Session is the magpie session of the row.
func (it Item) Session() magpie.Session { return it.session }

// Open is the pane that holds the row's session, or nil when no pane has it.
func (it Item) Open() *OpenPane { return it.open }

// PaneID is the id of the pane.
func (p OpenPane) PaneID() string { return p.paneID }

// WorkspaceID is the id of the pane's workspace.
func (p OpenPane) WorkspaceID() string { return p.workspaceID }

// BuildItems pairs sessions with the agent panes that hold them. The result is
// newest first, as the launcher lists it (see buildItems).
func BuildItems(sessions []magpie.Session, agents []herdr.Agent) []Item {
	return buildItems(sessions, agents)
}

// FilterItems keeps the items whose agent, title or directory contains every
// word of query, ignoring case. An empty query keeps all (see filterItems).
func FilterItems(items []Item, query, home string) []Item {
	return filterItems(items, query, home)
}

// NewLayout sizes the columns of rows drawn width columns wide, for items. The
// directory column is dropped on narrow screens (see newLayout).
func NewLayout(width int, items []Item, home string) Layout {
	return newLayout(width, items, home)
}

// Row is one list row for it, exactly the layout's width (see row).
func (l Layout) Row(it Item, now time.Time) string {
	return l.row(it, now)
}

// Open opens session s as the launcher's Enter does. A session that a pane already
// holds gets that pane focused. Any other is resumed in a new pane: in a tab of the
// workspace that holds its directory, or in a new workspace for that directory.
// current is the workspace the caller runs in, and home the user's home directory.
// It returns the notice of a fallback launch (see resume), or "" when the session
// opened as usual.
//
// agents must be a successful read of the agent panes. With a failed read a session
// that a pane already holds would be resumed a second time, so the caller should
// refuse to open a session it cannot match.
func Open(ctx context.Context, h *herdr.Client, s magpie.Session, agents []herdr.Agent, current, home string) (notice string, err error) {
	it := buildItems([]magpie.Session{s}, agents)[0]
	p, err := resume(ctx, h, it, agents, current, home, isDir)
	if err != nil {
		return "", err
	}
	return p.notice, nil
}

// DisplayWidth is the number of terminal columns s takes (see displayWidth).
func DisplayWidth(s string) int { return displayWidth(s) }

// Fit returns s cut or padded to exactly w columns, cut with "…" (see fit).
func Fit(s string, w int) string { return fit(s, w) }

// TailFit is Fit for paths: when it cuts, it keeps the end (see tailFit).
func TailFit(s string, w int) string { return tailFit(s, w) }

// RelTime says how long ago t was, as "now", "5m", "3h" or "2d", or "-" for a zero
// time (see relTime).
func RelTime(now, t time.Time) string { return relTime(now, t) }

// ShortPath writes a directory with the home directory shown as ~ (see shortPath).
func ShortPath(p, home string) string { return shortPath(p, home) }

// DisplayTitle is the session's title, or its directory's last part when it has none.
func DisplayTitle(s magpie.Session, home string) string { return displayTitle(s, home) }

// AgentName is the session's agent, or "-" when magpie named none.
func AgentName(s magpie.Session) string { return agentName(s) }

// TabLabel is the label for a new tab that resumes s: "<agent>: <title>", with the
// title cut to 30 columns (see tabLabel).
func TabLabel(s magpie.Session, home string) string { return tabLabel(s, home) }
