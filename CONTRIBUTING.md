# Contributing to turttyl

Thanks for helping. This page covers building, testing and sending a change.

## Build and run

You need:

| Tool | Version |
|---|---|
| Go | 1.27 or newer |
| Node.js and npm | a current LTS release |
| macOS | 13 or newer, with the Xcode Command Line Tools |
| Linux | GTK 3 and WebKitGTK 4.1 development packages (for example `gtk3` and `webkit2gtk-4.1` on Arch, `libgtk-3-dev` and `libwebkit2gtk-4.1-dev` on Debian/Ubuntu) |

The Linux build is not tested yet; reports and fixes are very welcome.

```bash
make build
```

```bash
./build/turttyl
```

`make help` lists every target. `make test` builds the UI first, because the Go tests
embed it.

## Before you send a pull request

```bash
make test
```

```bash
make lint
```

```bash
make vuln
```

- Add or update tests for what you change.
- Update `README.md` (and `.env.example` if you add an environment variable) when
  behaviour, setup or commands change.
- Write commit messages as `type(scope): summary`, for example
  `fix(ptyshell): restore ZDOTDIR after login`. Types: `feat`, `fix`, `docs`,
  `refactor`, `test`, `chore`, `security`. Say why in the body.
- Keep each pull request to one change.

To work on the UI alone, run `npm --prefix frontend run dev` and open
`http://localhost:5173/?mock` (or `?mock=omarchy`, `?mock=omarchy-light`). The page runs
on fake sessions, so no Go build is needed.

## How the code is laid out

Dependencies point inward: `internal/domain` (types and rules) ← `internal/app` (use
cases and ports) ← `internal/adapters/*` (PTY, Wails bridge, MCP, socket, audit,
config, Omarchy look) ← `cmd/turttyl` (wiring). The UI in `frontend/` reaches Go only
through `frontend/src/transport.ts`.

## Rules that keep turttyl safe

Please keep these when you change the code. Reviews check them.

- **Elevated sessions:** only a person (`domain.Human`) may create one, only when the
  config enables it, and agents are blocked until it is unlocked. All three checks live
  in `app.Manager`. The control socket must never gain a way to create one.
- **Passwords:** agents must never type into a password prompt, and a person's
  keystrokes are never logged (they can contain passwords).
- **Input from outside** (socket, MCP, config, theme files) is validated and size
  limited. Anything that reaches CSS or the terminal is strictly checked.
- **Dependencies:** only add one when it is really needed, with an MIT, BSD, ISC,
  Apache-2.0 or similar permissive licence. Pin npm packages to exact versions released
  at least two weeks ago.

## Reporting bugs and ideas

Open an issue with what you did, what you expected and what happened, plus your OS and
`turttyl version`. For security problems, follow [SECURITY.md](SECURITY.md) instead.

By contributing, you agree that your contribution is licensed under the
[MIT licence](LICENSE).
