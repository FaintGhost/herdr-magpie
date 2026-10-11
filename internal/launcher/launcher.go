// Package launcher is the herdr sessions popup. It lists magpie sessions newest
// first and marks the ones already open in a herdr pane. Picking an open session
// focuses its pane; picking any other session resumes it in a new tab, or in a new
// workspace when no workspace holds its directory.
package launcher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// Deps are the launcher's collaborators. Herdr, In and Out are required.
type Deps struct {
	Herdr            *herdr.Client
	MagpieBin        string        // magpie CLI; empty means "magpie" on PATH
	Magpie           magpie.Runner // runs the magpie CLI; nil means the real one
	Days             int           // session window; <= 0 means 14
	CurrentWorkspace string        // workspace the launcher runs in; may be empty
	In               *os.File      // terminal to read keys from
	Out              io.Writer     // terminal to draw on
}

const (
	// escTimeout is how long a lone Esc waits for the rest of an escape sequence.
	escTimeout = 50 * time.Millisecond

	enterScreen = "\x1b[?1049h\x1b[?25l" // alternate screen, cursor hidden
	leaveScreen = "\x1b[?25h\x1b[?1049l" // cursor shown, main screen back
)

// Run shows the sessions until the user opens one or quits. It returns nil when
// the user quits or a pick succeeds. Errors from herdr or magpie appear on the
// bottom line and keep the launcher open. So does a session that could not go to
// its own directory: it opens, and its notice shows on the bottom line. The
// terminal is restored on every return path, panics included.
func Run(ctx context.Context, d Deps) error {
	if d.Herdr == nil || d.In == nil || d.Out == nil {
		return errors.New("launcher: Herdr, In and Out are required")
	}
	fd := int(d.In.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("launcher: stdin is not a terminal")
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("launcher: raw mode: %w", err)
	}
	defer func() { _ = term.Restore(fd, state) }()
	if _, err := io.WriteString(d.Out, enterScreen); err != nil {
		return fmt.Errorf("launcher: draw: %w", err)
	}
	defer func() { _, _ = io.WriteString(d.Out, leaveScreen) }()

	// A signal cancels ctx, so a killed launcher still returns through the
	// deferred restores above. Cancelling on return also stops the loads.
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	home, _ := os.UserHomeDir()
	a := &app{
		d:           d,
		fd:          fd,
		color:       os.Getenv("NO_COLOR") == "",
		m:           newModel(home, effectiveDays(d.Days)),
		outstanding: 2,
	}
	defer func() {
		if a.esc != nil {
			a.esc.Stop()
		}
	}()
	a.m.resize(termSize(fd))

	input := readInput(ctx, d.In)
	loads := make(chan any, 2)
	startLoads(ctx, d, loads)
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	if err := a.draw(false); err != nil {
		return err
	}
	var escC <-chan time.Time
	for {
		repaint := false
		select {
		case <-ctx.Done():
			return ctx.Err()
		case in := <-input:
			if in.err != nil {
				return fmt.Errorf("launcher: read input: %w", in.err)
			}
			keys := a.dec.feed(in.data)
			escC = a.rearmEsc()
			if done, err := a.press(ctx, keys); done || err != nil {
				return err
			}
		case <-escC:
			escC = nil
			a.esc = nil
			if done, err := a.press(ctx, a.dec.flush()); done || err != nil {
				return err
			}
		case <-winch:
			a.m.resize(termSize(fd))
			repaint = true
		case v := <-loads:
			a.landed(v)
		}
		if err := a.draw(repaint); err != nil {
			return err
		}
	}
}

// app is the running launcher: the model, the terminal, and the loads in flight.
type app struct {
	d     Deps
	fd    int
	color bool
	m     *model
	dec   keyDecoder
	esc   *time.Timer // settles a lone Esc; nil when no input is pending

	outstanding  int // background loads not yet landed
	sessions     []magpie.Session
	agents       []herdr.Agent
	agentsFailed bool
	loadErrs     []error
}

// draw paints the model. repaint clears the whole screen first, as a resize needs.
func (a *app) draw(repaint bool) error {
	return writeLines(a.d.Out, a.m.frame(time.Now()), a.color, repaint)
}

// rearmEsc restarts the wait that settles a lone Esc after new input. It returns
// the timer's channel while input is pending, and nil otherwise.
func (a *app) rearmEsc() <-chan time.Time {
	if a.esc != nil {
		a.esc.Stop()
	}
	a.esc = nil
	if !a.dec.waiting() {
		return nil
	}
	a.esc = time.NewTimer(escTimeout)
	return a.esc.C
}

// press applies keys in order and reports done when one of them ends the launcher.
func (a *app) press(ctx context.Context, keys []key) (bool, error) {
	for _, k := range keys {
		switch a.m.press(k) {
		case actQuit:
			return true, nil
		case actPick:
			picked, err := a.pick(ctx)
			if picked || err != nil {
				return picked, err
			}
		}
	}
	return false, nil
}

