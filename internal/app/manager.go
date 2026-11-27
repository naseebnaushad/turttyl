package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/naseebnaushad/turttyl/internal/domain"
)

// Manager owns all sessions. It is the single entry point for the UI and for agents.
type Manager struct {
	cfg      Config
	spawner  Spawner
	auditLog AuditLog
	listener Listener

	mu       sync.Mutex
	sessions map[string]*session
	order    []string
	stop     chan struct{}
	stopOnce sync.Once
}

// NewManager builds a Manager. listener may be nil and set later with SetListener.
func NewManager(cfg Config, sp Spawner, audit AuditLog, l Listener) *Manager {
	if cfg.ScrollbackBytes <= 0 {
		cfg.ScrollbackBytes = 2 << 20
	}
	if cfg.ElevationIdle <= 0 {
		cfg.ElevationIdle = 30 * time.Minute
	}
	m := &Manager{cfg: cfg, spawner: sp, auditLog: audit, listener: l,
		sessions: map[string]*session{}, stop: make(chan struct{})}
	go m.idleReaper()
	go m.passwordWatcher()
	return m
}

// SetListener sets the UI listener. Call it before any session starts.
func (m *Manager) SetListener(l Listener) { m.listener = l }

// ElevationEnabled reports whether elevated sessions are allowed.
func (m *Manager) ElevationEnabled() bool { return m.cfg.ElevationEnabled }

func (m *Manager) audit(e AuditEvent) {
	if m.auditLog != nil {
		if e.Time.IsZero() {
			e.Time = time.Now().UTC()
		}
		m.auditLog.Record(e)
	}
}

func (m *Manager) changed() {
	if m.listener != nil {
		m.listener.SessionsChanged()
	}
}

// CreateRequest describes a new session.
type CreateRequest struct {
	Name     string
	Cwd      string
	Elevated bool
	Cols     int
	Rows     int
}

// Create starts a new session. Only a person may create an elevated one, and only
// when elevation is turned on.
func (m *Manager) Create(actor domain.Actor, req CreateRequest) (domain.SessionInfo, error) {
	if req.Elevated {
		if !m.cfg.ElevationEnabled {
			return domain.SessionInfo{}, domain.ErrElevationDisabled
		}
		if actor.Kind != domain.Human {
			return domain.SessionInfo{}, domain.ErrElevationHumanOnly
		}
	}
	name, err := domain.CleanLabel(req.Name, domain.MaxNameLen, "")
	if err != nil {
		return domain.SessionInfo{}, err
	}
	cwd, err := m.resolveCwd(req.Cwd)
	if err != nil {
		return domain.SessionInfo{}, err
	}
	if req.Cols == 0 && req.Rows == 0 {
		req.Cols, req.Rows = 120, 32
	}
	if err := domain.ValidateSize(req.Cols, req.Rows); err != nil {
		return domain.SessionInfo{}, err
	}
	id := newID()
	if name == "" {
		name = "session " + id[:4]
		if req.Elevated {
			name = "root " + id[:4]
		}
	}
	proc, err := m.spawner.Spawn(ShellSpec{Cwd: cwd, Elevated: req.Elevated, Cols: req.Cols, Rows: req.Rows})
	if err != nil {
		return domain.SessionInfo{}, err
	}
	state := domain.Starting
	if req.Elevated {
		state = domain.Authenticating
	}
	info := domain.SessionInfo{ID: id, Name: name, Cwd: cwd, Elevated: req.Elevated, State: state, CreatedBy: actor}
	s := newSession(info, proc, m)
	m.mu.Lock()
	m.sessions[id] = s
	m.order = append(m.order, id)
	m.mu.Unlock()
	typ := "session_created"
	if req.Elevated {
		typ = "elevated_started"
	}
	m.audit(AuditEvent{Type: typ, Session: id, Actor: actor, Detail: cwd})
	go s.readLoop()
	m.changed()
	return s.snapshotInfo(), nil
}

func (m *Manager) resolveCwd(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		cwd = m.cfg.DefaultCwd
	}
	if cwd == "" {
		cwd, _ = os.UserHomeDir()
	}
	if len(cwd) > 4096 || strings.ContainsRune(cwd, 0) {
		return "", &domain.ValidationError{Msg: "cwd is not a valid path"}
	}
	st, err := os.Stat(cwd)
	if err != nil || !st.IsDir() {
		return "", &domain.ValidationError{Msg: "cwd must be an existing directory"}
	}
	return cwd, nil
}

func (m *Manager) get(id string) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return s, nil
}

