package panel

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// maxProfileName is the longest profile name the panel accepts, in characters.
const maxProfileName = 64

// noProfilesText is the list's empty state, and the notice for Enter on it.
const noProfilesText = "no profiles yet — press S to save the current setup"

// profileState is the profile list, and the name prompt over it.
type profileState struct {
	open   bool   // the list is on screen
	naming bool   // a name for the current setup is being typed
	name   string // the name typed so far
	cursor int    // the selected profile
}

// profileAppliedMsg is the result of a UseProfile call, with the agents read again
// after it, so that the changed models can be found.
type profileAppliedMsg struct {
	name     string
	changed  []string           // ids of the agents whose model changed
	after    []magpie.AgentInfo // the agents after the apply; nil when they could not be read
	err      error              // the apply's error
	afterErr error              // the re-read's error
}

// profileSavedMsg is the result of a SaveProfile call.
type profileSavedMsg struct {
	name string
	err  error
}

// openProfiles shows the profile list.
func (m *Model) openProfiles() {
	m.prof.open = true
	m.prof.naming = false
}

// startSave opens the name prompt for the current setup.
func (m *Model) startSave() {
	m.prof = profileState{open: true, naming: true}
}

// keyProfiles handles a key while the profile list is open. The list is modal, so
// the tab keys do nothing here.
func (m *Model) keyProfiles(k string) tea.Cmd {
	if m.prof.naming {
		return m.keyProfileName(k)
	}
	p := &m.prof
	profs := m.snap.Profiles.Data
	switch k {
	case "q":
		return tea.Quit
	case "esc":
		p.open = false
	case "up":
		p.cursor = max(p.cursor-1, 0)
	case "down":
		p.cursor = min(p.cursor+1, max(len(profs)-1, 0))
	case "enter":
		if len(profs) == 0 {
			m.setNotice(noticeInfo, noProfilesText)
			return nil
		}
		m.askApply(profs[min(p.cursor, len(profs)-1)])
	case "S":
		p.naming, p.name = true, ""
	}
	return nil
}

// keyProfileName handles a key while the name prompt is open. Every printable key
// is text, q included. Esc goes back to the list.
func (m *Model) keyProfileName(k string) tea.Cmd {
	p := &m.prof
	switch k {
	case "esc":
		p.naming, p.name = false, ""
	case "enter":
		m.askSave(strings.TrimSpace(p.name))
	case "backspace":
		if p.name != "" {
			p.name = dropLastRune(p.name)
		}
	default:
		if r, ok := typedRune(k); ok && utf8.RuneCountInString(p.name) < maxProfileName {
			p.name += string(r)
		}
	}
	return nil
}

// askApply puts "Apply profile <name>?" on the confirm line. Enter applies it, and
// Esc keeps the list open.
func (m *Model) askApply(pr magpie.Profile) {
	m.ask = &confirmLine{
		text: "Apply profile " + clean(pr.Name) + "? Enter/Esc",
		yes: func() tea.Cmd {
			m.prof.open = false
			return m.beginApply(pr.Name)
		},
	}
}

// beginApply starts UseProfile, unless a write is already in flight.
func (m *Model) beginApply(name string) tea.Cmd {
	if !m.beginWrite("applying " + clean(name) + "…") {
		return nil
	}
	return applyProfileCmd(m.ctx, m.act, m.src, name, m.snap.Agents.Data)
}

// applyProfileCmd runs UseProfile off the event loop, then reads the agents again
// and compares their models with before, the list the panel last showed.
func applyProfileCmd(ctx context.Context, act Actions, src Source, name string, before []magpie.AgentInfo) tea.Cmd {
	return func() tea.Msg {
		msg := profileAppliedMsg{name: name}
		msg.err = runWriteErr(ctx, func(ctx context.Context) error {
			if act == nil {
				return notConfigured("profiles")
			}
			return act.UseProfile(ctx, name)
		})
		if msg.err != nil {
			return msg
		}
		if src == nil {
			msg.afterErr = errors.New("no data source configured")
			return msg
		}
		after, err := bounded(ctx, fetchTimeout, src.Agents)
		msg.after, msg.afterErr = after, err
		if err == nil {
			msg.changed = modelChanges(before, after)
		}
		return msg
	}
}

// modelChanges lists the ids of the agents whose model differs between before and
// after. An agent missing from before has nothing to be compared with, so it is not
// listed.
func modelChanges(before, after []magpie.AgentInfo) []string {
	prev := make(map[string]string, len(before))
	for _, a := range before {
		if a.ID != "" {
			prev[a.ID] = a.Model()
		}
	}
	var out []string
	for _, a := range after {
		if old, ok := prev[a.ID]; ok && a.ID != "" && old != a.Model() {
			out = append(out, a.ID)
		}
	}
	return out
}

