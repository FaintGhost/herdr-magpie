// Command herdr-magpie is a Herdr plugin. It shows magpie gateway health and
// upstream error storms in the tab bar, per-session model and cost in the
// sidebar, and a launcher for magpie sessions.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/FaintGhost/herdr-magpie/internal/herdr"
	"github.com/FaintGhost/herdr-magpie/internal/magpie"
)

// errUsage marks a command-line mistake (exit 2). errHelp marks a request for
// help, which is a success.
var (
	errUsage = errors.New("usage error")
	errHelp  = errors.New("help requested")
)

const usageText = `Usage: herdr-magpie <command> [options]

Commands:
  status      one line for the tab bar: gateway state and error storms
  refresh     recompute storms and session tokens (alias: sync)
  sessions    magpie session launcher; Enter opens or resumes a session
  panel       magpie panel: agents, allowances, usage, routing and sessions
  setup       print the config snippets to merge into herdr's config.toml
  doctor      check magpie, herdr, the gateway and the CLI calls
  version     print the plugin version

Run 'herdr-magpie <command> --help' for the options of one command.
HERDR_MAGPIE_DRY_RUN=1 prints herdr writes instead of running them.
`

// env is what the commands need from the process. Tests build their own.
type env struct {
	stdin  *os.File // launcher input and the --wait prompt; may be nil
	stdout io.Writer
	stderr io.Writer
	dryRun bool
	now    func() time.Time
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	e := env{
		stdin:  os.Stdin,
		stdout: os.Stdout,
		stderr: os.Stderr,
		dryRun: dryRunEnabled(),
		now:    time.Now,
	}
	err := run(ctx, e, os.Args[1:])
	stop()
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return
	case errors.Is(err, errUsage):
		fmt.Fprintf(os.Stderr, "herdr-magpie: %v\nRun 'herdr-magpie --help' for usage.\n", err)
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "herdr-magpie: %v\n", err)
		os.Exit(1)
	}
}

// run dispatches one command. With no arguments it prints the usage.
func run(ctx context.Context, e env, args []string) error {
	if len(args) == 0 {
		_, err := io.WriteString(e.stdout, usageText)
		return err
	}
	name, rest := args[0], args[1:]
	var err error
	switch name {
	case "-h", "--help", "help":
		_, err = io.WriteString(e.stdout, usageText)
		return err
	case "version", "--version":
		_, err = fmt.Fprintf(e.stdout, "herdr-magpie %s\n", version)
		return err
	case "status":
		return cmdStatus(ctx, e, rest)
	case "refresh", "sync":
		err = cmdRefresh(ctx, e, rest)
	case "sessions":
		err = cmdSessions(ctx, e, rest)
	case "panel":
		err = cmdPanel(ctx, e, rest)
	case "setup":
		err = cmdSetup(ctx, e, rest)
	case "doctor":
		err = cmdDoctor(ctx, e, rest)
	default:
		return fmt.Errorf("%w: unknown command %q", errUsage, name)
	}
	if errors.Is(err, errHelp) {
		return nil
	}
	return err
}

// newFlagSet creates a subcommand's flag set. Errors and usage are reported by
// parseFlags, so the flag package itself stays silent.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("herdr-magpie "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses one subcommand's arguments. -h prints that command's
// options to stdout and returns errHelp. Other problems wrap errUsage.
func parseFlags(fs *flag.FlagSet, stdout io.Writer, args []string) error {
	err := fs.Parse(args)
	switch {
	case errors.Is(err, flag.ErrHelp):
		fmt.Fprintf(stdout, "Usage: %s [options]\n", fs.Name())
		fs.SetOutput(stdout)
		fs.PrintDefaults()
		return errHelp
	case err != nil:
		return fmt.Errorf("%w: %v", errUsage, err)
	case fs.NArg() > 0:
		return fmt.Errorf("%w: unexpected argument %q", errUsage, fs.Arg(0))
	}
	return nil
}

