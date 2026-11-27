package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/naseebnaushad/turttyl/internal/domain"
)

// fakeProc is an in-memory shell. Bytes written to it are recorded; output is pushed
// by the test with emit.
type fakeProc struct {
	pw      atomic.Bool // simulated password prompt
	mu      sync.Mutex
	written strings.Builder
	r       *io.PipeReader
	w       *io.PipeWriter
	exit    chan int
}

func newFakeProc() *fakeProc {
	r, w := io.Pipe()
	return &fakeProc{r: r, w: w, exit: make(chan int, 1)}
}

func (p *fakeProc) Read(b []byte) (int, error) { return p.r.Read(b) }
func (p *fakeProc) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.Write(b)
}
func (p *fakeProc) Resize(int, int) error { return nil }
func (p *fakeProc) PasswordPrompt() bool  { return p.pw.Load() }
func (p *fakeProc) Wait() (int, error)    { return <-p.exit, nil }
func (p *fakeProc) Close() error {
	_ = p.w.Close()
	select {
	case p.exit <- 129:
	default:
	}
	return nil
}
func (p *fakeProc) emit(s string) { _, _ = p.w.Write([]byte(s)) }
func (p *fakeProc) got() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.String()
}

type fakeSpawner struct {
	mu    sync.Mutex
	procs []*fakeProc
	specs []ShellSpec
}

func (f *fakeSpawner) Spawn(spec ShellSpec) (Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := newFakeProc()
	f.procs = append(f.procs, p)
	f.specs = append(f.specs, spec)
	return p, nil
}

type memAudit struct {
	mu sync.Mutex
	ev []AuditEvent
}

func (a *memAudit) Record(e AuditEvent) { a.mu.Lock(); a.ev = append(a.ev, e); a.mu.Unlock() }
func (a *memAudit) types() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var t []string
	for _, e := range a.ev {
		t = append(t, e.Type)
	}
	return t
}

var claude = domain.Actor{Kind: domain.Agent, Name: "claude"}

func newTestManager(t *testing.T, elevation bool) (*Manager, *fakeSpawner, *memAudit) {
	t.Helper()
	sp := &fakeSpawner{}
	au := &memAudit{}
	m := NewManager(Config{TypingDelayMs: 1, ElevationEnabled: elevation, DefaultCwd: t.TempDir()}, sp, au, nil)
	t.Cleanup(m.Shutdown)
	return m, sp, au
}

func waitState(t *testing.T, m *Manager, id string, want domain.State) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if info, _ := m.Info(id); info.State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	info, _ := m.Info(id)
	t.Fatalf("state = %s, want %s", info.State, want)
}

func TestElevationOffByDefault(t *testing.T) {
	m, _, _ := newTestManager(t, false)
	_, err := m.Create(domain.HumanActor, CreateRequest{Elevated: true})
	if !errors.Is(err, domain.ErrElevationDisabled) {
		t.Fatalf("err = %v", err)
	}
}

func TestAgentsCannotCreateElevated(t *testing.T) {
	m, _, _ := newTestManager(t, true)
	_, err := m.Create(claude, CreateRequest{Elevated: true})
	if !errors.Is(err, domain.ErrElevationHumanOnly) {
		t.Fatalf("err = %v", err)
	}
}

func TestElevatedBlocksAgentsUntilUnlocked(t *testing.T) {
	m, sp, au := newTestManager(t, true)
	info, err := m.Create(domain.HumanActor, CreateRequest{Elevated: true})
	if err != nil {
		t.Fatal(err)
	}
	if info.State != domain.Authenticating || !info.Elevated {
		t.Fatalf("info = %+v", info)
	}
	if !sp.specs[0].Elevated {
		t.Fatal("spawner was not asked for an elevated shell")
	}
	p := sp.procs[0]
	p.emit("Password:")
	ctx := context.Background()
	if err := m.Type(ctx, claude, info.ID, "hunter2", true, nil); !errors.Is(err, domain.ErrAuthenticating) {
		t.Fatalf("type err = %v", err)
	}
	if _, err := m.Read(info.ID, 0, 0); !errors.Is(err, domain.ErrAuthenticating) {
		t.Fatalf("read err = %v", err)
	}
	if err := m.SendKeys(claude, info.ID, []string{"enter"}); !errors.Is(err, domain.ErrAuthenticating) {
		t.Fatalf("keys err = %v", err)
	}
	// The person types the password; nothing about it reaches the audit log.
	if err := m.WriteHuman(info.ID, "s3cret\r"); err != nil {
		t.Fatal(err)
	}
	p.emit("\r\n\x1b]133;A\x07root# ")
	waitState(t, m, info.ID, domain.Ready)
	if err := m.Type(ctx, claude, info.ID, "whoami", true, nil); err != nil {
		t.Fatal(err)
	}
	for _, e := range au.ev {
		if strings.Contains(e.Detail, "s3cret") {
			t.Fatalf("password leaked into audit: %+v", e)
		}
	}
	types := strings.Join(au.types(), ",")
	if !strings.Contains(types, "elevated_started") || !strings.Contains(types, "elevated_unlocked") {
		t.Fatalf("audit types = %s", types)
	}
}

