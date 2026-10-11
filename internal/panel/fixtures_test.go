package panel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// sampleAgents are magpie's agents with their fields, synthetic: Claude Code and
// Codex, and a hidden DeepSeek Harness that herdr has no label for.
func sampleAgents() []magpie.AgentInfo {
	return []magpie.AgentInfo{
		{Name: "Claude Code", ID: "claude", ConfigPath: "~/.claude/settings.json", Fields: []magpie.AgentField{
			{Name: "model", Value: "claude-opus-4-1"}, {Name: "effort", Value: "high"}, {Name: "thinking", Value: "on"},
		}},
		{Name: "Codex", ID: "codex", ConfigPath: "~/.codex/config.toml", Fields: []magpie.AgentField{
			{Name: "model", Value: "gpt-5-codex"}, {Name: "effort", Value: "—"},
		}},
		{Name: "DeepSeek Harness", ID: "dsh", ConfigPath: "~/.dsh/config.json", Hidden: true, Fields: []magpie.AgentField{
			{Name: "model", Value: "deepseek-flash"},
		}},
	}
}

// samplePanes are herdr's agent panes, synthetic. Two claude panes are working and
// idle, a done claude pane names a session magpie does not list, an omp pane's
// session has no resume command, a codex pane is blocked, and an opencode pane has
// a label no magpie agent maps to.
func samplePanes() []herdr.Agent {
	pane := func(id, agent, status, cwd, session string) herdr.Agent {
		a := herdr.Agent{
			Agent: agent, Status: status, CWD: cwd, PaneID: id,
			WorkspaceID: strings.SplitN(id, ":", 2)[0], Focused: false,
		}
		if session != "" {
			a.Session = &herdr.AgentSession{Agent: agent, Kind: "id", Source: "test", Value: session}
		}
		return a
	}
	return []herdr.Agent{
		pane("w3H:p1", "claude", "working", "/home/u/pp/app", "AAAA-CLAUDE-1"),
		pane("w20:p1", "claude", "idle", "/home/u/pp/docs", "bbbb-claude-2"),
		pane("w3G:p1", "claude", "done", "/home/u/pp/gone", "eeee-unknown"),
		pane("w2P:p3", "omp", "idle", "/home/u/pp/tool", "cccc-omp-3"),
		pane("w36:p1", "codex", "blocked", "/home/u/pp/x", "dddd-codex-4"),
		pane("w40:p1", "opencode", "idle", "/home/u/pp/other", "ffff-other-5"),
	}
}

// sampleSessions are magpie's sessions, synthetic, with the resume commands magpie
// prints. A session is newest first by Last.
func sampleSessions() []magpie.Session {
	ago := func(d time.Duration) time.Time { return testNow.Add(-d) }
	return []magpie.Session{
		{Agent: "claude", ID: "aaaa-claude-1", CWD: "/home/u/pp/app", Title: "修复登录",
			Last: ago(5 * time.Minute), Cost: 1.25,
			Resume: "cd '/home/u/pp/app' && claude --resume aaaa-claude-1"},
		{Agent: "claude", ID: "bbbb-claude-2", CWD: "/home/u/pp/docs", Title: "写文档",
			Last: ago(time.Hour), Cost: 0.4,
			Resume: "cd '/home/u/pp/docs' && claude --resume bbbb-claude-2"},
		{Agent: "omp", ID: "cccc-omp-3", CWD: "/home/u/pp/tool", Title: "",
			Last: ago(2 * time.Hour)},
		{Agent: "codex", ID: "dddd-codex-4", CWD: "/home/u/pp/x", Title: "codex job",
			Last: ago(3 * time.Hour), Cost: 0.01,
			Resume: "cd '/home/u/pp/x' && codex resume dddd-codex-4"},
		{Agent: "claude", ID: "gggg-free-6", CWD: "/home/u/pp/free", Title: "没有窗格的会话",
			Last: ago(20 * time.Minute), Cost: 0.2,
			Resume: "cd '/home/u/pp/free' && claude --resume gggg-free-6"},
	}
}

// samplePanelSource is sampleSource with the sessions, the profiles and agents of
// this file in place of its own.
func samplePanelSource() *fakeSource {
	src := sampleSource()
	src.agents = sampleAgents()
	src.sessions = sampleSessions()
	src.profiles = []magpie.Profile{
		{Name: "work", Summary: "claude x · codex y"},
		{Name: "cheap", Summary: "claude z"},
	}
	src.requests = sampleTTFTRequests(testNow)
	return src
}

// sampleTTFTRequests are gateway requests with their times to first token: one
// swapped in the last hour, one failed, one older than the hour that was swapped,
// and a log row, which never passed the gateway.
func sampleTTFTRequests(now time.Time) []magpie.Request {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	return []magpie.Request{
		{Time: ago(2 * time.Minute), Agent: "omp", Provider: "openai", RequestedModel: "gpt-x", ServedModel: "gpt-y",
			Swapped: true, Status: 200, TTFT: 800 * time.Millisecond},
		{Time: ago(5 * time.Minute), Agent: "claude", Provider: "claude", RequestedModel: "claude-sonnet",
			ServedModel: "claude-sonnet", Status: 429, Error: true, ErrorType: "rate_limit"},
		{Time: ago(30 * time.Minute), Agent: "hermes", Provider: "deepseek", RequestedModel: "deepseek-chat",
			ServedModel: "deepseek-chat", Status: 200, TTFT: 1200 * time.Millisecond},
		{Time: ago(90 * time.Minute), Agent: "kimi", Provider: "kimi", RequestedModel: "k1", ServedModel: "k2",
			Status: 200, TTFT: 400 * time.Millisecond},
		{Time: ago(3 * time.Minute), Agent: "claude", Provider: "claude", Source: "log", Status: 200},
	}
}

// manyRequests is n gateway requests, one a minute, newest first.
func manyRequests(now time.Time, n int) []magpie.Request {
	out := make([]magpie.Request, 0, n)
	for i := range n {
		out = append(out, magpie.Request{
			Time: now.Add(-time.Duration(i) * time.Minute), Agent: "omp", Provider: fmt.Sprintf("p%02d", i),
			Status: 200,
		})
	}
	return out
}

// panelModel builds a model over the panel fixtures, with the herdr view h, loaded
// and sized to w by h.
func panelModel(t *testing.T, src *fakeSource, h *fakeHerdr, act *fakeActions, w, ht int) *Model {
	t.Helper()
	d := Deps{Source: src}
	if h != nil {
		d.Herdr = h
	}
	if act != nil {
		d.Actions = act
	}
	m := sized(t, d, w, ht)
	m.home = fixtureHome
	return m
}

// fixtureHome is the home directory the fixtures' paths are written from.
const fixtureHome = "/home/u"
