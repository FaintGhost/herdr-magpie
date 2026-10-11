package magpie

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Quotas runs `magpie quota --json` and returns the subscription allowances.
func Quotas(ctx context.Context, r Runner, bin string) ([]Quota, error) {
	out, err := runCLI(ctx, r, bin, "quota", "--json")
	if err != nil {
		return nil, err
	}
	var raw []rawQuota
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("magpie quota: decode: %w", err)
	}
	quotas := make([]Quota, 0, len(raw))
	for _, q := range raw {
		quotas = append(quotas, q.quota())
	}
	return quotas, nil
}

// Accounts runs `magpie accounts --json` and returns the signed-in accounts.
func Accounts(ctx context.Context, r Runner, bin string) ([]Account, error) {
	out, err := runCLI(ctx, r, bin, "accounts", "--json")
	if err != nil {
		return nil, err
	}
	var raw []rawAccount
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("magpie accounts: decode: %w", err)
	}
	accounts := make([]Account, 0, len(raw))
	for _, a := range raw {
		accounts = append(accounts, a.account())
	}
	return accounts, nil
}

// The raw forms mirror magpie's JSON. Times stay strings: magpie leaves them out,
// or prints "", when it has none, and that must not fail the whole list.

type rawQuota struct {
	Provider     string      `json:"provider"`
	Name         string      `json:"name"`
	Kind         string      `json:"kind"`
	Plan         string      `json:"plan"`
	User         string      `json:"user"`
	Windows      []rawWindow `json:"windows"`
	ReadAt       string      `json:"readAt"`
	LastServedAt string      `json:"lastServedAt"`
}

type rawAccount struct {
	Agent   string      `json:"agent"`
	User    string      `json:"user"`
	Plan    string      `json:"plan"`
	Active  bool        `json:"active"`
	On      bool        `json:"on"`
	Windows []rawWindow `json:"windows"`
	ReadAt  string      `json:"readAt"`
}

type rawWindow struct {
	Name      string  `json:"name"`
	Used      float64 `json:"used"`
	Remaining float64 `json:"remaining"`
	ResetsAt  string  `json:"resetsAt"`
}

func (q rawQuota) quota() Quota {
	return Quota{
		Provider:     q.Provider,
		Name:         q.Name,
		Kind:         q.Kind,
		Plan:         q.Plan,
		User:         q.User,
		Windows:      windows(q.Windows),
		ReadAt:       looseTime(q.ReadAt),
		LastServedAt: looseTime(q.LastServedAt),
	}
}

func (a rawAccount) account() Account {
	return Account{
		Agent:   a.Agent,
		User:    a.User,
		Plan:    a.Plan,
		Active:  a.Active,
		On:      a.On,
		Windows: windows(a.Windows),
		ReadAt:  looseTime(a.ReadAt),
	}
}

func windows(raw []rawWindow) []QuotaWindow {
	out := make([]QuotaWindow, 0, len(raw))
	for _, w := range raw {
		out = append(out, QuotaWindow{
			Name:      w.Name,
			Used:      w.Used,
			Remaining: w.Remaining,
			ResetsAt:  looseTime(w.ResetsAt),
		})
	}
	return out
}

// looseTime parses a magpie timestamp. An empty or unreadable one is the zero time.
func looseTime(s string) time.Time {
	t, err := parseRFC3339(s)
	if err != nil {
		return time.Time{}
	}
	return t
}
