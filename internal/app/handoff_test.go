package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/naseebnaushad/turttyl/internal/domain"
)

// readyShell creates a session whose fake shell has shown a prompt, and returns a
// helper that waits until the agent's command line has been typed.
func readyShell(t *testing.T) (*Manager, *fakeProc, string, func()) {
	t.Helper()
	m, sp, _ := newTestManager(t, false)
	info, err := m.Create(claude, CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	p := sp.procs[0]
	p.emit("\x1b]133;A\x07$ ")
	typed := func() {
		deadline := time.Now().Add(2 * time.Second)
		for !strings.HasSuffix(p.got(), "\r") {
			if time.Now().After(deadline) {
				t.Fatal("command never typed")
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	return m, p, info.ID, typed
}

func waitAttention(t *testing.T, m *Manager, id string, want bool) *domain.Attention {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		info, _ := m.Info(id)
		if (info.Attention != nil) == want {
			return info.Attention
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("attention present = %v, want %v", !want, want)
	return nil
}

func TestPasswordPromptAsksHumanAndAgentCarriesOn(t *testing.T) {
	m, p, id, typed := readyShell(t)
	resc := make(chan RunResult, 1)
	go func() {
		res, err := m.Run(context.Background(), claude, id, "sudo apt update", RunOpts{Timeout: 10 * time.Second, Idle: 300 * time.Millisecond})
		if err != nil {
			t.Error(err)
		}
		resc <- res
	}()
	typed()
	p.emit("sudo apt update\r\n\x1b]133;C\x07[sudo] password for dev: ")
	p.pw.Store(true)

	a := waitAttention(t, m, id, true)
	if a.Kind != domain.NeedsPassword || a.Agent != "claude" {
		t.Fatalf("attention = %+v", a)
	}
	// The agent can't type into the prompt, and Run hasn't returned on idle.
	if err := m.Type(context.Background(), claude, id, "guess", true, nil); !errors.Is(err, domain.ErrWaitingForHuman) {
		t.Fatalf("type err = %v", err)
	}
	time.Sleep(600 * time.Millisecond) // longer than Idle
	select {
	case res := <-resc:
		t.Fatalf("run returned while waiting for the person: %+v", res)
	default:
	}
	// The person types the password; the prompt goes away; the command finishes.
	if err := m.WriteHuman(id, "pw\r"); err != nil {
		t.Fatal(err)
	}
	p.pw.Store(false)
	waitAttention(t, m, id, false)
	p.emit("\r\nHit:1 archive\r\nDone\r\n\x1b]133;D;0\x07\x1b]133;A\x07$ ")
	res := <-resc
	if res.Running || res.ExitCode == nil || *res.ExitCode != 0 || !strings.Contains(res.Output, "Done") {
		t.Fatalf("res = %+v", res)
	}
}

func TestAgentCannotTypeIntoPasswordPrompt(t *testing.T) {
	m, p, id, _ := readyShell(t)
	p.pw.Store(true)
	if err := m.Type(context.Background(), claude, id, "secret", true, nil); !errors.Is(err, domain.ErrPasswordPrompt) {
		t.Fatalf("err = %v", err)
	}
	if err := m.SendKeys(claude, id, []string{"enter"}); !errors.Is(err, domain.ErrPasswordPrompt) {
		t.Fatalf("keys err = %v", err)
	}
	if err := m.SendKeys(claude, id, []string{"ctrl-c"}); err != nil {
		t.Fatalf("ctrl-c should be allowed: %v", err)
	}
	if strings.Contains(p.got(), "secret") {
		t.Fatal("password text reached the terminal")
	}
	for _, e := range m.auditLog.(*memAudit).ev {
		if strings.Contains(e.Detail, "secret") {
			t.Fatalf("refused password text logged: %+v", e)
		}
	}
}

func TestQuietPromptReturnsEarlyAndWaitContinues(t *testing.T) {
	m, p, id, typed := readyShell(t)
	resc := make(chan RunResult, 1)
	go func() {
		res, _ := m.Run(context.Background(), claude, id, "./install.sh", RunOpts{Timeout: 10 * time.Second})
		resc <- res
	}()
	typed()
	p.emit("./install.sh\r\n\x1b]133;C\x07Installing...\r\nOverwrite config? [y/N] ")
	start := time.Now()
	res := <-resc
	if !res.Running || !res.LooksLikePrompt || res.LastLine != "Overwrite config? [y/N]" || res.Next == "" {
		t.Fatalf("res = %+v", res)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("took %v; prompts should return after about 2s", time.Since(start))
	}
	// The agent answers, then waits for the end.
	if err := m.Type(context.Background(), claude, id, "y", true, nil); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(100 * time.Millisecond)
		p.emit("y\r\nDone.\r\n\x1b]133;D;0\x07\x1b]133;A\x07$ ")
	}()
	res, err := m.WaitCommand(context.Background(), claude, id, RunOpts{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Running || res.ExitCode == nil || !strings.Contains(res.Output, "Done.") || !strings.Contains(res.Output, "Installing...") {
		t.Fatalf("wait res = %+v", res)
	}
}

func TestRequestHumanHandBack(t *testing.T) {
	m, p, id, typed := readyShell(t)
	go func() {
		_, _ = m.Run(context.Background(), claude, id, "./wizard", RunOpts{Timeout: 10 * time.Second, Idle: -1})
	}()
	typed()
	p.emit("\x1b]133;C\x07Pick a region:\r\n> eu-west\r\n  us-east\r\n")

	type out struct {
		res HumanResult
		err error
	}
	done := make(chan out, 1)
	go func() {
		r, err := m.RequestHuman(context.Background(), claude, id, "Pick the region\x1b[2J in the menu", 5*time.Second)
		done <- out{r, err}
	}()
	a := waitAttention(t, m, id, true)
	if a.Kind != domain.NeedsInput || a.Reason != "Pick the region[2J in the menu" {
		t.Fatalf("attention = %+v", a)
	}
	// Other agents can't jump in while the person is acting.
	codex := domain.Actor{Kind: domain.Agent, Name: "codex"}
	if err := m.Type(context.Background(), codex, id, "x", false, nil); !errors.Is(err, domain.ErrWaitingForHuman) {
		t.Fatalf("type err = %v", err)
	}
	if _, err := m.RequestHuman(context.Background(), codex, id, "me too", time.Second); !errors.Is(err, domain.ErrWaitingForHuman) {
		t.Fatalf("second request err = %v", err)
	}
	_ = m.WriteHuman(id, "\x1b[B\r")
	p.emit("Region: us-east\r\n")
	if err := m.HandBack(id); err != nil {
		t.Fatal(err)
	}
	o := <-done
	if o.err != nil {
		t.Fatal(o.err)
	}
	if o.res.ResolvedBy != "human" || !strings.Contains(o.res.Output, "Region: us-east") || !o.res.Running {
		t.Fatalf("res = %+v", o.res)
	}
	waitAttention(t, m, id, false)
}

func TestRequestHumanEndsWhenCommandFinishes(t *testing.T) {
	m, p, id, typed := readyShell(t)
	go func() {
		_, _ = m.Run(context.Background(), claude, id, "make deploy", RunOpts{Timeout: 10 * time.Second, Idle: -1})
	}()
	typed()
	p.emit("\x1b]133;C\x07Approve? ")
	done := make(chan HumanResult, 1)
	go func() {
		r, _ := m.RequestHuman(context.Background(), claude, id, "Approve the deploy", 5*time.Second)
		done <- r
	}()
	waitAttention(t, m, id, true)
	p.emit("yes\r\ndeployed\r\n\x1b]133;D;0\x07\x1b]133;A\x07$ ")
	r := <-done
	if r.ResolvedBy != "command_finished" || r.ExitCode == nil || *r.ExitCode != 0 || r.Running {
		t.Fatalf("res = %+v", r)
	}
}

func TestRequestHumanTimeoutClearsRequest(t *testing.T) {
	m, _, id, _ := readyShell(t)
	r, err := m.RequestHuman(context.Background(), claude, id, "anyone?", 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if r.ResolvedBy != "timeout" {
		t.Fatalf("res = %+v", r)
	}
	waitAttention(t, m, id, false)
}

func TestWaitCommandWithoutCommand(t *testing.T) {
	m, _, id, _ := readyShell(t)
	if _, err := m.WaitCommand(context.Background(), claude, id, RunOpts{}); !errors.Is(err, domain.ErrNoCommand) {
		t.Fatalf("err = %v", err)
	}
}

func TestPromptHeuristic(t *testing.T) {
	yes := []string{"Continue? [Y/n]", "Proceed (y/n)", "Are you sure you want to continue connecting (yes/no/[fingerprint])?",
		"Press any key to continue", "Enter passphrase for key '/home/u/.ssh/id_ed25519':", "Username for 'https://github.com':", "Name:"}
	no := []string{"Compiling crate 12/90", "Downloading 45%", "done"}
	for _, l := range yes {
		if !promptRE.MatchString(l) {
			t.Errorf("should look like a prompt: %q", l)
		}
	}
	for _, l := range no {
		if promptRE.MatchString(l) {
			t.Errorf("should not look like a prompt: %q", l)
		}
	}
}