// pick opens the selected session. It reports true when that succeeded, which
// ends the launcher. A failure is left on the bottom line. So is the notice of a
// fallback launch, which keeps the launcher open.
func (a *app) pick(ctx context.Context) (bool, error) {
	m := a.m
	switch it, ok := m.selected(); {
	case !m.loaded:
		m.message = "still loading"
		return false, nil
	case !ok:
		return false, nil
	case it.open == nil && !m.agentsOK:
		m.message = "herdr agents unavailable, so the session is not opened in a new tab"
		return false, nil
	default:
		m.message = "opening…"
		if err := a.draw(false); err != nil {
			return false, err
		}
		where, err := resume(ctx, a.d.Herdr, it, m.agents, a.d.CurrentWorkspace, m.home, isDir)
		if err != nil {
			m.message = err.Error()
			return false, nil
		}
		if where.notice == "" {
			return true, nil
		}
		// The notice is read on the bottom line, so the launcher stays open. The
		// session is open now, so Enter on it must focus its pane, not resume it.
		m.markOpen(it.session.ID, where.open)
		m.message = where.notice
		return false, nil
	}
}

// landed records one background load. When both have landed, the model gets the data.
func (a *app) landed(v any) {
	var err error
	switch v := v.(type) {
	case sessionsLoaded:
		a.sessions, err = v.sessions, v.err
	case agentsLoaded:
		a.agents, err = v.agents, v.err
		a.agentsFailed = err != nil
	}
	if err != nil {
		a.loadErrs = append(a.loadErrs, err)
	}
	a.outstanding--
	if a.outstanding > 0 {
		return
	}
	a.m.setLoaded(a.sessions, a.agents, !a.agentsFailed)
	a.m.message = errorLine(a.loadErrs...)
}

// errorLine joins errs into one line for the bottom of the screen. It is empty
// when there are no errors.
func errorLine(errs ...error) string {
	joined := errors.Join(errs...)
	if joined == nil {
		return ""
	}
	return strings.ReplaceAll(joined.Error(), "\n", "; ")
}

// sessionsLoaded and agentsLoaded carry the two background loads to the loop.
type sessionsLoaded struct {
	sessions []magpie.Session
	err      error
}

type agentsLoaded struct {
	agents []herdr.Agent
	err    error
}

// startLoads fetches the sessions and the agents concurrently. Each goroutine
// sends exactly one message to out, which has room for both, so neither blocks.
// A panic becomes that load's error, so the loop still sees both and can restore
// the terminal.
func startLoads(ctx context.Context, d Deps, out chan<- any) {
	go func() {
		var msg sessionsLoaded
		defer func() {
			if r := recover(); r != nil {
				msg = sessionsLoaded{err: fmt.Errorf("magpie sessions: panic: %v", r)}
			}
			out <- msg
		}()
		msg.sessions, msg.err = loadSessions(ctx, d)
	}()
	go func() {
		var msg agentsLoaded
		defer func() {
			if r := recover(); r != nil {
				msg = agentsLoaded{err: fmt.Errorf("herdr agent list: panic: %v", r)}
			}
			out <- msg
		}()
		msg.agents, msg.err = loadAgents(ctx, d)
	}()
}

// loadSessions lists the magpie sessions in the window.
func loadSessions(ctx context.Context, d Deps) ([]magpie.Session, error) {
	r := d.Magpie
	if r == nil {
		r = magpie.ExecRunner{}
	}
	return magpie.Sessions(ctx, r, d.MagpieBin, d.Days)
}

// loadAgents lists the herdr agent panes.
func loadAgents(ctx context.Context, d Deps) ([]herdr.Agent, error) {
	return d.Herdr.Agents(ctx)
}

// inputMsg is one read from the terminal, or the error that ended reading.
type inputMsg struct {
	data []byte
	err  error
}

// readInput reads the terminal on its own goroutine, so the loop can wait on input,
// the Esc timer and signals together. The goroutine stops once ctx is done.
func readInput(ctx context.Context, in io.Reader) <-chan inputMsg {
	ch := make(chan inputMsg)
	go func() {
		buf := make([]byte, 256)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				select {
				case ch <- inputMsg{data: bytes.Clone(buf[:n])}:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				select {
				case ch <- inputMsg{err: err}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()
	return ch
}

// writeLines paints the frame. Each row is positioned, erased, then written, so
// that no row keeps stale text. repaint clears the screen first.
func writeLines(w io.Writer, lines []line, color, repaint bool) error {
	var buf []byte
	if repaint {
		buf = append(buf, "\x1b[2J"...)
	}
	for i, l := range lines {
		buf = fmt.Appendf(buf, "\x1b[%d;1H\x1b[2K", i+1)
		buf = append(buf, styled(l, color)...)
	}
	_, err := w.Write(buf)
	return err
}

// styled wraps a line in the SGR attributes for its kind.
func styled(l line, color bool) string {
	const reset = "\x1b[0m"
	switch l.kind {
	case kindHeader:
		return "\x1b[1m" + l.text + reset
	case kindSelected:
		return "\x1b[7m" + l.text + reset
	case kindNotice:
		if color {
			return "\x1b[31m" + l.text + reset
		}
	case kindHint:
		return "\x1b[2m" + l.text + reset
	}
	return l.text
}

// termSize returns the terminal size, or 80x24 when it cannot be read.
func termSize(fd int) (width, height int) {
	w, h, err := term.GetSize(fd)
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

// effectiveDays is the session window for days, applying the default.
func effectiveDays(days int) int {
	if days <= 0 {
		return defaultDays
	}
	return days
}
