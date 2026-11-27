package ptyshell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/naseebnaushad/turttyl/internal/app"
	"github.com/naseebnaushad/turttyl/internal/domain"
)

var agent = domain.Actor{Kind: domain.Agent, Name: "test-agent"}

// hermetic gives the shell an empty HOME so the developer's own rc files don't matter.
func hermetic(t *testing.T, shell string) string {
	t.Helper()
	if _, err := os.Stat(shell); err != nil {
		t.Skipf("%s not installed", shell)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", shell)
	t.Setenv("ZDOTDIR", "")
	return home
}

func newManager(t *testing.T, sp *Spawner, elevation bool) *app.Manager {
	t.Helper()
	m := app.NewManager(app.Config{TypingDelayMs: 0, ElevationEnabled: elevation, DefaultCwd: t.TempDir()}, sp, nil, nil)
	t.Cleanup(m.Shutdown)
	return m
}

func runChecks(t *testing.T, m *app.Manager, actor domain.Actor, id string) {
	t.Helper()
	ctx := context.Background()
	res, err := m.Run(ctx, actor, id, "echo hello-$((40+2)) && false", app.RunOpts{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == nil || *res.ExitCode != 1 || !strings.Contains(res.Output, "hello-42") {
		t.Fatalf("run 1: %+v output=%q", res, res.Output)
	}
	if strings.Contains(res.Output, "echo hello") {
		t.Fatalf("command echo not dropped: %q", res.Output)
	}
	res, err = m.Run(ctx, actor, id, "cd / && pwd", app.RunOpts{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == nil || *res.ExitCode != 0 || strings.TrimSpace(res.Output) != "/" {
		t.Fatalf("run 2: %+v output=%q", res, res.Output)
	}
	// Typed input (not run) shows up in the output too.
	if err := m.Type(ctx, actor, id, "echo typed-by-agent", true, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := m.Read(id, -1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(r.Output, "typed-by-agent") >= 2 { // echo + output
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := m.Read(id, -1, 0)
	t.Fatalf("typed output not seen: %q", r.Output)
}

func TestZsh(t *testing.T) {
	hermetic(t, "/bin/zsh")
	sp, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(t, sp, false)
	info, err := m.Create(agent, app.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	runChecks(t, m, agent, info.ID)
}

func TestZshKeepsUserRC(t *testing.T) {
	home := hermetic(t, "/bin/zsh")
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("export AT_FROM_USER_RC=yes\nprecmd() { true }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sp, _ := New(t.TempDir())
	m := newManager(t, sp, false)
	info, _ := m.Create(agent, app.CreateRequest{})
	res, err := m.Run(context.Background(), agent, info.ID, `echo "rc=$AT_FROM_USER_RC zdot=$ZDOTDIR"`, app.RunOpts{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "rc=yes zdot="+home) {
		t.Fatalf("user rc not loaded or ZDOTDIR not restored: %q", res.Output)
	}
}

func TestBash(t *testing.T) {
	hermetic(t, "/bin/bash")
	sp, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(t, sp, false)
	info, err := m.Create(agent, app.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	runChecks(t, m, agent, info.ID)
}

// TestElevatedLaunchWithoutSudo checks the inline rc-file launch used for root shells,
// with a stand-in for sudo that skips authentication and runs the rest as this user.
func TestElevatedLaunchWithoutSudo(t *testing.T) {
	hermetic(t, "/bin/bash")
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-sudo")
	script := "#!/bin/sh\n[ \"$1 $2 $3\" = \"-k -H --\" ] || { echo bad-args; exit 2; }\nshift 3\nprintf 'Password:'\nread -r pw\nexec \"$@\"\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	sp, _ := New(t.TempDir())
	sp.sudo = fake
	m := newManager(t, sp, true)
	info, err := m.Create(domain.HumanActor, app.CreateRequest{Elevated: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if i, _ := m.Info(info.ID); i.State != domain.Authenticating {
		t.Fatalf("state = %s", i.State)
	}
	if err := m.Type(context.Background(), agent, info.ID, "x", true, nil); err != domain.ErrAuthenticating {
		t.Fatalf("agent typed during authentication: %v", err)
	}
	if err := m.WriteHuman(info.ID, "pw\r"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if i, _ := m.Info(info.ID); i.State == domain.Ready {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	runChecks(t, m, agent, info.ID)
}

// TestRealShell runs against the developer's own shell and rc files. Opt in with
// AT_REAL_SHELL_TEST=1; it is not hermetic.
func TestRealShell(t *testing.T) {
	if os.Getenv("AT_REAL_SHELL_TEST") != "1" {
		t.Skip("set AT_REAL_SHELL_TEST=1 to run against your own shell setup")
	}
	sp, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(t, sp, false)
	info, err := m.Create(agent, app.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	runChecks(t, m, agent, info.ID)
}

// handoffChecks runs the hand-off flows on a real shell: a password prompt the person
// answers, and a y/N question the agent answers itself.
func handoffChecks(t *testing.T, shell string) {
	hermetic(t, shell)
	// zsh and bash spell "read with a prompt" differently.
	readPw, readAns := `read -rs -p "Password: " pw`, `read -r -p "Overwrite? [y/N] " a`
	if filepath.Base(shell) == "zsh" {
		readPw, readAns = `read -rs "pw?Password: "`, `read -r "a?Overwrite? [y/N] "`
	}
	sp, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m := newManager(t, sp, false)
	info, err := m.Create(agent, app.CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// 1. Password prompt: the person is asked; the agent's run continues afterwards.
	resc := make(chan app.RunResult, 1)
	go func() {
		res, err := m.Run(ctx, agent, info.ID, readPw+`; echo; echo "len=${#pw}"`, app.RunOpts{Timeout: 20 * time.Second, Idle: 500 * time.Millisecond})
		if err != nil {
			t.Error(err)
		}
		resc <- res
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		i, _ := m.Info(info.ID)
		if i.Attention != nil && i.Attention.Kind == domain.NeedsPassword && i.PasswordPrompt {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("password prompt not detected: %+v", i)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := m.Type(ctx, agent, info.ID, "nope", true, nil); err == nil {
		t.Fatal("agent typed into a password prompt")
	}
	time.Sleep(time.Second) // past Idle: run must still be waiting
	if err := m.WriteHuman(info.ID, "hunter22\r"); err != nil {
		t.Fatal(err)
	}
	res := <-resc
	if res.Running || res.ExitCode == nil || *res.ExitCode != 0 || !strings.Contains(res.Output, "len=8") {
		t.Fatalf("password run: %+v", res)
	}
	if strings.Contains(res.Output, "hunter22") {
		t.Fatal("password echoed into output")
	}

	// 2. A question: run returns early; the agent answers and waits for the end.
	res, err = m.Run(ctx, agent, info.ID, readAns+`; echo "answer=$a"`, app.RunOpts{Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Running || !res.LooksLikePrompt || !strings.Contains(res.LastLine, "Overwrite? [y/N]") {
		t.Fatalf("prompt run: %+v", res)
	}
	if err := m.Type(ctx, agent, info.ID, "y", true, nil); err != nil {
		t.Fatal(err)
	}
	res, err = m.WaitCommand(ctx, agent, info.ID, app.RunOpts{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Running || res.ExitCode == nil || !strings.Contains(res.Output, "answer=y") {
		t.Fatalf("wait: %+v", res)
	}
}

func TestHandoffZsh(t *testing.T)  { handoffChecks(t, "/bin/zsh") }
func TestHandoffBash(t *testing.T) { handoffChecks(t, "/bin/bash") }
