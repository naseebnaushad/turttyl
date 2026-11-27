# Turttyl: notes for Claude

A Wails v3 desktop app (Go + React/xterm.js) where people and AI agents share terminal
sessions. Agents connect through `turttyl mcp` (stdio MCP), which talks to the
running app over `~/.turttyl/control.sock`. See README.md for features.

## Layout (clean architecture: dependencies point inward)
- `internal/domain`: types, errors, validation and limits. Standard library only.
- `internal/app`: `Manager` use cases, ports (`Spawner`, `Process`, `AuditLog`,
  `Listener`), output ring buffer, OSC 133 scanner, plain-text conversion.
- `internal/adapters/ptyshell`: PTY shells and the zsh/bash integration scripts
  (`scripts/`). They emit OSC 133 A (prompt), C (output start) and D;code (finished).
- `internal/adapters/{desktop,control,mcpserver,audit,config}`: the Wails bridge,
  Unix socket API, MCP tools, JSONL audit log and config.json.
- `internal/adapters/omarchy`: reads the Omarchy theme, font and text size (Linux)
  for the UI. Values reach CSS, so keep the strict validation.
- `cmd/turttyl`: wiring only. `frontend/`: the UI, built to `frontend/dist`
  and embedded.

## Rules specific to this repo
- Elevated sessions: only `domain.Human` may create them, only when config enables
  it, and agents are blocked while the state is `authenticating`. Keep all three
  checks in `app.Manager`, not in adapters. The control socket must never gain a way
  to create one.
- Never log a person's keystrokes (they can contain passwords); agent input is logged,
  except text refused at a password prompt.
- Agents must never type into a password prompt (`Process.PasswordPrompt()`: echo off
  and ICANON on). The hand-off logic (attention, `waitCommand`, `RequestHuman`,
  `passwordWatcher`) is in `internal/app/handoff.go`.
- The UI reaches Go only through `frontend/src/transport.ts` (browser mode will add a
  second transport). JS calls Go methods by name:
  `github.com/naseebnaushad/turttyl/internal/adapters/desktop.TerminalService.<Method>`. Renaming the
  package, type or methods breaks the UI.
- Pin npm packages to exact versions released at least two weeks ago.

## Commands
`make build`, `make test` (it builds the UI first: `go test ./...` needs
`frontend/dist`), `make lint`, `make vuln`, `make test-real`.

## Release owner
Not named yet; until then, the team lead.
