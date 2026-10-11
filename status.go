package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
	"github.com/FaintGhost/herdr-magpie/internal/state"
)

const (
	gatewayFailsDown = 2                // consecutive failed probes before the gateway counts as down
	discoveryTTL     = 10 * time.Minute // how long a discovered gateway URL is reused
	discoveryBudget  = 700 * time.Millisecond
	probeBudget      = 1500 * time.Millisecond
	toastBudget      = 800 * time.Millisecond
	usageReadBudget  = 300 * time.Millisecond  // soft budget for reading the usage log; past it the last reading stands
	statusBudget     = 2800 * time.Millisecond // whole status run; herdr kills status at 3 s
	refreshStale     = 5 * time.Minute         // age of the last successful refresh after which status starts one
	stormVisible     = 10 * time.Minute        // age up to which status shows storms
	logLimit         = 1 << 20                 // refresh.log moves to refresh.log.1 past this size
)

// errBudget is the cancellation cause of a status step that ran out of time.
var errBudget = errors.New("status time budget exceeded")

// transition is a confirmed change of gateway state.
type transition int

const (
	noChange transition = iota
	wentDown
	cameBack
)

// gatewayState is the debounced view of the gateway. Confirmed stays false
// until the first observation settles, so the first state after install never toasts.
type gatewayState struct {
	Confirmed bool      `json:"confirmed,omitzero"`
	Up        bool      `json:"up,omitzero"`
	Fails     int       `json:"consecutive_fail,omitzero"`
	Since     time.Time `json:"since,omitzero"`
}

// observe folds one probe result into the state. The gateway is confirmed down
// only after gatewayFailsDown failed probes in a row; until then the previous
// state stands. Only a change between two confirmed states is a transition.
func (g gatewayState) observe(up bool, now time.Time) (gatewayState, transition) {
	if up {
		next := gatewayState{Confirmed: true, Up: true, Since: g.Since}
		var tr transition
		switch {
		case !g.Confirmed:
			next.Since = now
		case !g.Up:
			next.Since, tr = now, cameBack
		}
		return next, tr
	}
	fails := min(g.Fails+1, gatewayFailsDown)
	if g.Confirmed && !g.Up {
		return gatewayState{Confirmed: true, Up: false, Fails: fails, Since: g.Since}, noChange
	}
	if fails < gatewayFailsDown {
		return gatewayState{Confirmed: g.Confirmed, Up: g.Up, Fails: fails, Since: g.Since}, noChange
	}
	next := gatewayState{Confirmed: true, Up: false, Fails: fails, Since: now}
	if g.Confirmed {
		return next, wentDown
	}
	return next, noChange
}

// down reports whether the gateway is confirmed down.
func (g gatewayState) down() bool {
	return g.Confirmed && !g.Up
}

// gatewayToast builds the notification for a confirmed gateway transition.
func gatewayToast(tr transition, shownURL, reason string) (title, body, sound string) {
	if tr == wentDown {
		body = shownURL
		if reason != "" {
			body += " · " + reason
		}
		return "magpie gateway down", body, "request"
	}
	return "magpie gateway back", shownURL, "done"
}

// statusLine renders the one-line segment. A confirmed outage hides storms.
// With color, a dot marks the state the way system-stats does: green, yellow or red.
func statusLine(down bool, storms []cachedStorm, color bool) string {
	var mark, text string
	switch {
	case down:
		mark, text = "🔴", "◇ magpie down"
	case len(storms) > 0:
		worst := slices.MaxFunc(storms, func(a, b cachedStorm) int {
			return cmp.Compare(a.Count, b.Count)
		})
		mark = "🟡"
		text = "◆ magpie ⚠ " + stormLabel(worst)
		if more := len(storms) - 1; more > 0 {
			text += " +" + strconv.Itoa(more)
		}
	default:
		mark, text = "🟢", "◆ magpie"
	}
	if color {
		return mark + " " + text
	}
	return text
}

// stormLabel is "provider status×count", for example "opencode-go 429×438".
func stormLabel(s cachedStorm) string {
	return fmt.Sprintf("%s %s×%d", s.Provider, statusCode(s.Status), s.Count)
}

// colorsEnabled follows system-stats: NO_COLOR, even when empty, and TERM=dumb turn the dots off.
func colorsEnabled() bool {
	_, noColor := os.LookupEnv("NO_COLOR")
	return !noColor && os.Getenv("TERM") != "dumb"
}

