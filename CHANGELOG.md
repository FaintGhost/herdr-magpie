# Changelog

## 0.1.0 - 2026-10-12

- Show magpie gateway health in Herdr's top bar, with a notification when the gateway goes down and when it comes back (two failed probes confirm down).
- Detect upstream error storms per provider from magpie's usage log (5 failures in 10 minutes, or a single 402), show the worst one in the top bar and notify once per storm with a 30-minute cooldown.
- Report `$magpie_model` and `$magpie_cost` agent-sidebar tokens and a `$magpie_cost` workspace-sidebar token from `magpie sessions`.
- Add a session launcher popup across all agents magpie knows: filter, focus an open session, or resume it in the matching workspace, or in a new workspace named after the project directory.
- Provide setup and doctor panes; setup prints safely quoted configuration and never edits user files.
- Keep magpie CLI calls to about one every five minutes, because each call rewrites magpie's credential file.
- Support Herdr servers from 0.9.1; document that sidebar rows and keybindings belong in the client's config under `herdr --remote`.
