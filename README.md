# Herdr Magpie

[![CI](https://github.com/FaintGhost/herdr-magpie/actions/workflows/ci.yml/badge.svg)](https://github.com/FaintGhost/herdr-magpie/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**[magpie](https://github.com/yetone/magpie) gateway health, upstream error storms, per-session model and cost, and a cross-agent session launcher, inside [Herdr](https://herdr.dev).**

[简体中文](README.zh-CN.md)

```text
🟢 ◆ magpie                              tab bar: gateway is up
🟡 ◆ magpie ⚠ opencode-go 429×438 +1    tab bar: worst active storm, plus one more
🔴 ◇ magpie down                        tab bar: gateway confirmed down

claude-sonnet-x +1  $1.75               agent sidebar (illustrative)
$3.12                                   workspace sidebar (illustrative)
```

The tab bar reads magpie's usage log for storms, so it never waits on the magpie CLI. Session costs come from one `magpie sessions` call, made about every five minutes, or at most once a minute when an agent changes state. Nothing needs a gateway key. The only network request the plugin makes is an anonymous health probe to the gateway.

## What it shows

| Where | What | When it updates |
| --- | --- | --- |
| Tab bar | Gateway state, or the worst active error storm | Every 10 seconds |
| Agent sidebar | `$magpie_model`: the models the pane's session used, costliest first. `$magpie_cost`: the session's cost | When an agent's status changes (idle, done, blocked, or detected), at most once a minute; in the background about every five minutes |
| Workspace sidebar | `$magpie_cost`: total cost of the sessions open in the workspace | Same as the agent sidebar |
| Notifications | Gateway down and back; a provider storm starting | When the state changes |
| Popup pane | Session launcher: every magpie session across agents. Enter focuses an open one. Otherwise it opens a tab in the workspace that holds the session's directory, or a new workspace for that directory, and resumes it there | On demand |
| Popup pane (panel) | Panel: agents, allowances, usage, routing and sessions in one full-screen popup. Profile and account switches run only after confirmation | On demand |

Storms are provider-level. A storm means several recent requests to one provider, through the gateway, failed with the same status code. It does not name a pane, because gateway requests carry no session ID.

## Requirements

| Requirement | Supported |
| --- | --- |
| Host OS | Linux |
| Herdr | 0.9.1 or later (server) |
| magpie | The `magpie` CLI on `PATH`, or at `~/.local/bin/magpie`, or set `magpie_bin` |
| Build tools | Go 1.26+ and Git on the machine running the Herdr server |
| Runtime | One compiled binary; no daemon |

Installation compiles the binary from source. The only Go dependencies are `golang.org/x/term` (the launcher's raw terminal mode) and `golang.org/x/text` (East Asian cell widths); installation downloads the pinned versions. No CGO.

## Install

```sh
herdr plugin install FaintGhost/herdr-magpie
```

Review Herdr's installation preview. The build command creates `bin/herdr-magpie`; the plugin ID is **`faintghost.magpie`**.

### Add the top bar and sidebar tokens

Open the setup pane:

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint setup
```

It prints TOML that you merge into your Herdr config by hand. The printed paths are absolute, so the entries work even when herdr's server has a minimal `PATH`:

- `tab_bar_right`: one command entry that runs `status` every 10 seconds, guarded so that a missing binary prints nothing.
- `[ui.sidebar.agents]` and `[ui.sidebar.spaces]`: `$magpie_*` tokens added to rows you already show.
- An optional `[[keys.command]]` entry: `prefix+m` invokes the `open-panel` action. The panel includes the session launcher as its Sessions tab; the `open-sessions` action still opens the launcher alone if you bind it.

Sidebar layout and keybindings are read from the **client's** config. If you attach with `herdr --remote`, put the sidebar rows and the keybinding in the `config.toml` of the machine you attach from; the top-bar entry stays on the server.

Setup never edits your configuration. Keep your existing entries, do not add a second `[ui]` table, then run `herdr config check` and reload the config from Herdr's menu or with `herdr server reload-config`.

### Session launcher

Open the panel's Sessions tab (`prefix+m`, then `5`), or open the launcher alone with:

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint sessions
```

The popup lists sessions newest first. Type to filter by agent, title or directory. Arrow keys or Ctrl-P and Ctrl-N move. Enter acts: it focuses a session that is already open. Otherwise it creates a tab in the workspace that has an agent in the session's directory, or a new workspace for that directory when none does, and runs the session's resume command there. When the session has no directory or its directory is gone, the tab opens in the current workspace and the popup stays open with a notice. Esc or Ctrl-C quits.

### Panel

Press `prefix+m` (Ctrl+B, then M with the default prefix), or run:

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint panel
```

The panel fills most of the screen and has five tabs. Tab and Shift+Tab move between them, and the keys 1 to 5 jump straight to one:

- **Agents**: each agent magpie knows, with its model and effort, and the Herdr panes that run it with their status. Read-only.
- **Allowances**: subscription windows with the share used, the reset countdown, balances, read errors, and each agent's accounts. Enter on an account picks it to switch to.
- **Usage**: requests in the last 10 minutes and the last hour, today's requests and errors by provider, time to first token, and active storms. With a gateway key it adds today's calls and cost from the gateway.
- **Routing**: recent requests, each shown as requested → served.
- **Sessions**: the session launcher as a tab. Enter opens a session the same way the launcher does.

The panel paints its last snapshot at once, from `panel.json` in the plugin state directory, and refreshes every feed in the background. A feed that fails shows its error where its data would be, and the other feeds keep working.

Keys: Up and Down move through a list, and Enter acts on the selected row. `/` filters Sessions. `p` opens the profile list, and `S` saves the current setup as a profile. `r` refreshes now. `q`, Esc or Ctrl-C quits.

Writes change magpie or Herdr. Each one first shows a confirmation line: Enter runs it, and Esc cancels it.

- Apply a saved profile (`magpie use <name>`) or save the current setup as one (`magpie save <name>`). The panel never deletes a profile.
- Switch the account an agent uses (`magpie accounts switch <agent> <account>`).
- After a profile apply or an account switch, the panel offers to restart the Herdr panes that run an affected agent, one pane at a time. A pane is restarted only when it is idle or done. The panel checks that again just before it acts, closes the pane, and resumes its session in a new tab in the same workspace and directory.

Not offered: changing models or efforts, deleting profiles, logins and logouts, provider or gateway-key changes, and `magpie plugin`.

**Gateway key (optional).** The Usage tab's gateway figures (today's calls and cost) need a gateway key, which is a secret. Routing and the rest of Usage read the usage log and need no key. Create one without any limit, and store it in the plugin config directory, beside `config.json`:

```sh
magpie gateway-key add herdr
```

Store the key magpie prints as a single line in `gateway-key` in that directory, with mode 0600. Writing it with `cat` keeps the key off the command line:

```sh
dir="$(herdr plugin config-dir faintghost.magpie)"
(umask 077 && cat > "$dir/gateway-key")   # paste the key, press Enter, then Ctrl-D
chmod 600 "$dir/gateway-key"
```

Create the key without a budget, model or account limit. A limited key is told only its own calls, and the gateway refuses it the usage summary, so the panel would show part of the usage and not all of it. The plugin sends the key only with `GET /api/hello` and `GET /v1/magpie/*`, never prints it and never puts it on a command line. Doctor checks the file's mode and that the gateway accepts the key. Without a key the panel works and shows no gateway figures.

### Doctor

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint doctor
```

Doctor checks the config, the magpie and herdr binaries, the gateway probe (with the gateway URL and its source), the time of the one magpie call it makes, the usage log (its path, size, the age of its newest row, and how many gateway rows fall in the last ten minutes), how many agent panes match a magpie session, and the gateway key. A missing usage log or a missing gateway key is a warning: storm alerts are off, or the panel shows no gateway figures. A key file that is not mode 0600, or that the gateway refuses, is a failure. Doctor exits non-zero only when a check fails. Its output contains no credentials and no session titles or paths.

## Configuration

Configuration is optional. Find the directory with:

```sh
herdr plugin config-dir faintghost.magpie
```

Create `config.json` there:

```json
{
  "gateway_url": "",
  "magpie_bin": "",
  "herdr_bin": "",
  "notify": true,
  "session_days": 14,
  "usage_path": ""
}
```

| Key | Default | Meaning |
| --- | --- | --- |
| `gateway_url` | empty | Gateway base URL. Empty means discover it from the `magpie.service` environment, then `MAGPIE_GATEWAY_URL`, `MAGPIE_PUBLIC_URL`, `MAGPIE_ADDR`, and finally `http://127.0.0.1:3425` |
| `magpie_bin` | empty | Absolute path to the magpie CLI. Empty means `PATH`, then `~/.local/bin/magpie` |
| `herdr_bin` | empty | Absolute path to the herdr CLI. Empty means `HERDR_BIN_PATH`, then `PATH`, then the mise shim |
| `notify` | `true` | Send gateway and storm notifications |
| `session_days` | `14` | How far back the session list and the session tokens look |
| `usage_path` | empty | magpie's usage log, read for storm alerts. Empty means `$XDG_CONFIG_HOME/magpie/usage.jsonl`, or `~/.config/magpie/usage.jsonl` when `XDG_CONFIG_HOME` is unset. A value must be an absolute path |

Unknown keys are errors, so a typo cannot silently do nothing. Configuration is read on every status tick, so most edits take effect within ten seconds.

## Notification rules

- **Gateway.** A probe that fails twice in a row confirms the gateway is down. The first confirmed down, and every change after it, sends a notification. A single failed probe changes nothing.
- **Storms.** Gateway failures in the last ten minutes are grouped by provider and status code. Five failures make a storm; a single 402 (balance or payment) is enough. Responses 499 (the client cancelled) and 400 (the caller sent a bad request) are not counted, and neither are log rows, which come from agents that call providers directly.
- **Cooldown.** A storm notifies once when it starts, then waits 30 minutes before notifying again for the same provider and status code.
- **Source.** Storms are read from magpie's usage log (see `usage_path`). When the log is missing or unreadable, storm alerts are off and doctor says so. The plugin never polls the magpie CLI for storms.

## Privacy

- It never reads magpie's settings, logins, auth or key files. Status and refresh need no gateway key. The panel can use one, as described under Panel.
- It runs only one magpie command, `magpie sessions --json --limit 100000`, and it is read-only. Every magpie CLI call rewrites magpie's credential file, so the plugin calls it about once every five minutes, and at most once a minute for events.
- It sends one unauthenticated `GET /v1/models` to the gateway per status tick. A 401 that identifies itself as magpie counts as up.
- It reads magpie's usage log, `~/.config/magpie/usage.jsonl` by default, read-only. It reads from the end of the file, at most 4 MiB at a time. The log is magpie's own gateway log, not a documented interface, so its format may change between magpie versions. Lines it cannot read are skipped, and doctor counts them.
- The panel runs the magpie commands behind what it shows: `ls`, `quota --json`, `accounts --json`, `profiles` and `sessions --json`. It runs `use`, `save` and `accounts switch` only after you confirm. It sends the gateway key only with GET requests to `/api/hello` and `/v1/magpie/*`.
- It reads Herdr state with `herdr agent list` and `herdr workspace list`. The status bar and the refresh write only the plugin's metadata tokens and notifications. The launcher writes only when you press Enter: it creates a tab or a workspace, focuses a pane or workspace, and runs the resume command.
- Its state lives in Herdr's plugin state directory: `status.json`, `refresh.json`, `refresh.log`, and a lock file. It never edits Herdr's or magpie's configuration.
- Notifications never include session titles, paths or account names. Storm notifications show the provider, status code, most frequent error type and the busiest agent names.

## Known limitations

- Gateway requests have no session ID, so storms name a provider, not a pane.
- Model and cost come from `magpie sessions`. A pane whose agent has no magpie session shows no tokens.
- A magpie CLI call takes one to two seconds, and longer when the machine is busy. Session costs refresh in the background, not on each tick. A call that exceeds five seconds fails and is retried on the next refresh.
- Storm counts cover the last ten minutes of the usage log. The plugin reads at most the last 4 MiB of it, which covers far more than ten minutes at current volume, but a much busier gateway could undercount storms. A read that runs over its 300 ms budget keeps the previous reading for up to ten minutes.
- Costs are magpie's own estimates.
- The first gateway state seen after install never notifies. Only changes between two confirmed states do.
- The panel does not change models, efforts, providers or routing groups. Use `magpie tui` for those. It misdetects a LAN-only gateway, and plugin log lines can corrupt its screen (upstream issue #1677).

## Develop

```sh
git clone https://github.com/FaintGhost/herdr-magpie.git
cd herdr-magpie
go build -trimpath -ldflags='-s -w' -o bin/herdr-magpie .
go vet ./...
go test -race ./...
test -z "$(gofmt -l .)"
```

`HERDR_MAGPIE_DRY_RUN=1` runs every herdr write as a printed command instead of executing it. Use it with `status` or `refresh` to see what would change against live data, and keep `HERDR_PLUGIN_STATE_DIR` pointed at a scratch directory while you do.

| File | Responsibility |
| --- | --- |
| `main.go` | Subcommands, flags, the dry-run wrapper for herdr |
| `config.go` | Paths, strict `config.json` parsing, URL redaction |
| `status.go` | Gateway debounce, storm alerts from the usage log, the tab-bar line, background refresh start |
| `refresh.go` | Storm computation (pure), session token updates |
| `sessions.go` | Wires the session launcher |
| `setup.go`, `doctor.go` | Printed snippets and diagnostics |
| `internal/magpie` | Gateway discovery and probe, sessions, usage CSV, usage log tail |
| `internal/herdr` | Agents, workspaces, metadata, notifications, tabs |
| `internal/state` | Locked, atomic JSON state files |
| `internal/launcher` | The session launcher |

## Update or remove

Update by running `herdr plugin install FaintGhost/herdr-magpie` again. To remove, delete the plugin's entries from `tab_bar_right`, the sidebar rows and the keybinding, reload Herdr's config, then run:

```sh
herdr plugin uninstall faintghost.magpie
```

Herdr keeps the plugin's config and state directories after uninstall. Delete them yourself if you want no trace.

## License

[MIT](LICENSE). Bug reports and contributions: [GitHub Issues](https://github.com/FaintGhost/herdr-magpie/issues).
