# AI Onboarding

| | |
|---|---|
| **Date** | 2026-11-27 |
| **Audited** | Greenfield: the first build of v0.1.0 on `feature/turttyl` (no commits yet) |
| **Kind** | Baseline. The repo started empty, so there was no earlier code to audit or clean up. |

## Findings

These are known gaps in the first build. Nothing here is a vulnerability in the code;
the accepted risks are listed so they stay visible.

| # | Severity | Area | Finding | Proposed fix |
|---|---|---|---|---|
| F1 | Medium | Security (accepted risk) | Any process running as the same user can use `control.sock`, and therefore drive root sessions once a person has unlocked one. This is how the feature works, which is why it is off by default. | Documented in the README. Later: a per-agent approval prompt in the app, or a token handed out only to MCP processes the person approved. |
| F2 | Medium | Platform | The Linux build (Wails GTK/WebKit, `peer_linux.go`) has only been type-checked (`GOOS=linux go vet`), not built or run. | Build and test on Ubuntu before Beta. |
| F3 | Medium | Tests | Elevated sessions are tested with a stand-in for sudo. A real sudo unlock needs a person and has not been run yet. | Manual Alpha test case; see the change note. |
| F4 | Medium | Tests | `desktop`, `mcpserver` and `audit` have no unit tests. MCP was tested end to end by hand against the running app. The React UI has no tests. | Add an MCP stdio test against an in-process control server, and a bridge batching test. |
| F5 | Low | Security | `audit.jsonl` (0600) records agent input in full, which can include secrets an agent types on the command line. | Documented. Later: optional redaction patterns. |
| F6 | Low | Shell integration | The bash integration installs a DEBUG trap, replacing any DEBUG trap the user's rc set. | Chain an existing trap if one is found. |
| F7 | Low | Agents | `read_output` is plain text, so it is weak for full-screen programs (vim, top). | `read_screen` tool backed by a VT emulator (suggested in the CR). |
| F8 | Low | Packaging | The macOS `.app` is unsigned and not notarised, so Gatekeeper warns on first open. | Developer ID signing and notarisation before Beta. |
| F10 | Medium | Security | When the person types their password for an agent's `sudo` in a normal session, sudo's ticket (usually 5 minutes) then lets the agent run more `sudo` commands in that session without asking. This is standard sudo behaviour and is documented. | Option to run `sudo -k` in the session after a password hand-off, or to warn in the banner. |
| F11 | Low | Hand-off | Password prompts are detected from the terminal mode. Full-screen password dialogs (for example `pinentry-curses`) switch line mode off, so they look like a full-screen program and are only caught by the idle/prompt check. | Accept for now; add text heuristics for known pinentry screens if needed. |
| F9 | Low | Code | `control.Listen` sets the process umask for a moment while creating the socket. It is safe because this happens at startup before any shell starts, but it is process-wide. | Leave as is; revisit if the socket is ever recreated at runtime. |

## Architecture verdict

Clean architecture is followed: `domain` uses only the standard library; `app` depends
on `domain` and its own ports; adapters depend inward; `cmd/turttyl` only
wires things together. The UI renders state and forwards intent through one transport
module. The elevation rules live in `app.Manager`.

## Security and quality checks (2026-11-27)

| Check | Result |
|---|---|
| `govulncheck ./...` | No vulnerabilities found |
| `npm audit` | 0 vulnerabilities |
| Licences (Go modules in the binary, npm runtime) | MIT, BSD-3-Clause, ISC, MIT/Apache-2.0: all allowed |
| `go vet` (darwin and linux), `staticcheck`, `gofmt` | Clean |
| `go test -race ./...` | Pass |
| Secrets in code or history | None (no history yet) |

## Decision log

| Date | Decision | By |
|---|---|---|
| 2026-11-27 | Human hand-off added; F10 and F11 recorded. | Naseeb |
| 2026-11-27 | Greenfield baseline recorded; no clean-up refactor needed. Findings F1–F9 tracked here. | Naseeb (CR-0.1.0-01) |
