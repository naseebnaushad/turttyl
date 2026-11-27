---
type: feature
area: terminal
title: Shared desktop terminal for people and AI agents, with hand-off to the person and optional sudo sessions
authors: [Naseeb]
branch: feature/turttyl
cr: CR-0.1.0-01
after_the_fact: false
---

## What changed and why
New product. Agents used to run shell commands where nobody could see them. Turttyl is a
desktop app (Go and Wails v3.1, React and xterm.js) whose terminal
sessions are shared by the person at the app and by AI agents connected over MCP
(`turttyl mcp`).

- Agent input is typed one character at a time (default 20 ms per character), and the
  app shows "<agent> is typing…". If a person types, the agent's input stops and the
  agent gets `interrupted_by_human`.
- Agents get seven MCP tools: `list_sessions`, `create_session`, `run_command` (returns
  output and exit code), `type`, `send_keys`, `read_output` (with a cursor) and
  `close_session`.
- **Elevated sessions**, off by default: when `elevation.enabled` is set in
  `~/.turttyl/config.json`, a person can start one, unlock it by typing the
  sudo password in the terminal, and agents can then run commands as root with no
  password prompt. They end on close, when the app quits, or on an idle timeout.
- **Hand-off to the person:** agents no longer get stuck at prompts.
  - When a command asks for a **password**, agents can't type into it. The session is
    flagged "needs you" and the Dock icon bounces. The person types the password and
    the agent's `run_command`/`wait_for_command` carries on by itself.
  - When a command goes quiet at a **question**, `run_command` returns early with
    `running`, `lastLine` and `looksLikePrompt`.
  - New tools: `request_human_input(reason)`, which waits until the person clicks
    **Hand back** (⌘↵) or the command finishes, and `wait_for_command`.
- **Window design** follows Apple's Human Interface Guidelines: sessions are tabs in
  the title bar (⌘T new, ⌘1–9 switch, × or middle-click to close), the macOS window uses
  native Liquid Glass, the terminal sits on an opaque card, and notices, the new-session
  menu and the close-confirmation sheet use a glass material. Light and dark follow the
  system; Reduce Transparency, Reduce Motion and Increase Contrast are respected.
- The product is named **turttyl** (Go module `github.com/naseebnaushad/turttyl`,
  settings in `~/.turttyl/`, environment variables `TURTTYL_*`).
- Audit log of sessions, elevation, hand-offs and agent input in
  `~/.turttyl/audit.jsonl`.

## How it works (logic)
- `internal/app/manager.go`: the use cases. The elevation rules live here: config must
  enable it, only `domain.Human` may create a session, and agents are refused while the
  state is `authenticating`.
- `internal/app/session.go`: the per-session read loop, output ring buffer (cursors are
  byte offsets), and typing with interruption (a person's write closes the interrupt
  channel).
- Command completion: `internal/adapters/ptyshell/scripts/` makes zsh and bash
  (including bash 3.2 on macOS) emit OSC 133 markers: `A` for a prompt, `C` when output
  starts, and `D;code` when the command has finished. `internal/app/textproc.go` finds
  the markers even when they are split across reads. `run_command` returns the text
  between our `C` and `D`, with escape codes removed.
- zsh loads the user's own rc files through a ZDOTDIR chain and gives ZDOTDIR back at
  the end. Elevated shells run `sudo -k -H -- /bin/bash` with the rc passed inline
  through process substitution, so root never reads a file from a folder the user can
  write to.
- `internal/app/handoff.go`: the hand-off. `passwordWatcher` reads each PTY's mode four
  times a second (`ptyshell/termios.go`: echo off with ICANON on means a password
  prompt). `waitCommand` waits for a command's D marker, returns early on idle (but
  never while the person is being asked), and counts idle from the latest of the last
  output, the last hand-off and the start of the wait. `RequestHuman` and `HandBack`
  manage the `attention` flag, which also clears when the command finishes.
- `internal/adapters/control`: the Unix socket (0600, 0700 folder, peer UID check,
  single instance). Every caller is an agent, and there is no parameter that asks for
  elevation.
- `internal/adapters/desktop`: the Wails service and an output bridge that batches
  output to about 60 events a second. The UI draws the snapshot first, then live output
  after that offset.

## Tests added and results
Run on macOS (darwin/arm64), 2026-11-27.