// landApply records the result of UseProfile. The agents read after the apply
// replace the snapshot's, and the panes of the agents whose model changed are
// planned for a restart.
func (m *Model) landApply(msg profileAppliedMsg) tea.Cmd {
	m.writing = false
	name := clean(msg.name)
	switch {
	case msg.err != nil:
		m.setNotice(noticeBad, "apply failed: "+msg.err.Error())
		return m.afterWrite(nil, allFeeds()...)
	case msg.afterErr != nil:
		m.setNotice(noticeBad, "applied profile "+name+", but the agents could not be read: "+msg.afterErr.Error())
		return m.afterWrite(nil, allFeeds()...)
	}
	m.snap.Agents = stampAt(m.now(), msg.after)
	m.setNotice(noticeOK, "applied profile "+name)
	idle := "applied profile " + name + "; no agent's model changed"
	if len(msg.changed) > 0 {
		idle = "applied profile " + name + "; no Herdr pane runs an agent whose model changed"
	}
	plan := restartPlan{reason: "applying profile " + name, agents: msg.changed, idle: idle}
	return m.afterWrite(&plan, allFeeds()...)
}

// askSave puts "Save current setup as <name>?" on the confirm line, once the name
// is fit to save. Enter saves it; Esc returns to the name prompt.
func (m *Model) askSave(name string) {
	if why := validProfileName(name); why != "" {
		m.setNotice(noticeBad, why)
		return
	}
	m.ask = &confirmLine{
		text: "Save current setup as " + clean(name) + "? Enter/Esc",
		yes: func() tea.Cmd {
			m.prof = profileState{}
			return m.beginSave(name)
		},
	}
}

// validProfileName says why name cannot be a profile's, or "" when it can. The name
// is passed to magpie as an argument, so it must not read as a flag.
func validProfileName(name string) string {
	switch {
	case name == "":
		return "a profile needs a name"
	case strings.HasPrefix(name, "-"):
		return "a profile name cannot start with -"
	case utf8.RuneCountInString(name) > maxProfileName:
		return "a profile name can be at most 64 characters"
	case strings.ContainsFunc(name, unicode.IsControl):
		return "a profile name cannot hold control characters"
	}
	return ""
}

// beginSave starts SaveProfile, unless a write is already in flight.
func (m *Model) beginSave(name string) tea.Cmd {
	if !m.beginWrite("saving " + clean(name) + "…") {
		return nil
	}
	return saveProfileCmd(m.ctx, m.act, name)
}

// saveProfileCmd runs SaveProfile off the event loop, bounded by writeTimeout.
func saveProfileCmd(ctx context.Context, act Actions, name string) tea.Cmd {
	return func() tea.Msg {
		err := runWriteErr(ctx, func(ctx context.Context) error {
			if act == nil {
				return notConfigured("profiles")
			}
			return act.SaveProfile(ctx, name)
		})
		return profileSavedMsg{name: name, err: err}
	}
}

// landSave records the result of SaveProfile and reads the profile list again.
func (m *Model) landSave(msg profileSavedMsg) tea.Cmd {
	m.writing = false
	if msg.err != nil {
		m.setNotice(noticeBad, "save failed: "+msg.err.Error())
	} else {
		m.setNotice(noticeOK, "saved profile "+clean(msg.name))
	}
	return m.afterWrite(nil, feedProfiles)
}

// profileLines draws the profile list in h lines: a heading with the keys, then one
// line per profile with its summary. The selected profile is always on screen.
func (m *Model) profileLines(w, h int) []string {
	st := m.st
	out := []string{line(w, seg{"Profiles", st.bold}, seg{" · Enter apply · S save current setup · Esc close", st.dim})}
	if e := m.feeds[feedProfiles].err; e != "" {
		out = append(out, line(w, seg{"profiles: " + clean(e), st.warn}))
	}
	profs := m.snap.Profiles.Data
	switch {
	case len(profs) == 0 && m.loading(feedProfiles):
		return append(out, line(w, seg{"loading…", st.dim}))
	case len(profs) == 0 && m.feeds[feedProfiles].err == "":
		return append(out, line(w, seg{noProfilesText, st.dim}))
	case len(profs) == 0:
		return out
	}
	cur := min(max(m.prof.cursor, 0), len(profs)-1)
	heights := make([]int, len(profs))
	for i := range heights {
		heights[i] = 1
	}
	start, end := visibleSpan(heights, cur, h-len(out))
	for i := start; i < end; i++ {
		mark, style := "  ", st.base
		if i == cur {
			mark, style = "▸ ", st.bold
		}
		pr := profs[i]
		out = append(out, line(w, seg{mark + fit(clean(pr.Name), 24) + "  ", style}, seg{clean(pr.Summary), st.dim}))
	}
	return out
}
