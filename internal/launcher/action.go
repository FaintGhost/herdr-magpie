package launcher

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// errNoResume is the refusal for a session that magpie gave no resume command.
var errNoResume = errors.New("this session has no resume command")

// noticeMissingDir is the bottom-line notice for a session whose directory is
// empty or gone. Its pane opens in the current workspace instead.
const noticeMissingDir = "project dir missing, opened in current workspace"

// placement is where resume put a session: the pane that now runs it, the
// workspace holding that pane, and the notice for a fallback. An open session,
// which is only focused, has the zero placement.
type placement struct {
	open   openPane
	notice string
}

// target is where the new pane for a session goes.
type target struct {
	workspace    string // workspace that gets the new tab; empty when none is known
	newWorkspace bool   // create a workspace for dir instead of a tab
	dir          string // directory the tab or workspace starts in; empty when none is passed
	notice       string // bottom-line notice for a fallback; empty otherwise
}

// resume opens it. An open session has its workspace and pane focused. Any other
// session gets a new pane, and its resume command runs there: a tab or a new
// workspace, as chooseTarget decides. current is the workspace the launcher runs
// in, and dirExists reports whether a directory exists.
func resume(ctx context.Context, h *herdr.Client, it item, agents []herdr.Agent, current, home string, dirExists func(string) bool) (placement, error) {
	if it.open != nil {
		if it.open.workspaceID != "" {
			if err := h.FocusWorkspace(ctx, it.open.workspaceID); err != nil {
				return placement{}, err
			}
		}
		return placement{}, h.FocusPane(ctx, it.open.paneID)
	}
	s := it.session
	if strings.TrimSpace(s.Resume) == "" {
		return placement{}, errNoResume
	}
	t := chooseTarget(agents, s.CWD, current, home, dirExists)
	var ws, pane string
	var err error
	if t.newWorkspace {
		ws, pane, err = h.CreateWorkspace(ctx, t.dir, true)
	} else {
		ws = t.workspace
		pane, err = h.CreateTab(ctx, t.workspace, t.dir, tabLabel(s, home), true)
	}
	if err != nil {
		return placement{}, err
	}
	if err := h.RunInPane(ctx, pane, s.Resume); err != nil {
		return placement{}, fmt.Errorf("resume in new pane %s: %w", pane, err)
	}
	return placement{open: openPane{paneID: pane, workspaceID: ws}, notice: t.notice}, nil
}

// chooseTarget decides where a session in cwd opens. An empty directory, or one
// that dirExists rejects, gives a tab in current with no directory and a notice.
// Otherwise the workspace of an agent pane in the same directory gets the tab,
// and with several such panes current wins if it is one of them. With no such
// pane a new workspace is made for the directory, whatever current is.
func chooseTarget(agents []herdr.Agent, cwd, current, home string, dirExists func(string) bool) target {
	dir := normalizeDir(cwd, home)
	if dir == "" || !dirExists(dir) {
		return target{workspace: current, notice: noticeMissingDir}
	}
	if ws, ok := matchWorkspace(agents, dir, current, home); ok {
		return target{workspace: ws, dir: dir}
	}
	return target{newWorkspace: true, dir: dir}
}

// matchWorkspace returns the workspace of an agent pane whose directory is dir.
// When several panes match, current wins if it is one of them. It reports false
// when no pane matches.
func matchWorkspace(agents []herdr.Agent, dir, current, home string) (string, bool) {
	found := ""
	for _, a := range agents {
		if a.WorkspaceID == "" || normalizeDir(a.CWD, home) != dir {
			continue
		}
		if a.WorkspaceID == current {
			return current, true
		}
		if found == "" {
			found = a.WorkspaceID
		}
	}
	return found, found != ""
}

// tabLabel is "<agent>: <title>", with the title cut to 30 columns.
func tabLabel(s magpie.Session, home string) string {
	agent := cmp.Or(strings.TrimSpace(sanitize(s.Agent)), "session")
	return agent + ": " + strings.TrimRight(prefix(displayTitle(s, home), 30), " ")
}
