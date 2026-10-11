package magpie

import "time"

// Types the Herdr panel reads. They are the contract between the data layer
// (this package) and internal/panel; change them only together with both.

// AgentInfo is one row of `magpie ls`: an agent magpie detected and the
// values of its fields as magpie shows them.
type AgentInfo struct {
	Name       string       // display name, such as "Claude Code"
	ID         string       // CLI id, such as "claude"; empty when it cannot be derived
	ConfigPath string       // the config file magpie edits, as printed (may start with ~)
	Fields     []AgentField // model first, then effort and the rest, in printed order
	Hidden     bool         // hidden from magpie's app, which lists it last and dimmed
}

// AgentField is one field of an agent, such as model or effort.
type AgentField struct {
	Name  string // "model", "effort", "thinking", "subagent model", ...
	Value string // as printed; "—" or "" when unset
}

// Model reports the agent's model field.
func (a AgentInfo) Model() string {
	for _, f := range a.Fields {
		if f.Name == "model" {
			return f.Value
		}
	}
	return ""
}

// ModelOption is one row of `magpie models <agent>`: a model the agent can use,
// or a routing group, with the effort levels magpie lists for it.
type ModelOption struct {
	ID             string   // "deepseek/deepseek-flash", "group/auto-glm-5-3"
	DisplayName    string   // "DeepSeek V4.1 Flash"; empty when magpie prints none
	Group          string   // the provider's name, or "Routing groups"
	Efforts        []string // "low", "high", "max"; nil when none are listed
	IsRoutingGroup bool
}

// Quota is one entry of `magpie quota --json`.
type Quota struct {
	Provider     string        `json:"provider"`
	Name         string        `json:"name"`
	Kind         string        `json:"kind"` // "subscription", "balance", ...
	Plan         string        `json:"plan"`
	User         string        `json:"user"`
	Windows      []QuotaWindow `json:"windows"`
	ReadAt       time.Time     `json:"readAt"`
	LastServedAt time.Time     `json:"lastServedAt"`
	Balance      string        `json:"balance"` // a balance kind's amount, already formatted by magpie
	Error        string        `json:"error"`   // why magpie could not read this allowance; empty when it could
}

// QuotaWindow is one allowance window, such as "5 hours" or "7 days".
// Used and Remaining are percentages.
type QuotaWindow struct {
	Name      string    `json:"name"`
	Used      float64   `json:"used"`
	Remaining float64   `json:"remaining"`
	ResetsAt  time.Time `json:"resetsAt"`
}

// Account is one entry of `magpie accounts --json`.
type Account struct {
	Agent   string        `json:"agent"`
	User    string        `json:"user"`
	Plan    string        `json:"plan"`
	Active  bool          `json:"active"`
	On      bool          `json:"on"`
	Windows []QuotaWindow `json:"windows"`
	ReadAt  time.Time     `json:"readAt"`
}

// Profile is one saved profile from `magpie profiles`.
type Profile struct {
	Name    string
	Summary string // what the profile sets, such as "claude x · codex y"; may be empty
}

// UsageSummary is the part of the gateway's GET /v1/magpie/usage the panel
// shows. It is nil when no gateway key is configured.
type UsageSummary struct {
	Calls  int
	Cost   float64
	Agents []UsageRow
	Models []UsageRow
}

// UsageRow is one agent's or model's share of UsageSummary.
type UsageRow struct {
	Name   string
	Calls  int
	Errors int
	Cost   float64
}