// visibleStorms returns the storms of the last reading while that reading is
// recent enough to vouch for them. An older reading shows nothing.
func visibleStorms(v stormView, now time.Time) []cachedStorm {
	age := now.Sub(v.RefreshedAt)
	if v.RefreshedAt.IsZero() || age < 0 || age > stormVisible {
		return nil
	}
	return v.Storms
}

// needsRefresh reports whether status should start a refresh. The last successful
// refresh must be older than refreshStale, and status must not have started one
// within refreshStale either, so a failing magpie CLI is retried once per interval.
func needsRefresh(now, refreshedAt, lastSpawn time.Time) bool {
	if age := now.Sub(refreshedAt); !refreshedAt.IsZero() && age >= 0 && age <= refreshStale {
		return false
	}
	gap := now.Sub(lastSpawn)
	return lastSpawn.IsZero() || gap < 0 || gap >= refreshStale
}

// statusState is status.json. Only status writes it.
type statusState struct {
	Gateway   gatewayState `json:"gateway,omitzero"`
	Discovery discovery    `json:"discovery,omitzero"`
	LastSpawn time.Time    `json:"last_spawn,omitzero"`
	Storms    stormView    `json:"storms,omitzero"`       // the last storm reading, which status shows
	Alerts    stormState   `json:"storm_alerts,omitzero"` // storm toast cooldowns and active storms
}

// stormView is the storm reading that status shows. Each tick that reads the
// usage log replaces it.
type stormView struct {
	RefreshedAt time.Time     `json:"refreshed_at,omitzero"`
	Storms      []cachedStorm `json:"storms,omitempty"`
}

// cachedStorm is one active storm as status shows it.
type cachedStorm struct {
	Provider string `json:"provider"`
	Status   int    `json:"status"`
	Count    int    `json:"count"`
}

// stormState is what status keeps between ticks to decide toasts.
type stormState struct {
	Active    []string             `json:"active,omitempty"`
	LastToast map[string]time.Time `json:"last_toast,omitempty"`
}

// discovery is the gateway URL that magpie.Discover found, and the configured
// URL it was found for.
type discovery struct {
	URL       string    `json:"url,omitempty"`
	ConfigURL string    `json:"config_url,omitempty"`
	At        time.Time `json:"at,omitzero"`
}

// statusDeps are the effects of status. cmdStatus wires the real ones; tests
// substitute fakes.
type statusDeps struct {
	cfg       config
	store     *state.Store // nil when the state directory is unusable
	now       time.Time
	herdr     *herdr.Client // nil when the herdr CLI is missing
	color     bool
	stderr    io.Writer
	probe     func(context.Context, string) magpie.Health
	discover  func(context.Context, string) string
	spawn     func(context.Context) error // nil when there is no state to refresh into
	usagePath string                      // empty when the usage log path is unknown; storm alerts are then off
	readUsage func(path string, since time.Time) ([]magpie.Request, error)
	// usageBudget bounds one usage-log read. Zero means usageReadBudget.
	usageBudget time.Duration
}

// cmdStatus prints the tab-bar segment. It never fails: problems go to stderr
// and the segment still prints, because herdr shows nothing else.
func cmdStatus(ctx context.Context, e env, args []string) error {
	fs := newFlagSet("status")
	paths := addPathFlags(fs)
	if err := parseFlags(fs, e.stdout, args); err != nil {
		if errors.Is(err, errHelp) {
			return nil
		}
		logf(e.stderr, "status: %v", err)
	}
	cfg, err := loadConfig(*paths.config)
	if err != nil {
		logf(e.stderr, "status: %v; using defaults", err)
		cfg = defaultConfig()
	}
	d := statusDeps{
		cfg:       cfg,
		now:       e.now(),
		color:     colorsEnabled(),
		stderr:    e.stderr,
		probe:     probeGateway,
		discover:  discoverGateway,
		readUsage: tailUsageLog,
	}
	if path, err := usageLogPath(cfg); err != nil {
		logf(e.stderr, "status: %v; storm alerts are off", err)
	} else {
		d.usagePath = path
	}
	if stateDir := *paths.stateDir; stateDir != "" {
		if store, err := state.Open(stateDir); err != nil {
			logf(e.stderr, "status: state directory: %v", err)
		} else {
			d.store = store
			d.spawn = e.backgroundRefresh(*paths.config, stateDir, cfg, store)
		}
	}
	if bin, err := herdr.FindBin(cfg.HerdrBin); err != nil {
		logf(e.stderr, "status: %v; notifications are off", err)
	} else {
		d.herdr = &herdr.Client{Bin: bin, Runner: e.herdrRunner(), Timeout: toastBudget}
	}
	ctx, cancel := context.WithTimeout(ctx, statusBudget)
	defer cancel()
	fmt.Fprintln(e.stdout, status(ctx, d))
	return nil
}