| ID | Case | Result |
|---|---|---|
| TERMINAL-001 | Elevation off by default; config file without it, or with bad values, never enables it | ✅ unit |
| TERMINAL-002 | Agents cannot create an elevated session (core rule and socket API) | ✅ unit |
| TERMINAL-003 | Agents cannot type, read or send keys while an elevated session is authenticating; the password never reaches the audit log | ✅ unit |
| TERMINAL-004 | Agent typing is character by character; a person typing interrupts it | ✅ unit |
| TERMINAL-005 | Only one agent types into a session at a time (`busy`) | ✅ unit |
| TERMINAL-006 | `run_command` returns exact output and exit code on zsh and bash (real PTY) | ✅ integration |
| TERMINAL-007 | The user's own `.zshrc` still loads and ZDOTDIR is restored | ✅ integration |
| TERMINAL-008 | Elevated launch path (inline rc) with a stand-in sudo: blocked until unlocked, then works | ✅ integration |
| TERMINAL-009 | The developer's real shell setup works (`make test-real`) | ✅ |
| TERMINAL-010 | Socket mode 0600, single instance, bad input, agents can't close the person's sessions, app-not-running message | ✅ unit |
| TERMINAL-011 | Idle elevated session is closed | ✅ unit |
| TERMINAL-012 | End to end: the real app plus `turttyl mcp` driven by an MCP client; commands appear typed live in the window; exit codes 0 and 1 are correct; no elevation reachable over MCP | ✅ manual |
| TERMINAL-015 | Password prompt during `run_command`: the person is asked, the agent is blocked from typing, the run doesn't return on idle, and it finishes after the person types (fake shell, and real zsh and bash with `read -s`) | ✅ unit + integration |
| TERMINAL-016 | Agents can't type or press keys into a password prompt (only `ctrl-c`); refused text is not logged | ✅ unit |
| TERMINAL-017 | Quiet `[y/N]` question: `run_command` returns in about 2 s with `looksLikePrompt`; the agent answers; `wait_for_command` returns the exit code (fake shell, and real zsh and bash) | ✅ unit + integration |
| TERMINAL-018 | `request_human_input`: banner and badge, other agents blocked, ends on hand-back, on command finish, or on timeout (and the request is cleared) | ✅ unit |
| TERMINAL-019 | Live app over MCP: prompt returns early; request banner and "needs you" badge shown; agent blocked while waiting; password banner shown and the agent's typed text refused; the person typed into the prompt and the agent's run finished | ✅ manual (screenshots) |
| TERMINAL-020 | Clicking **Hand back** / ⌘↵ in the app, and the Dock bounce | ⬜ not run: needs a person |
| TERMINAL-013 | Real sudo unlock in the app, agent runs `whoami` → `root`, close ends the root shell | ⬜ not run: needs a person to type the password |
| TERMINAL-014 | Linux build and run | ⬜ not run |
| TERMINAL-021 | Redesigned window (dark appearance): tabs in the title bar lined up with the traffic lights, ⌘T opens a tab, Liquid Glass backdrop, a live agent request shows the orange tab and the notice with Hand Back | ✅ manual (screenshots) |
| TERMINAL-022 | Light appearance, Reduce Transparency and Reduce Motion | ⬜ not run: needs the system setting changed |

`go test -race ./...`, `go vet` (darwin and linux), `staticcheck`, `govulncheck` and
`npm audit` are all clean.

## Bugs fixed
None (new product). Problems found and fixed during development:
- `wait_for_command` returned at once after the agent answered a prompt, because the
  old question was still the last line. Idle time now also counts from the start of
  the wait.
- Refused agent input was logged as if it had been typed. Refused input now has its
  own event, and its text is withheld when the refusal was at a password prompt.
- On bash, the prompt text could leak into `run_command` output. Fixed by adding the
  OSC 133 `C` marker.
- The history drawn when a tab opens wrapped at 80 columns. Fixed by sizing xterm to
  the shell before drawing.

## Notes for users / upgrade steps
- Build with `make build` and open `build/turttyl` (or `make app` on macOS).
  Add it to Claude Code with `claude mcp add turttyl -- <path>/turttyl mcp`.
- The app must be open for agents to connect.
- Development builds from before the rename kept settings in `~/.agentic-terminal/`.
  Move `config.json` to `~/.turttyl/` if you changed it.
- When a tab shows **needs you**, do what the notice says in that terminal. For a
  request, click **Hand back** (or press ⌘↵) when you're done. For a password, the
  agent continues by itself.
- If you type your password for an agent's `sudo` in a normal session, sudo keeps it
  for that terminal for its usual timeout (often 5 minutes). The agent can use sudo
  again in that session without asking during that time.
- Before turning on elevation, read the warning in the README: any process running as
  the same user can drive the app's sessions, including unlocked root sessions.

## Technical notes
- Config: `~/.turttyl/config.json` (`elevation.enabled`,
  `elevation.idle_timeout_minutes`, `typing_delay_ms`, `scrollback_kb`, `default_cwd`);
  env `TURTTYL_HOME`, `TURTTYL_AGENT_NAME` (see `.env.example`).
- New Go dependencies: `github.com/wailsapp/wails/v3` v3.1.0 (MIT),
  `github.com/creack/pty` v1.1.24 (MIT), `github.com/modelcontextprotocol/go-sdk` v1.8.0
  (MIT/Apache-2.0), `golang.org/x/sys` v0.50.0 (BSD-3). Transitive: jsonschema-go,
  segmentio/encoding and asm (MIT); uritemplate, x/oauth2, x/sync, x/term, x/time (BSD-3).
- npm (exact pins, all at least two weeks old): `@wailsio/runtime` 3.1.0,
  `@xterm/xterm` 6.0.0, `@xterm/addon-fit` 0.11.0, `react`/`react-dom` 19.3.1 (MIT);
  build only: `vite` 8.3.8, `@vitejs/plugin-react` 6.3.0, `typescript` 7.0.2,
  `@types/react*` 19.3.0.
- No `VERSION` file yet, so builds are labelled `0.1.0-alpha.dev`.
- New MCP tools: `wait_for_command`, `request_human_input`. New `run_command` option:
  `idle_seconds` (default 30, 0 = never return early). `SessionInfo` gains `running`,
  `passwordPrompt` and `attention`.
- Risks: see `documents/AI Onboarding.md` F1–F11.

## Suggestions
- Browser mode (CR D9): a WebSocket transport. The Wails v3 runtime already supports
  custom transports (`setTransport`).
- `read_screen` MCP tool for full-screen programs.
- Let an agent request an elevated session for a person to approve.
- Per-agent approval of socket connections (onboarding F1).
- Windows: ConPTY and UAC.
- Developer ID signing and notarisation for macOS.
- An optional system notification (Wails notifications service) when the app is in
  the background and an agent needs the person.
- Optionally run `sudo -k` after a hand-off password, so agents don't inherit the sudo
  ticket (onboarding F10).
