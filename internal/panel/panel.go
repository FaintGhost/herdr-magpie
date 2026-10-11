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
}

// Actions change magpie's state. The panel asks for confirmation before it calls one.
type Actions interface {
	UseProfile(ctx context.Context, name string) error
	SaveProfile(ctx context.Context, name string) error
	SwitchAccount(ctx context.Context, agent, user string) error
}

// HerdrView is the part of internal/herdr the panel reads. Batch 2 wires it up;
// batch 1 only carries it.
type HerdrView interface {
	Agents(ctx context.Context) ([]herdr.Agent, error)
}

// CacheStore keeps the panel's last snapshot between runs. *state.Store satisfies it.
type CacheStore interface {
	Load(name string, v any) error
	Save(name string, v any) error
}

// Deps are the panel's collaborators. Source, In and Out are required. The rest
// may be nil: without Actions a switch reports that it is not configured, and
// without a Cache the panel starts empty every time.
type Deps struct {
	Source   Source
	Actions  Actions
	Herdr    HerdrView
	Cache    CacheStore
	In       *os.File
	Out      io.Writer
	Now      func() time.Time // nil means time.Now
	StartTab string           // a tab name or a digit 1 to 5; anything else means Agents
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