// backgroundRefresh returns how status starts a refresh. Normally that is a
// detached process. In dry-run mode it runs in-process and waits, so the writes
// it would make are printed while status is still running. That run is not
// bounded by the status budget, so the dry run shows a whole refresh.
func (e env) backgroundRefresh(configPath, stateDir string, cfg config, store *state.Store) func(context.Context) error {
	if e.dryRun {
		return func(ctx context.Context) error {
			return runRefresh(context.WithoutCancel(ctx), newRefreshDeps(e, cfg, store))
		}
	}
	return func(context.Context) error {
		return spawnRefresh(configPath, stateDir)
	}
}

// tailUsageLog reads the usage rows since the given time, for storm detection.
func tailUsageLog(path string, since time.Time) ([]magpie.Request, error) {
	return magpie.TailUsage(path, since, magpie.DefaultUsageTail)
}

// readUsageWithin runs read and waits at most budget for it. A read that runs
// over is abandoned with errBudget. Its goroutine ends with the process.
func readUsageWithin(ctx context.Context, budget time.Duration, read func() ([]magpie.Request, error)) ([]magpie.Request, error) {
	type result struct {
		reqs []magpie.Request
		err  error
	}
	done := make(chan result, 1)
	go func() {
		reqs, err := read()
		done <- result{reqs, err}
	}()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.reqs, r.err
	case <-timer.C:
		return nil, errBudget
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}

// probeGateway checks the gateway with a plain HTTP client.
func probeGateway(ctx context.Context, gatewayURL string) magpie.Health {
	return magpie.Probe(ctx, &http.Client{}, gatewayURL)
}

// discoverGateway returns the gateway URL that magpie would use.
func discoverGateway(ctx context.Context, configURL string) string {
	u, _ := magpie.Discover(ctx, magpie.DiscoverOptions{ConfigURL: configURL})
	return u
}

// status computes the segment and performs its effects: it probes the gateway,
// reads the usage log for storms, toasts confirmed transitions, and starts a
// refresh when the last one is stale. Toasts go out before the state is saved,
// so the saved state includes the cooldown entries that a failed toast removed.
func status(ctx context.Context, d statusDeps) string {
	var st statusState
	if d.store != nil {
		if err := d.store.Load(statusFile, &st); err != nil {
			logf(d.stderr, "status: %v; starting from empty state", err)
			st = statusState{}
		}
	}
	gatewayURL := d.resolveGateway(ctx, &st)
	pctx, cancel := context.WithTimeout(ctx, probeBudget)
	health := d.probe(pctx, gatewayURL)
	cancel()

	gw, tr := st.Gateway.observe(health.Up, d.now)
	st.Gateway = gw
	toasts := d.updateStorms(ctx, &st)
	d.startRefreshIfDue(ctx, &st)
	d.announce(ctx, tr, gatewayURL, health)
	d.sendStormToasts(ctx, toasts, &st)
	if d.store != nil {
		if err := d.store.Save(statusFile, st); err != nil {
			logf(d.stderr, "status: save state: %v", err)
		}
	}
	return statusLine(gw.down(), visibleStorms(st.Storms, d.now), d.color)
}

// resolveGateway returns the URL to probe. A discovery cached for discoveryTTL
// is reused while the configured URL is unchanged. A discovery cut short by its
// budget is not cached, and the last good answer for this configuration stands in.
func (d statusDeps) resolveGateway(ctx context.Context, st *statusState) string {
	cached := st.Discovery
	sameConfig := cached.URL != "" && cached.ConfigURL == d.cfg.GatewayURL
	if age := d.now.Sub(cached.At); sameConfig && age >= 0 && age < discoveryTTL {
		return cached.URL
	}
	dctx, cancel := context.WithTimeoutCause(ctx, discoveryBudget, errBudget)
	defer cancel()
	found := d.discover(dctx, d.cfg.GatewayURL)
	if context.Cause(dctx) != nil {
		if sameConfig {
			return cached.URL
		}
		return found
	}
	st.Discovery = discovery{URL: found, ConfigURL: d.cfg.GatewayURL, At: d.now}
	return found
}