// pathFlags are the --config and --state-dir options. Their defaults come from
// defaultPaths, which may be empty when no home directory is known.
type pathFlags struct {
	config   *string
	stateDir *string
}

func addPathFlags(fs *flag.FlagSet) pathFlags {
	configDefault, stateDefault, _ := defaultPaths()
	return pathFlags{
		config:   fs.String("config", configDefault, "optional config.json"),
		stateDir: fs.String("state-dir", stateDefault, "plugin state directory"),
	}
}

// resolvePaths returns the chosen config path and state directory.
func resolvePaths(pf pathFlags) (configPath, stateDir string, err error) {
	configPath, stateDir = *pf.config, *pf.stateDir
	if configPath == "" || stateDir == "" {
		return "", "", errors.New("no config or state path: set HOME, or pass --config and --state-dir")
	}
	return configPath, stateDir, nil
}

// herdrRunner runs herdr for real, or in dry-run mode prints the writes instead.
func (e env) herdrRunner() herdr.Runner {
	if e.dryRun {
		return dryRunner{out: e.stderr, inner: herdr.ExecRunner{}}
	}
	return herdr.ExecRunner{}
}

// magpieRunner runs the magpie CLI for real, or in dry-run mode prints the magpie
// writes (use, save, accounts switch) instead. The reads still run.
func (e env) magpieRunner() magpie.Runner {
	if e.dryRun {
		return dryMagpieRunner{out: e.stderr, inner: magpie.ExecRunner{}}
	}
	return magpie.ExecRunner{}
}

// dryRunEnabled reads HERDR_MAGPIE_DRY_RUN. Unset or unparsable means off.
func dryRunEnabled() bool {
	on, _ := strconv.ParseBool(os.Getenv("HERDR_MAGPIE_DRY_RUN"))
	return on
}

// dryRunner runs the read-only herdr commands and prints every other call to
// out instead of running it. Writes return a stub result that callers can decode.
type dryRunner struct {
	out   io.Writer
	inner herdr.Runner
}

// Run implements herdr.Runner.
func (d dryRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if isHerdrRead(args) {
		return d.inner.Run(ctx, name, args...)
	}
	fmt.Fprintf(d.out, "dry-run: would run %s\n", quoteArgv(name, args))
	return []byte(`{"result":{"workspace":{"workspace_id":"dry-run-workspace"},"root_pane":{"pane_id":"dry-run-pane"}}}`), nil
}

// isHerdrRead reports whether args name one of the herdr commands the plugin
// only reads. Everything else counts as a write.
func isHerdrRead(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] + " " + args[1] {
	case "agent list", "agent get", "workspace list":
		return true
	}
	return false
}

// dryMagpieRunner runs the magpie reads and prints every other call to out instead.
// A printed write returns no output and no error, so its caller carries on as if
// it had run.
type dryMagpieRunner struct {
	out   io.Writer
	inner magpie.Runner
}

// Run implements magpie.Runner.
func (d dryMagpieRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if isMagpieRead(args) {
		return d.inner.Run(ctx, name, args...)
	}
	fmt.Fprintf(d.out, "dry-run: would run %s\n", quoteArgv(name, args))
	return nil, nil
}

// isMagpieRead reports whether args, the arguments after "magpie", name a command
// that only reads. Anything else counts as a write. The accounts command reads
// only in its listing form, "accounts" or "accounts --json"; "accounts switch" writes.
func isMagpieRead(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "ls", "profiles", "models", "quota", "sessions", "usage":
		return true
	case "accounts":
		return len(args) == 1 || (len(args) == 2 && args[1] == "--json")
	}
	return false
}

// quoteArgv renders a command for display. Arguments with spaces or quotes are Go-quoted.
func quoteArgv(name string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	for _, a := range append([]string{name}, args...) {
		if strings.ContainsAny(a, " \t\n\"'") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// logf writes one diagnostic line to w. Stdout is reserved for the status line,
// the launcher and the reports, so diagnostics never go there.
func logf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "herdr-magpie: "+format+"\n", args...)
}
