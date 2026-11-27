package app

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/naseebnaushad/turttyl/internal/domain"
)

// session is one running shell with its output history and typing state.
type session struct {
	info domain.SessionInfo
	proc Process
	mgr  *Manager

	mu             sync.Mutex
	out            *ring
	scan           oscScanner
	prompts        int      // number of 'A' markers seen
	marks          []marker // recent 'C' and 'D' markers
	notify         chan struct{}
	lastUsed       time.Time
	lastOutput     time.Time
	lastHumanAudit time.Time
	waiters        int    // agents blocked waiting on this session's command or person
	waiter         string // name of the latest of them
	resolvedBy     string // how the last attention request ended
	lastAttn       time.Time
	pwLine         string // last line on screen when a password prompt appeared

	writeMu   sync.Mutex    // serialises writes to the PTY
	typingMu  sync.Mutex    // one agent typing at a time
	interrupt chan struct{} // closed when a person types during agent input
	closed    chan struct{}
	closeOnce sync.Once
}

func newSession(info domain.SessionInfo, proc Process, m *Manager) *session {
	return &session{
		info: info, proc: proc, mgr: m,
		out:      newRing(m.cfg.ScrollbackBytes),
		notify:   make(chan struct{}),
		lastUsed: time.Now(),
		closed:   make(chan struct{}),
	}
}

// readLoop copies shell output into the history and tells the listener.
func (s *session) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.proc.Read(buf)
		if n > 0 {
			s.ingest(buf[:n])
		}
		if err != nil {
			break
		}
	}
	code, _ := s.proc.Wait()
	s.mu.Lock()
	s.info.State = domain.Exited
	s.info.ExitCode = &code
	s.info.Typing = ""
	s.info.Running = false
	s.info.PasswordPrompt = false
	s.clearAttention("session_ended")
	s.wake()
	s.mu.Unlock()
	s.mgr.audit(AuditEvent{Type: "session_exited", Session: s.info.ID, Actor: s.info.CreatedBy, Detail: strconv.Itoa(code)})
	s.mgr.changed()
}

func (s *session) ingest(p []byte) {
	s.mu.Lock()
	off := s.out.end
	s.out.write(p)
	s.lastUsed = time.Now()
	s.lastOutput = s.lastUsed
	stateChanged := false
	for _, m := range s.scan.feed(off, p) {
		switch m.kind {
		case 'A':
			s.prompts++
			if s.info.State == domain.Starting || s.info.State == domain.Authenticating {
				if s.info.State == domain.Authenticating {
					s.mgr.audit(AuditEvent{Type: "elevated_unlocked", Session: s.info.ID, Actor: domain.HumanActor})
				}
				s.info.State = domain.Ready
				stateChanged = true
			}
		case 'C', 'D':
			s.marks = append(s.marks, m)
			if len(s.marks) > 128 {
				s.marks = s.marks[len(s.marks)-128:]
			}
			s.info.Running = m.kind == 'C'
			if m.kind == 'D' && s.info.Attention != nil {
				s.clearAttention("command_finished")
			}
			stateChanged = true
		}
	}
	s.wake()
	s.mu.Unlock()
	if l := s.mgr.listener; l != nil {
		l.Output(s.info.ID, off, p)
	}
	if stateChanged {
		s.mgr.changed()
	}
}

// wake tells waiters that something changed. Caller holds s.mu.
func (s *session) wake() {
	close(s.notify)
	s.notify = make(chan struct{})
}

func (s *session) snapshotInfo() domain.SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.info
}

func (s *session) write(p string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-s.closed:
		return domain.ErrExited
	default:
	}
	_, err := s.proc.Write([]byte(p))
	return err
}

// writeHuman sends a person's keystrokes, interrupting any agent typing.
func (s *session) writeHuman(p string) error {
	s.mu.Lock()
	if s.interrupt != nil {
		close(s.interrupt)
		s.interrupt = nil
	}
	s.lastUsed = time.Now()
	logIt := time.Since(s.lastHumanAudit) > time.Minute
	if logIt {
		s.lastHumanAudit = time.Now()
	}
	s.mu.Unlock()
	if logIt {
		s.mgr.audit(AuditEvent{Type: "human_input", Session: s.info.ID, Actor: domain.HumanActor})
	}
	return s.write(p)
}

// checkAgent returns an error if agents may not use the session right now.
func (s *session) checkAgent() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.info.State {
	case domain.Authenticating:
		return domain.ErrAuthenticating
	case domain.Exited:
		return domain.ErrExited
	}
	return nil
}

// checkAgentInput returns an error if an agent may not send input right now: the
// checks of checkAgent, plus a pending request to the person or a password prompt.
func (s *session) checkAgentInput() error {
	if err := s.checkAgent(); err != nil {
		return err
	}
	s.mu.Lock()
	waiting := s.info.Attention != nil
	s.mu.Unlock()
	if waiting {
		return domain.ErrWaitingForHuman
	}
	if s.proc.PasswordPrompt() {
		return domain.ErrPasswordPrompt
	}
	return nil
}

// typeAgent types text one character at a time. A person typing stops it, and it
// never types into a password prompt.
func (s *session) typeAgent(ctx context.Context, actor domain.Actor, text string, delay time.Duration) error {
	if err := s.checkAgentInput(); err != nil {
		return err
	}
	if !s.typingMu.TryLock() {
		return domain.ErrBusy
	}
	defer s.typingMu.Unlock()

	intr := make(chan struct{})
	s.mu.Lock()
	s.interrupt = intr
	s.info.Typing = actor.Name
	s.lastUsed = time.Now()
	s.mu.Unlock()
	s.mgr.changed()
	defer func() {
		s.mu.Lock()
		if s.interrupt == intr {
			s.interrupt = nil
		}
		s.info.Typing = ""
		s.mu.Unlock()
		s.mgr.changed()
	}()

	if delay <= 0 {
		select {
		case <-intr:
			return domain.ErrInterruptedByHuman
		default:
		}
		return s.write(text)
	}
	t := time.NewTimer(0)
	defer t.Stop()
	for _, r := range text {
		select {
		case <-intr:
			return domain.ErrInterruptedByHuman
		case <-ctx.Done():
			return ctx.Err()
		case <-s.closed:
			return domain.ErrExited
		case <-t.C:
		}
		if s.proc.PasswordPrompt() {
			return domain.ErrPasswordPrompt
		}
		if err := s.write(string(r)); err != nil {
			return err
		}
		t.Reset(delay)
	}
	// Let the last character's delay pass so a following Enter is visible as separate.
	select {
	case <-intr:
		return domain.ErrInterruptedByHuman
	case <-t.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// waitFor blocks until cond (checked under s.mu) is true, the context ends, or the
// session closes.
func (s *session) waitFor(ctx context.Context, cond func() bool) error {
	for {
		s.mu.Lock()
		if cond() {
			s.mu.Unlock()
			return nil
		}
		ch := s.notify
		exited := s.info.State == domain.Exited
		s.mu.Unlock()
		if exited {
			return domain.ErrExited
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *session) close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		_ = s.proc.Close()
	})
}
