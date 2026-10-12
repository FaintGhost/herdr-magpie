# Changelog

## Unreleased

- Add the magpie panel (`prefix+m`, action `open-panel`): Agents, Allowances, Usage, Routing and Sessions tabs in one Herdr popup, painted from a cached snapshot and refreshed in the background.
- Agents shows each agent's model and reasoning with the Herdr panes running it; opening the panel from a pane selects that pane's agent.
- Allowances groups subscriptions by agent with inline window bars, balances and sign-in errors, and switches the active account after a confirmation line.
- Usage shows requests, all errors, provider errors and TTFT p50 for 10m, 1h and today, plus today's gateway cost by agent and model when a gateway key is set.
- Routing lists recent gateway requests and marks a real model swap; a provider prefix alone is no longer counted as one.
- Profiles can be applied and saved from the panel; after a profile or account change, idle panes whose agent changed can be restarted one by one from their session's resume command.
- Read magpie's documented gateway API with an optional gateway key file (`gateway-key`, 0600) for allowances and cost; doctor checks it without printing it.
- Measure text with grapheme-aware widths, so emoji with a variation selector and CJK titles stay aligned.
- `prefix+m` now opens the panel; the session launcher is its Sessions tab.

## 0.1.0 - 2026-10-12

- Show magpie gateway health in Herdr's top bar, with a notification when the gateway goes down and when it comes back (two failed probes confirm down).
- Detect upstream error storms per provider from magpie's usage log (5 failures in 10 minutes, or a single 402), show the worst one in the top bar and notify once per storm with a 30-minute cooldown.
- Report `$magpie_model` and `$magpie_cost` agent-sidebar tokens and a `$magpie_cost` workspace-sidebar token from `magpie sessions`.
- Add a session launcher popup across all agents magpie knows: filter, focus an open session, or resume it in the matching workspace, or in a new workspace named after the project directory.
- Provide setup and doctor panes; setup prints safely quoted configuration and never edits user files.
- Keep magpie CLI calls to about one every five minutes, because each call rewrites magpie's credential file.
- Support Herdr servers from 0.9.1; document that sidebar rows and keybindings belong in the client's config under `herdr --remote`.
