package panel

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

const (
	// snapshotFile is the cache entry the panel keeps in the state directory.
	snapshotFile = "panel.json"
	// snapshotVersion is bumped when the snapshot's shape changes. Other versions are ignored.
	snapshotVersion = 1
)

var (
	// fetchTimeout bounds each Source method. It is a variable so tests can shorten it.
	fetchTimeout = 6 * time.Second
	// switchTimeout bounds SwitchAccount, a write that may take longer.
	switchTimeout = 30 * time.Second

	errTimeout = errors.New("timed out")
)

// stamp is one feed's data and the time it was read. A zero At means never read.
type stamp[T any] struct {
	At   time.Time `json:"at,omitzero"`
	Data T         `json:"data"`
}

// stampAt is the stamp of v read at at.
func stampAt[T any](at time.Time, v T) stamp[T] {
	return stamp[T]{At: at, Data: v}
}

// snapshot is what the panel keeps between runs: the last good reading of each feed.
type snapshot struct {
	Version  int                         `json:"version"`
	Agents   stamp[[]magpie.AgentInfo]   `json:"agents"`
	Quotas   stamp[[]magpie.Quota]       `json:"quotas"`
	Accounts stamp[[]magpie.Account]     `json:"accounts"`
	Profiles stamp[[]magpie.Profile]     `json:"profiles"`
	Requests stamp[[]magpie.Request]     `json:"requests"`
	APIUsage stamp[*magpie.UsageSummary] `json:"api_usage"`
}

// at is when feed f was last read, zero when never.
func (s snapshot) at(f feed) time.Time {
	switch f {
	case feedAgents:
		return s.Agents.At
	case feedQuotas:
		return s.Quotas.At
	case feedAccounts:
		return s.Accounts.At
	case feedProfiles:
		return s.Profiles.At
	case feedRequests:
		return s.Requests.At
	case feedAPIUsage:
		return s.APIUsage.At
	}
	return time.Time{}
}

// Results of the fetches. Each carries the Source's answer for one feed.
type (
	agentsMsg struct {
		v   []magpie.AgentInfo
		err error
	}
	quotasMsg struct {
		v   []magpie.Quota
		err error
	}
	accountsMsg struct {
		v   []magpie.Account
		err error
	}
	profilesMsg struct {
		v   []magpie.Profile
		err error
	}
	requestsMsg struct {
		v   []magpie.Request
		err error
	}
	apiUsageMsg struct {
		v   *magpie.UsageSummary
		err error
	}
)

// savedMsg reports the result of writing the snapshot.
type savedMsg struct{ err error }

// tickMsg wakes the panel so that its ages and countdowns are redrawn.
type tickMsg struct{}

// tickCmd schedules the next tickMsg.
func tickCmd() tea.Cmd {
	return tea.Tick(tickEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

// fetch runs f through bounded and turns its answer into a message.
func fetch[T any](ctx context.Context, wrap func(T, error) tea.Msg, f func(context.Context) (T, error)) tea.Cmd {
	return func() tea.Msg {
		v, err := bounded(ctx, fetchTimeout, f)
		return wrap(v, err)
	}
}

// bounded runs f with a context that ends after d, and returns its result. It
// returns at the deadline even when f ignores its context, so a stuck source
// cannot hold a feed forever. A panic in f becomes an error.
func bounded[T any](parent context.Context, d time.Duration, f func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeoutCause(parent, d, errTimeout)
	defer cancel()

	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		var r result
		defer func() {
			if p := recover(); p != nil {
				r.err = fmt.Errorf("panic: %v", p)
			}
			done <- r
		}()
		r.v, r.err = f(ctx)
	}()

	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		var zero T
		if errors.Is(context.Cause(ctx), errTimeout) {
			return zero, fmt.Errorf("timed out after %s", d)
		}
		return zero, context.Cause(ctx)
	}
}

// saveCmd writes snap to the cache, off the event loop.
func saveCmd(c CacheStore, snap snapshot) tea.Cmd {
	return func() tea.Msg {
		return savedMsg{err: c.Save(snapshotFile, snap)}
	}
}
