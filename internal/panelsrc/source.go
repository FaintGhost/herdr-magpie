// Package panelsrc connects the magpie panel to magpie, the gateway and Herdr. It
// implements panel.Source and panel.Actions, and keeps the panel's snapshot in the
// plugin state directory. The panel package itself never runs a command.
package panelsrc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/panel"
	"github.com/FaintGhost/herdr-magpie/internal/state"
)

// Compile-time checks: the panel's interfaces are satisfied as the panel expects.
var (
	_ panel.Source     = Source{}
	_ panel.Actions    = Actions{}
	_ panel.HerdrView  = (*herdr.Client)(nil)
	_ panel.CacheStore = Cache{}
)

// Source reads what the panel shows. Magpie CLI calls go through Magpie with Bin,
// the usage log is read from UsagePath, and the gateway is read through API.
type Source struct {
	Magpie    magpie.Runner
	Bin       string           // magpie CLI path; empty means "magpie"
	UsagePath string           // magpie's usage.jsonl
	Days      int              // session window for Sessions; <= 0 means magpie's default
	API       magpie.APIClient // gateway base URL and key file; APIUsage is nil without a key
}

// Agents runs `magpie ls`.
func (s Source) Agents(ctx context.Context) ([]magpie.AgentInfo, error) {
	return magpie.Agents(ctx, s.Magpie, s.Bin)
}

// Quotas runs `magpie quota --json`.
func (s Source) Quotas(ctx context.Context) ([]magpie.Quota, error) {
	return magpie.Quotas(ctx, s.Magpie, s.Bin)
}

// Accounts runs `magpie accounts --json`.
func (s Source) Accounts(ctx context.Context) ([]magpie.Account, error) {
	return magpie.Accounts(ctx, s.Magpie, s.Bin)
}

// Profiles runs `magpie profiles`.
func (s Source) Profiles(ctx context.Context) ([]magpie.Profile, error) {
	return magpie.Profiles(ctx, s.Magpie, s.Bin)
}

// RecentRequests returns the gateway's usage rows at or after since, from the
// usage log. It reads at most magpie.DefaultUsageTail bytes from the log's end. A
// log that does not exist yet means no requests, not an error.
func (s Source) RecentRequests(ctx context.Context, since time.Time) ([]magpie.Request, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reqs, err := magpie.TailUsage(s.UsagePath, since, magpie.DefaultUsageTail)
	if errors.Is(err, magpie.ErrNoUsageLog) {
		return nil, nil
	}
	return reqs, err
}

// APIUsage reads today's summary from the gateway. It is nil, with no error, when
// no gateway key is set.
func (s Source) APIUsage(ctx context.Context) (*magpie.UsageSummary, error) {
	return s.API.Usage(ctx)
}

// Sessions runs `magpie sessions --json` over the session window.
func (s Source) Sessions(ctx context.Context) ([]magpie.Session, error) {
	return magpie.Sessions(ctx, s.Magpie, s.Bin, s.Days)
}

// snapshotFile is the state file that holds the panel's snapshot. The panel names
// it the same way.
const snapshotFile = "panel.json"

// Cache is the panel's snapshot store over the plugin state directory. It reads
// and writes the snapshot file only, and refuses any other name.
type Cache struct {
	Store *state.Store
}

// Load reads the snapshot into v.
func (c Cache) Load(name string, v any) error {
	if name != snapshotFile {
		return fmt.Errorf("panel cache: %q is not %s", name, snapshotFile)
	}
	return c.Store.Load(name, v)
}

// Save writes the snapshot from v.
func (c Cache) Save(name string, v any) error {
	if name != snapshotFile {
		return fmt.Errorf("panel cache: %q is not %s", name, snapshotFile)
	}
	return c.Store.Save(name, v)
}