// List returns all sessions in creation order.
func (m *Manager) List() []domain.SessionInfo {
	m.mu.Lock()
	ss := make([]*session, 0, len(m.order))
	for _, id := range m.order {
		ss = append(ss, m.sessions[id])
	}
	m.mu.Unlock()
	out := make([]domain.SessionInfo, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.snapshotInfo())
	}
	return out
}

// Info returns one session's description.
func (m *Manager) Info(id string) (domain.SessionInfo, error) {
	s, err := m.get(id)
	if err != nil {
		return domain.SessionInfo{}, err
	}
	return s.snapshotInfo(), nil
}

// WriteHuman sends a person's keystrokes (from the app window) to a session.
func (m *Manager) WriteHuman(id, data string) error {
	if len(data) > domain.MaxWriteBytes {
		return &domain.ValidationError{Msg: "input is too large"}
	}
	s, err := m.get(id)
	if err != nil {
		return err
	}
	return s.writeHuman(data)
}

// Resize changes a session's terminal size.
func (m *Manager) Resize(id string, cols, rows int) error {
	if err := domain.ValidateSize(cols, rows); err != nil {
		return err
	}
	s, err := m.get(id)
	if err != nil {
		return err
	}
	return s.proc.Resize(cols, rows)
}

// Snapshot returns the raw kept output of a session and the offset it starts at,
// so the UI can draw history and then apply live output after it.
func (m *Manager) Snapshot(id string) (data []byte, from int64, err error) {
	s, err := m.get(id)
	if err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, from, _ = s.out.since(0, 0)
	return data, from, nil
}

func (m *Manager) delay(ms *int) (time.Duration, error) {
	d := m.cfg.TypingDelayMs
	if ms != nil {
		if err := domain.ValidateDelay(*ms); err != nil {
			return 0, err
		}
		d = *ms
	}
	return time.Duration(d) * time.Millisecond, nil
}

// Type types text into a session as an agent, one character at a time, and
// optionally presses Enter.
func (m *Manager) Type(ctx context.Context, actor domain.Actor, id, text string, submit bool, delayMs *int) error {
	if err := domain.ValidateText(text, domain.MaxTypeTextBytes); err != nil {
		return err
	}
	if text == "" && !submit {
		return &domain.ValidationError{Msg: "nothing to type"}
	}
	d, err := m.delay(delayMs)
	if err != nil {
		return err
	}
	s, err := m.get(id)
	if err != nil {
		return err
	}
	if submit {
		text += "\r"
	}
	err = s.typeAgent(ctx, actor, text, d)
	m.auditInput(actor, id, text, err)
	return err
}

// auditInput records agent input after the attempt, so refused input is not logged as
// if it had been typed. Text refused at a password prompt is withheld from the log.
func (m *Manager) auditInput(actor domain.Actor, id, text string, err error) {
	e := AuditEvent{Type: "agent_type", Session: id, Actor: actor, Detail: text}
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrPasswordPrompt), errors.Is(err, domain.ErrWaitingForHuman), errors.Is(err, domain.ErrAuthenticating):
		e.Type, e.Detail = "agent_type_refused", "text withheld: "+err.Error()
	case errors.Is(err, domain.ErrInterruptedByHuman):
		e.Type = "agent_type_interrupted"
	default:
		e.Type, e.Detail = "agent_type_failed", text+" ("+err.Error()+")"
	}
	m.audit(e)
}

// SendKeys sends special keys (Ctrl-C, arrows…) as an agent.
func (m *Manager) SendKeys(actor domain.Actor, id string, keys []string) error {
	b, err := domain.KeyBytes(keys)
	if err != nil {
		return err
	}
	s, err := m.get(id)
	if err != nil {
		return err
	}
	if err := s.checkAgent(); err != nil {
		return err
	}
	// While the person is being asked, or a password is being read, agents may only
	// interrupt (ctrl-c), never answer.
	if !(len(keys) == 1 && strings.EqualFold(strings.TrimSpace(keys[0]), "ctrl-c")) {
		if err := s.checkAgentInput(); err != nil {
			return err
		}
	}
	m.audit(AuditEvent{Type: "agent_keys", Session: id, Actor: actor, Detail: strings.Join(keys, " ")})
	return s.write(b)
}

// ReadResult is output returned to an agent.
type ReadResult struct {
	Output    string       `json:"output"`
	Cursor    int64        `json:"cursor"`
	Truncated bool         `json:"truncated"`
	State     domain.State `json:"state"`
}

