// Package panel is the magpie panel: a full-screen Herdr popup with tabs for
// agents, allowances (subscription windows and accounts), usage, routing and
// sessions. It draws from the cached snapshot at once and refreshes every feed in
// the background. Its data and its actions come in through the Source and Actions
// interfaces, so the package never runs magpie itself.
package panel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// Source reads what the panel shows. Each method is called on its own, with its
// own timeout. An error only blanks the part of the panel that method feeds.
type Source interface {
	Agents(ctx context.Context) ([]magpie.AgentInfo, error)
	Quotas(ctx context.Context) ([]magpie.Quota, error)
	Accounts(ctx context.Context) ([]magpie.Account, error)
	Profiles(ctx context.Context) ([]magpie.Profile, error)
	RecentRequests(ctx context.Context, since time.Time) ([]magpie.Request, error) // usage.jsonl tail
	APIUsage(ctx context.Context) (*magpie.UsageSummary, error)                    // nil, nil without key
	Sessions(ctx context.Context) ([]magpie.Session, error)                        // magpie sessions --json, for the Sessions tab and resume commands
}

// Actions change magpie's state. The panel asks for confirmation before it calls one.
type Actions interface {
	UseProfile(ctx context.Context, name string) error
	SaveProfile(ctx context.Context, name string) error
	SwitchAccount(ctx context.Context, agent, user string) error
	// OpenSession focuses s when a pane already runs it, otherwise resumes it as the
	// launcher does (matching workspace, else a new one for its directory). notice is
	// shown in the status line when non-empty.
	OpenSession(ctx context.Context, s magpie.Session) (notice string, err error)
	// RestartPane closes an idle agent pane and resumes its session in a new tab of
	// the same workspace and directory. It refuses a pane that is not idle.
	RestartPane(ctx context.Context, t RestartTarget) error
}

// RestartTarget is one Herdr pane to restart after a profile apply or an account
// switch, with the command that resumes its session.
type RestartTarget struct {
	PaneID      string
	WorkspaceID string
	CWD         string
	Agent       string // herdr agent label, such as "claude"
	Resume      string // the session's resume command from magpie sessions
	Label       string // tab label for the new tab
}

// HerdrView is the part of internal/herdr the panel reads: the agent panes, which
// the Agents and Sessions tabs and the restart plan use. It may be nil, and then the
// panel shows no panes and plans no restarts.
type HerdrView interface {
	Agents(ctx context.Context) ([]herdr.Agent, error)
}

// CacheStore keeps the panel's last snapshot between runs. *state.Store satisfies it.
type CacheStore interface {
	Load(name string, v any) error
	Save(name string, v any) error
}

// Deps are the panel's collaborators. Source, In and Out are required. The rest
// may be nil: without Actions a write reports that it is not configured, without
// Herdr the panes are not shown, and without a Cache the panel starts empty every time.
type Deps struct {
	Source   Source
	Actions  Actions
	Herdr    HerdrView
	Cache    CacheStore
	In       *os.File
	Out      io.Writer
	Now      func() time.Time // nil means time.Now
	StartTab string           // a tab name or a digit 1 to 5; anything else means Agents
	// FocusAgent is the herdr agent label of the pane the panel was opened from, such
	// as "claude". The Agents tab selects that agent. "" when there is none.
	FocusAgent string
}

// Run shows the panel full screen until the user quits, and returns nil then. The
// terminal is restored on every return path. Cancelling ctx stops the panel and
// returns ctx's error.
func Run(ctx context.Context, d Deps) error {
	if d.Source == nil || d.In == nil || d.Out == nil {
		return errors.New("panel: Source, In and Out are required")
	}
	m := NewModel(ctx, d)
	m.live = true
	p := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(d.In),
		tea.WithOutput(d.Out),
		tea.WithAltScreen(),
	)
	if _, err := p.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("panel: %w", err)
	}
	return nil
}
