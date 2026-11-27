// Package ptyshell starts shells on pseudo-terminals (macOS and Linux) with shell
// integration that reports prompts and exit codes as OSC 133 markers.
package ptyshell

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/naseebnaushad/turttyl/internal/app"
)

//go:embed scripts/bashrc.sh
var bashRC string

//go:embed scripts/zshrc.zsh
var zshRC string

// Spawner starts shells. Create it with New.
type Spawner struct {
	shell   string // user's login shell
	zdotdir string // folder holding the zsh integration files
	bashrc  string // path of the bash integration rc file
	sudo    string
}

// New writes the integration scripts under dir (created 0700) and returns a Spawner.
func New(dir string) (*Spawner, error) {
	if err := os.MkdirAll(filepath.Join(dir, "zsh"), 0o700); err != nil {
		return nil, err
	}
	s := &Spawner{
		shell:   loginShell(),
		zdotdir: filepath.Join(dir, "zsh"),
		bashrc:  filepath.Join(dir, "bashrc"),
		sudo:    "/usr/bin/sudo",
	}
	files := map[string]string{
		s.bashrc:                              bashRC,
		filepath.Join(s.zdotdir, ".zshenv"):   zshStage(".zshenv", ""),
		filepath.Join(s.zdotdir, ".zprofile"): zshStage(".zprofile", ""),
		filepath.Join(s.zdotdir, ".zshrc"):    zshStage(".zshrc", zshRC),
		filepath.Join(s.zdotdir, ".zlogin"):   zshStage(".zlogin", zshLoginTail),
	}
	for p, content := range files {
		if err := writeAtomic(p, []byte(content), 0o600); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// zshStage builds one zsh startup file: it runs the user's file of the same name with
// the user's ZDOTDIR, then switches back to ours so zsh finds the next stage here.
func zshStage(name, extra string) string {
	return fmt.Sprintf(`# turttyl shell integration (generated; do not edit)
__at_dir="$ZDOTDIR"
ZDOTDIR="${AT_USER_ZDOTDIR:-$HOME}"
[ -r "$ZDOTDIR/%[1]s" ] && . "$ZDOTDIR/%[1]s"
AT_USER_ZDOTDIR="$ZDOTDIR"
ZDOTDIR="$__at_dir"
unset __at_dir
%[2]s
`, name, extra)
}

const zshLoginTail = `# Last stage: give the user's ZDOTDIR back to subshells.
ZDOTDIR="$AT_USER_ZDOTDIR"
unset AT_USER_ZDOTDIR
`

func loginShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		if base := filepath.Base(sh); base == "zsh" || base == "bash" {
			if _, err := os.Stat(sh); err == nil {
				return sh
			}
		}
	}
	for _, sh := range []string{"/bin/zsh", "/bin/bash", "/usr/bin/bash", "/usr/bin/zsh"} {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	return "/bin/sh"
}

func bashPath() string {
	for _, sh := range []string{"/bin/bash", "/usr/bin/bash"} {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	return "bash"
}

// command builds the shell command for a spec.
func (s *Spawner) command(spec app.ShellSpec) *exec.Cmd {
	var cmd *exec.Cmd
	env := baseEnv()
	switch {
	case spec.Elevated:
		// sudo -k: ask for the password now and leave no cached sudo ticket behind.
		// The rc file is passed inline so root never reads a file from a folder the
		// user can write to.
		bash := bashPath()
		cmd = exec.Command(s.sudo, "-k", "-H", "--", bash, "-c",
			`exec "$0" --rcfile <(printf '%s' "$1") -i`, bash, bashRC)
	case filepath.Base(s.shell) == "zsh":
		user := os.Getenv("ZDOTDIR")
		if user == "" {
			user, _ = os.UserHomeDir()
		}
		env = append(env, "ZDOTDIR="+s.zdotdir, "AT_USER_ZDOTDIR="+user)
		cmd = exec.Command(s.shell, "-l")
	case filepath.Base(s.shell) == "bash":
		cmd = exec.Command(s.shell, "--rcfile", s.bashrc, "-i")
	default:
		cmd = exec.Command(s.shell, "-l")
	}
	cmd.Env = env
	cmd.Dir = spec.Cwd
	return cmd
}

func baseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		switch {
		case hasPrefix(kv, "ZDOTDIR="), hasPrefix(kv, "AT_USER_ZDOTDIR="), hasPrefix(kv, "TERM="),
			hasPrefix(kv, "TERM_PROGRAM="), hasPrefix(kv, "COLORTERM="):
			continue
		}
		env = append(env, kv)
	}
	return append(env, "TERM=xterm-256color", "COLORTERM=truecolor",
		"TERM_PROGRAM=turttyl", "TURTTYL=1")
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }

// Spawn starts a shell on a new PTY.
func (s *Spawner) Spawn(spec app.ShellSpec) (app.Process, error) {
	cmd := s.command(spec)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(spec.Cols), Rows: uint16(spec.Rows)})
	if err != nil {
		return nil, fmt.Errorf("start shell: %w", err)
	}
	return &process{f: f, cmd: cmd, done: make(chan struct{})}, nil
}

type process struct {
	f         *os.File
	cmd       *exec.Cmd
	done      chan struct{}
	closeOnce sync.Once
}

func (p *process) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *process) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *process) Resize(cols, rows int) error {
	return pty.Setsize(p.f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (p *process) Wait() (int, error) {
	err := p.cmd.Wait()
	close(p.done)
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal()), nil
		}
		return ee.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

// Close hangs up the terminal. Closing the PTY sends SIGHUP to the shell, which also
// works for a root shell under sudo that this user cannot signal directly.
func (p *process) Close() error {
	p.closeOnce.Do(func() {
		_ = p.f.Close()
		if p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP) // EPERM for root: fine
			go func() {
				select {
				case <-p.done:
				case <-time.After(3 * time.Second):
					_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
				}
			}()
		}
	})
	return nil
}

// writeAtomic writes a file through a temp file and rename.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