// updateStorms reads the recent usage rows, records the storms they show, and
// returns the toasts for storms that have just started. Without a usable usage
// log, storm detection is off: nothing is shown and nothing is toasted. A read
// that runs over its budget leaves the last reading in place.
func (d statusDeps) updateStorms(ctx context.Context, st *statusState) []stormToast {
	if d.usagePath == "" || d.readUsage == nil {
		st.Storms = stormView{}
		return nil
	}
	budget := cmp.Or(d.usageBudget, usageReadBudget)
	reqs, err := readUsageWithin(ctx, budget, func() ([]magpie.Request, error) {
		return d.readUsage(d.usagePath, d.now.Add(-stormWindow))
	})
	if err != nil {
		switch {
		case errors.Is(err, errBudget), ctx.Err() != nil:
			// Too slow this tick: the last reading stands while it is recent enough to show.
		case errors.Is(err, magpie.ErrNoUsageLog):
			st.Storms = stormView{}
		default:
			logf(d.stderr, "status: usage log: %v; storm alerts are paused", err)
			st.Storms = stormView{}
		}
		return nil
	}
	storms := computeStorms(reqs, d.now)
	toasts, alerts := planStorms(d.now, storms, st.Alerts, d.cfg.Notify && d.herdr != nil)
	st.Alerts = alerts
	view := stormView{RefreshedAt: d.now}
	for _, s := range storms {
		if s.Active {
			view.Storms = append(view.Storms, cachedStorm{Provider: s.Provider, Status: s.Status, Count: s.Count})
		}
	}
	st.Storms = view
	return toasts
}

// startRefreshIfDue starts a background refresh when the last successful one is
// stale and status has not started one within refreshStale.
func (d statusDeps) startRefreshIfDue(ctx context.Context, st *statusState) {
	if d.spawn == nil || d.store == nil {
		return
	}
	var rs refreshState
	if err := d.store.Load(refreshFile, &rs); err != nil {
		logf(d.stderr, "status: %v", err)
	}
	if !needsRefresh(d.now, rs.LastOK, st.LastSpawn) || !d.refreshIdle() {
		return
	}
	if err := d.spawn(ctx); err != nil {
		logf(d.stderr, "status: start refresh: %v", err)
		return
	}
	st.LastSpawn = d.now
}

// sendStormToasts sends the storm toasts. A toast that fails is forgotten, so
// the next tick plans it again.
func (d statusDeps) sendStormToasts(ctx context.Context, toasts []stormToast, st *statusState) {
	if d.herdr == nil {
		return
	}
	for _, t := range toasts {
		if err := d.herdr.Notify(ctx, t.title, t.body, "request"); err != nil {
			logf(d.stderr, "status: notify: %v", err)
			delete(st.Alerts.LastToast, t.key)
		}
	}
}

// announce sends the toast for a confirmed gateway transition, when notifications are on.
func (d statusDeps) announce(ctx context.Context, tr transition, gatewayURL string, h magpie.Health) {
	if tr == noChange || !d.cfg.Notify || d.herdr == nil {
		return
	}
	title, body, sound := gatewayToast(tr, redactURL(gatewayURL), h.Err)
	if err := d.herdr.Notify(ctx, title, body, sound); err != nil {
		logf(d.stderr, "status: notify: %v", err)
	}
}

// refreshIdle reports whether no refresh holds the lock. It takes the lock
// without waiting and releases it at once, so the answer is only a hint: the
// refresh takes the lock itself and exits quietly when it is busy.
func (d statusDeps) refreshIdle() bool {
	unlock, ok, err := d.store.Lock(true)
	unlock()
	return err == nil && ok
}

// spawnRefresh starts `herdr-magpie refresh` in a new session and does not wait
// for it. Its stdin is /dev/null, and its output is appended to refresh.log.
func spawnRefresh(configPath, stateDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	out, err := openRefreshLog(filepath.Join(stateDir, refreshLog))
	if err != nil {
		return err
	}
	defer out.Close()
	fmt.Fprintf(out, "--- %s spawn refresh\n", time.Now().UTC().Format(time.RFC3339))
	cmd := exec.Command(exe, "refresh", "--config", configPath, "--state-dir", stateDir)
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// openRefreshLog opens path for appending. A file past logLimit moves to
// path+".1" first, so at most two files remain.
func openRefreshLog(path string) (*os.File, error) {
	if fi, err := os.Stat(path); err == nil && fi.Size() > logLimit {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