func TestTypeIsCharacterByCharacterAndHumanInterrupts(t *testing.T) {
	m, sp, _ := newTestManager(t, false)
	info, err := m.Create(claude, CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	p := sp.procs[0]
	delay := 20
	errc := make(chan error, 1)
	go func() {
		errc <- m.Type(context.Background(), claude, info.ID, strings.Repeat("x", 100), false, &delay)
	}()
	time.Sleep(120 * time.Millisecond)
	if typing := mustInfo(t, m, info.ID).Typing; typing != "claude" {
		t.Fatalf("typing = %q", typing)
	}
	if err := m.WriteHuman(info.ID, "\x03"); err != nil {
		t.Fatal(err)
	}
	if err := <-errc; !errors.Is(err, domain.ErrInterruptedByHuman) {
		t.Fatalf("err = %v", err)
	}
	got := p.got()
	n := strings.Count(got, "x")
	if n == 0 || n >= 100 || !strings.HasSuffix(got, "\x03") {
		t.Fatalf("written = %q", got)
	}
	if typing := mustInfo(t, m, info.ID).Typing; typing != "" {
		t.Fatalf("typing still set: %q", typing)
	}
}

func TestOnlyOneAgentTypesAtATime(t *testing.T) {
	m, _, _ := newTestManager(t, false)
	info, _ := m.Create(claude, CreateRequest{})
	delay := 10
	go func() { _ = m.Type(context.Background(), claude, info.ID, strings.Repeat("a", 30), false, &delay) }()
	time.Sleep(50 * time.Millisecond)
	other := domain.Actor{Kind: domain.Agent, Name: "codex"}
	if err := m.Type(context.Background(), other, info.ID, "b", false, &delay); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunWaitsForExitCode(t *testing.T) {
	m, sp, _ := newTestManager(t, false)
	info, _ := m.Create(claude, CreateRequest{})
	p := sp.procs[0]
	p.emit("\x1b]133;A\x07$ ")
	go func() {
		// Act as the shell: once the command line arrives, print output and markers.
		for !strings.HasSuffix(p.got(), "\r") {
			time.Sleep(2 * time.Millisecond)
		}
		p.emit("ls\r\nfile1\r\nfile2\r\n\x1b]133;D;3\x07\x1b]133;A\x07$ ")
	}()
	res, err := m.Run(context.Background(), claude, info.ID, "ls", RunOpts{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == nil || *res.ExitCode != 3 || res.Output != "file1\nfile2" || res.TimedOut {
		t.Fatalf("res = %+v (output %q)", res, res.Output)
	}
}

func TestRunTimesOut(t *testing.T) {
	m, sp, _ := newTestManager(t, false)
	info, _ := m.Create(claude, CreateRequest{})
	sp.procs[0].emit("\x1b]133;A\x07$ ")
	res, err := m.Run(context.Background(), claude, info.ID, "sleep 100", RunOpts{Timeout: 200 * time.Millisecond, Idle: -1})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || !res.Running || res.ExitCode != nil {
		t.Fatalf("res = %+v", res)
	}
}

func TestRunRejectsMultiline(t *testing.T) {
	m, _, _ := newTestManager(t, false)
	info, _ := m.Create(claude, CreateRequest{})
	_, err := m.Run(context.Background(), claude, info.ID, "a\nb", RunOpts{Timeout: time.Second})
	var ve *domain.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v", err)
	}
}

func TestAgentsCloseOnlyTheirOwnSessions(t *testing.T) {
	m, _, _ := newTestManager(t, false)
	mine, _ := m.Create(domain.HumanActor, CreateRequest{})
	if err := m.Close(claude, mine.ID); !errors.Is(err, domain.ErrNotOwner) {
		t.Fatalf("err = %v", err)
	}
	theirs, _ := m.Create(claude, CreateRequest{})
	if err := m.Close(claude, theirs.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Info(theirs.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidation(t *testing.T) {
	m, _, _ := newTestManager(t, false)
	if _, err := m.Create(claude, CreateRequest{Cwd: "/definitely/not/here"}); err == nil {
		t.Fatal("bad cwd accepted")
	}
	if _, err := m.Create(claude, CreateRequest{Name: strings.Repeat("n", 65)}); err == nil {
		t.Fatal("long name accepted")
	}
	info, err := m.Create(claude, CreateRequest{Name: "build\x1b[31m\u202e"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "build[31m" {
		t.Fatalf("name not cleaned: %q", info.Name)
	}
	if err := m.Type(context.Background(), claude, info.ID, strings.Repeat("a", domain.MaxTypeTextBytes+1), false, nil); err == nil {
		t.Fatal("long text accepted")
	}
	bad := 999
	if err := m.Type(context.Background(), claude, info.ID, "a", false, &bad); err == nil {
		t.Fatal("bad delay accepted")
	}
	if err := m.SendKeys(claude, info.ID, []string{"rm -rf"}); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := m.Resize(info.ID, 1, 1); err == nil {
		t.Fatal("bad size accepted")
	}
}

func TestIdleElevatedSessionIsClosed(t *testing.T) {
	sp := &fakeSpawner{}
	m := NewManager(Config{ElevationEnabled: true, ElevationIdle: 50 * time.Millisecond}, sp, nil, nil)
	defer m.Shutdown()
	info, err := m.Create(domain.HumanActor, CreateRequest{Elevated: true})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := m.Info(info.ID); errors.Is(err, domain.ErrNotFound) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("idle elevated session was not closed")
}

func mustInfo(t *testing.T, m *Manager, id string) domain.SessionInfo {
	t.Helper()
	info, err := m.Info(id)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
