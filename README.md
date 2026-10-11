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
- An optional `[[keys.command]]` entry, `prefix+m` by default, that invokes the `open-sessions` action.

Sidebar layout and keybindings are read from the **client's** config. If you attach with `herdr --remote`, put the sidebar rows and the keybinding in the `config.toml` of the machine you attach from; the top-bar entry stays on the server.

Setup never edits your configuration. Keep your existing entries, do not add a second `[ui]` table, then run `herdr config check` and reload the config from Herdr's menu or with `herdr server reload-config`.

### Session launcher

Press the keybinding from setup (`prefix+m`, that is Ctrl+B then M with the default prefix), or run:

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint sessions
```

The popup lists sessions newest first. Type to filter by agent, title or directory. Arrow keys or Ctrl-P and Ctrl-N move. Enter acts: it focuses a session that is already open. Otherwise it creates a tab in the workspace that has an agent in the session's directory, or a new workspace for that directory when none does, and runs the session's resume command there. When the session has no directory or its directory is gone, the tab opens in the current workspace and the popup stays open with a notice. Esc or Ctrl-C quits.

### Doctor

```sh
herdr plugin pane open --plugin faintghost.magpie --entrypoint doctor
```

Doctor checks the config, the magpie and herdr binaries, the gateway probe (with the gateway URL and its source), the time of the one magpie call it makes, the usage log (its path, size, the age of its newest row, and how many gateway rows fall in the last ten minutes), and how many agent panes match a magpie session. A missing usage log is a warning: storm alerts are off. Doctor exits non-zero only when a check fails. Its output contains no credentials and no session titles or paths.

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

- It never reads magpie's settings, logins, auth or key files, and it does not need a gateway key.
- It runs only one magpie command, `magpie sessions --json --limit 100000`, and it is read-only. Every magpie CLI call rewrites magpie's credential file, so the plugin calls it about once every five minutes, and at most once a minute for events.
- It sends one unauthenticated `GET /v1/models` to the gateway per status tick. A 401 that identifies itself as magpie counts as up.
- It reads magpie's usage log, `~/.config/magpie/usage.jsonl` by default, read-only. It reads from the end of the file, at most 4 MiB at a time. The log is magpie's own gateway log, not a documented interface, so its format may change between magpie versions. Lines it cannot read are skipped, and doctor counts them.
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
