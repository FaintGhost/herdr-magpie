package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// cmdDoctor checks what the plugin depends on and prints one line per check.
// It exits non-zero when any check fails. The output holds no credentials and
// no session titles or paths; the gateway URL is shown without userinfo.
func cmdDoctor(ctx context.Context, e env, args []string) error {
	fs := newFlagSet("doctor")
	wait := fs.Bool("wait", false, "keep the pane open until Enter (for overlays)")
	paths := addPathFlags(fs)
	if err := parseFlags(fs, e.stdout, args); err != nil {
		return err
	}
	configPath, stateDir, err := resolvePaths(paths)
	if err != nil {
		return err
	}

	r := &reporter{out: e.stdout}
	fmt.Fprintf(e.stdout, "herdr-magpie %s doctor\n\n", version)

	cfg, cfgErr := loadConfig(configPath)
	switch {
	case cfgErr != nil:
		cfg = defaultConfig()
		r.check(false, "config", cfgErr.Error())
	case fileExists(configPath):
		r.check(true, "config", configPath)
	default:
		r.check(true, "config", "defaults (no config.json)")
	}
	if fileExists(stateDir) {
		r.check(true, "state directory", stateDir)
	} else {
		r.check(true, "state directory", stateDir+" (created on first use)")
	}

	magpieBin, magpieErr := magpie.FindBin(cfg.MagpieBin)
	r.result(magpieErr, "magpie CLI", magpieBin)

	herdrBin, herdrErr := herdr.FindBin(cfg.HerdrBin)
	herdrText := herdrBin
	if herdrErr == nil {
		vctx, cancel := context.WithTimeout(ctx, herdr.DefaultTimeout)
		out, err := (herdr.ExecRunner{}).Run(vctx, herdrBin, "--version")
		cancel()
		if err == nil {
			herdrText += " (" + firstLine(out) + ")"
		}
	}
	r.result(herdrErr, "herdr CLI", herdrText)

	gatewayURL, source := magpie.Discover(ctx, magpie.DiscoverOptions{ConfigURL: cfg.GatewayURL})
	health := magpie.Probe(ctx, &http.Client{}, gatewayURL)
	if health.Up {
		r.check(true, "gateway", fmt.Sprintf("%s (%s): up, HTTP %d, %s",
			redactURL(gatewayURL), source, health.Status, roundDuration(health.Latency)))
	} else {
		r.check(false, "gateway", fmt.Sprintf("%s (%s): down: %s", redactURL(gatewayURL), source, health.Err))
	}

	// The one magpie CLI call doctor makes. Every call rewrites magpie's
	// credential file, so it is not repeated for timing.
	var sessions []magpie.Session
	sessionsOK := false
	if magpieErr == nil {
		start := time.Now()
		sessions, err = magpie.Sessions(ctx, magpie.ExecRunner{}, magpieBin, cfg.SessionDays)
		if err != nil {
			r.check(false, "magpie sessions", err.Error())
		} else {
			sessionsOK = true
			r.check(true, "magpie sessions", fmt.Sprintf("%d in the last %d days, %s",
				len(sessions), cfg.SessionDays, roundDuration(time.Since(start))))
		}
	}

	if usagePath, err := usageLogPath(cfg); err != nil {
		r.warn("storm alerts", "usage log unavailable → storm alerts disabled ("+err.Error()+")")
	} else {
		doctorUsageLog(r, usagePath, time.Now())
	}

	if herdrErr == nil {
		agents, err := (&herdr.Client{Bin: herdrBin}).Agents(ctx)
		switch {
		case err != nil:
			r.check(false, "herdr agents", err.Error())
		case sessionsOK:
			byKey := magpie.SessionsByKey(sessions)
			matched := 0
			for _, a := range agents {
				if _, ok := byKey[herdr.SessionKey(a.Session)]; ok {
					matched++
				}
			}
			r.check(true, "herdr agents", fmt.Sprintf("%d agent panes, %d with a magpie session", len(agents), matched))
		default:
			r.check(true, "herdr agents", fmt.Sprintf("%d agent panes", len(agents)))
		}
	}

	if r.failures == 0 {
		fmt.Fprintln(e.stdout, "\nAll checks passed.")
	} else {
		fmt.Fprintf(e.stdout, "\n%d check(s) failed.\n", r.failures)
	}
	if *wait {
		if err := waitForEnter(ctx, e); err != nil {
			return err
		}
	}
	if r.failures > 0 {
		return fmt.Errorf("%d check(s) failed", r.failures)
	}
	return nil
}

// doctorUsageLog reports the usage log that storm alerts read: its size, the age
// of its newest row, the gateway rows of the storm window, and lines the decoder
// did not understand, which would point to a format change in magpie. A missing
// or unreadable log only turns storm alerts off, so it is a warning, not a failure.
func doctorUsageLog(r *reporter, path string, now time.Time) {
	reqs, stats, err := magpie.TailUsageStats(path, time.Time{}, magpie.DefaultUsageTail)
	if err != nil {
		r.warn("storm alerts", "usage log unavailable → storm alerts disabled ("+err.Error()+")")
		return
	}
	var newest time.Time
	recent := 0
	cutoff := now.Add(-stormWindow)
	for _, q := range reqs {
		if q.Time.After(newest) {
			newest = q.Time
		}
		if !q.Time.Before(cutoff) {
			recent++
		}
	}
	last := "no rows"
	if !newest.IsZero() {
		last = "last row " + ago(now.Sub(newest))
	}
	size := ""
	if fi, err := os.Stat(path); err == nil {
		size = " (" + sizeText(fi.Size()) + ")"
	}
	text := fmt.Sprintf("%s%s: %s, %d gateway rows in the last %dm",
		path, size, last, recent, int(stormWindow.Minutes()))
	if stats.Skipped > 0 {
		text += fmt.Sprintf(", %d lines not understood", stats.Skipped)
	}
	r.check(true, "usage log", text)
}

// reporter prints check lines and counts the failures.
type reporter struct {
	out      io.Writer
	failures int
}

func (r *reporter) check(ok bool, name, text string) {
	mark := "ok"
	if !ok {
		mark = "FAIL"
		r.failures++
	}
	fmt.Fprintf(r.out, "%-5s %-16s %s\n", mark, name, text)
}

// warn prints a line for a feature that is degraded. It does not count as a failure.
func (r *reporter) warn(name, text string) {
	fmt.Fprintf(r.out, "%-5s %-16s %s\n", "warn", name, text)
}

// result reports a lookup that succeeded with okText or failed with err.
func (r *reporter) result(err error, name, okText string) {
	if err != nil {
		r.check(false, name, err.Error())
		return
	}
	r.check(true, name, okText)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// firstLine returns the first line of b, trimmed.
func firstLine(b []byte) string {
	line, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(line)
}

func roundDuration(d time.Duration) string {
	return d.Round(time.Millisecond).String()
}

// ago renders an age in whole seconds, such as "3m0s ago".
func ago(d time.Duration) string {
	return d.Round(time.Second).String() + " ago"
}

// sizeText renders a byte count in KiB or MiB.
func sizeText(n int64) string {
	if n < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}