// Read returns plain-text output since a cursor. Pass cursor -1 for the most recent
// output (up to maxBytes).
func (m *Manager) Read(id string, cursor int64, maxBytes int) (ReadResult, error) {
	if maxBytes <= 0 || maxBytes > domain.MaxReadBytes {
		maxBytes = 64 * 1024
	}
	s, err := m.get(id)
	if err != nil {
		return ReadResult{}, err
	}
	if err := s.checkAgent(); err != nil && !errors.Is(err, domain.ErrExited) {
		return ReadResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if cursor < 0 {
		cursor = max64(0, s.out.end-int64(maxBytes))
	}
	data, from, trunc := s.out.since(cursor, maxBytes)
	return ReadResult{Output: plainText(data), Cursor: from + int64(len(data)), Truncated: trunc, State: s.info.State}, nil
}

// RunOpts tunes run_command and wait_for_command.
type RunOpts struct {
	// Timeout is the longest to wait (default 2 min, max 30 min). On timeout the
	// command keeps running.
	Timeout time.Duration
	// Idle returns early after this long with no output (default 30 s; sooner when
	// the last line looks like a question). Negative turns it off.
	Idle time.Duration
	// DelayMs overrides the typing delay.
	DelayMs *int
}

func (o RunOpts) timeout() time.Duration {
	if o.Timeout <= 0 || o.Timeout > 30*time.Minute {
		return 2 * time.Minute
	}
	return o.Timeout
}

func (o RunOpts) idle() time.Duration {
	if o.Idle == 0 {
		return DefaultIdle
	}
	return o.Idle
}

// Run types a command as an agent, presses Enter and waits for the shell to report
// that it finished. If the command needs a password, the person is asked to type it
// and the wait continues. If it goes quiet (for example at a y/n question), the
// output so far is returned with Running set so the agent can decide what to do.
func (m *Manager) Run(ctx context.Context, actor domain.Actor, id, command string, opts RunOpts) (RunResult, error) {
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return RunResult{}, &domain.ValidationError{Msg: "command is empty"}
	}
	if strings.ContainsAny(command, "\r\n") {
		return RunResult{}, &domain.ValidationError{Msg: "run one command line at a time (no newlines); join with && or ;"}
	}
	if err := domain.ValidateText(command, domain.MaxCommandBytes); err != nil {
		return RunResult{}, err
	}
	d, err := m.delay(opts.DelayMs)
	if err != nil {
		return RunResult{}, err
	}
	s, err := m.get(id)
	if err != nil {
		return RunResult{}, err
	}
	if err := s.checkAgentInput(); err != nil {
		return RunResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()

	// The shell must have shown a prompt, or we can't tell when the command ends.
	if err := s.waitFor(ctx, func() bool { return s.prompts > 0 }); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return RunResult{}, domain.ErrNoShellIntegration
		}
		return RunResult{}, err
	}
	s.mu.Lock()
	start := s.out.end
	s.mu.Unlock()

	m.audit(AuditEvent{Type: "agent_run", Session: id, Actor: actor, Detail: command})
	if err := s.typeAgent(ctx, actor, command+"\r", d); err != nil {
		return RunResult{}, err
	}
	return s.waitCommand(ctx, actor, start, opts.idle())
}

// dropEcho removes the echoed command line from the start of command output.
func dropEcho(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimRight(s[i+1:], "\n")
	}
	return ""
}

// Close ends a session. Agents may only close sessions they created.
func (m *Manager) Close(actor domain.Actor, id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	if actor.Kind == domain.Agent && s.info.CreatedBy != actor {
		return domain.ErrNotOwner
	}
	m.mu.Lock()
	delete(m.sessions, id)
	for i, x := range m.order {
		if x == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	s.close()
	m.audit(AuditEvent{Type: "session_closed", Session: id, Actor: actor})
	m.changed()
	return nil
}

// Shutdown closes every session. Elevated shells end with the app.
func (m *Manager) Shutdown() {
	m.stopOnce.Do(func() { close(m.stop) })
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(domain.Actor{Kind: domain.Human, Name: "app shutdown"}, id)
	}
}

// idleReaper closes elevated sessions that have had no input or output for too long.
func (m *Manager) idleReaper() {
	tick := m.cfg.ElevationIdle / 10
	if tick > 30*time.Second {
		tick = 30 * time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		m.mu.Lock()
		var idle []string
		for id, s := range m.sessions {
			s.mu.Lock()
			if s.info.Elevated && s.info.State != domain.Exited && time.Since(s.lastUsed) > m.cfg.ElevationIdle {
				idle = append(idle, id)
			}
			s.mu.Unlock()
		}
		m.mu.Unlock()
		for _, id := range idle {
			m.audit(AuditEvent{Type: "elevated_idle_timeout", Session: id, Actor: domain.Actor{Kind: domain.Human, Name: "idle timeout"}})
			_ = m.Close(domain.Actor{Kind: domain.Human, Name: "idle timeout"}, id)
		}
	}
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
