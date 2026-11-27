package app

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/naseebnaushad/turttyl/internal/domain"
)

// Hand-off between agents and the person: when a command an agent is waiting on needs
// something only the person can give (a password, an answer, a choice in a menu), the
// session is flagged, the person acts in the terminal, and the agent carries on.

const (
	// DefaultIdle is how long run_command waits with no output before returning to
	// the agent so it can decide what to do.
	DefaultIdle = 30 * time.Second
	// promptIdle is the shorter wait when the last line looks like a question.
	promptIdle = 2 * time.Second
	// DefaultHumanTimeout is how long request_human_input waits for the person.
	DefaultHumanTimeout = 5 * time.Minute
	maxHumanTimeout     = 60 * time.Minute
)

// promptRE matches lines that usually wait for an answer.
var promptRE = regexp.MustCompile(`(?i)(\[y/n\]|\(y/n\)|\[yes/no\]|\(yes/no|\by/n\b|press (any key|enter|return)|` +
	`(continue|proceed|overwrite|replace|remove|delete)\b.*\?|passphrase|username|login:|\?\s*$|:\s*$|>\s*$)`)

// RunResult is what run_command and wait_for_command return.
type RunResult struct {
	Output   string `json:"output"`
	ExitCode *int   `json:"exitCode,omitempty"`
	// Running is true when the command has not finished yet.
	Running  bool  `json:"running"`
	TimedOut bool  `json:"timedOut"`
	Cursor   int64 `json:"cursor"`
	// LastLine is the last line on screen, when the command is still running.
	LastLine string `json:"lastLine,omitempty"`
	// LooksLikePrompt hints that the command is waiting for an answer.
	LooksLikePrompt bool `json:"looksLikePrompt,omitempty"`
	// WaitingForHuman is true while the person has been asked to act.
	WaitingForHuman bool `json:"waitingForHuman,omitempty"`
	// Next tells the agent what it can do now.
	Next string `json:"next,omitempty"`
}

// HumanResult is what request_human_input returns.
type HumanResult struct {
	// ResolvedBy is "human" (handed back), "password_entered", "command_finished",
	// "session_ended" or "timeout".
	ResolvedBy string `json:"resolvedBy"`
	// Output is what appeared in the terminal while the agent waited (plain text).
	Output   string `json:"output"`
	Cursor   int64  `json:"cursor"`
	Running  bool   `json:"running"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Next     string `json:"next,omitempty"`
}

// clearAttention ends the current request to the person. Caller holds s.mu.
func (s *session) clearAttention(by string) {
	if s.info.Attention == nil {
		return
	}
	a := s.info.Attention
	s.info.Attention = nil
	s.resolvedBy = by
	s.lastAttn = time.Now()
	go s.mgr.audit(AuditEvent{Type: "attention_resolved", Session: s.info.ID,
		Actor: domain.Actor{Kind: domain.Agent, Name: a.Agent}, Detail: by})
}

// setAttention asks the person to act. Caller holds s.mu. It returns the request.
func (s *session) setAttention(kind domain.AttentionKind, agent, reason string) *domain.Attention {
	a := &domain.Attention{Kind: kind, Agent: agent, Reason: reason, Since: time.Now().UTC()}
	s.info.Attention = a
	s.resolvedBy = ""
	s.lastAttn = time.Now()
	info := s.info
	go func() {
		s.mgr.audit(AuditEvent{Type: "attention_requested", Session: info.ID,
			Actor: domain.Actor{Kind: domain.Agent, Name: agent}, Detail: string(kind) + ": " + reason})
		if l := s.mgr.listener; l != nil {
			l.NeedsAttention(info)
		}
		s.mgr.changed()
	}()
	return a
}

func (s *session) addWaiter(name string) {
	s.mu.Lock()
	s.waiters++
	s.waiter = name
	s.mu.Unlock()
}

func (s *session) doneWaiter() {
	s.mu.Lock()
	s.waiters--
	s.mu.Unlock()
}

// commandSpan finds the command that started at or after from: the offset where its
// output starts (its C marker, or from if the shell sent none) and its D marker.
// Caller holds s.mu.
func (s *session) commandSpan(from int64) (start int64, hasC bool, done *marker) {
	start = from
	for i := range s.marks {
		mk := s.marks[i]
		if mk.offset < from {
			continue
		}
		if mk.kind == 'C' && !hasC {
			start, hasC = mk.offset, true
		}
		if mk.kind == 'D' {
			return start, hasC, &mk
		}
	}
	return start, hasC, nil
}

// lastLine returns the last non-empty line of the recent output. Caller holds s.mu.
func (s *session) lastLine() string {
	tail, _, _ := s.out.since(s.out.end-2048, 0)
	lines := strings.Split(plainText(tail), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			if len(l) > 300 {
				l = l[len(l)-300:]
			}
			return l
		}
	}
	return ""
}

// waitCommand waits for the command that started at or after from to finish. It
// returns early, with Running set, when the output has been idle (sooner if the last
// line looks like a question), and it never returns for idleness while the person is
// being asked for something. idle < 0 turns early return off.
func (s *session) waitCommand(ctx context.Context, actor domain.Actor, from int64, idle time.Duration) (RunResult, error) {
	s.addWaiter(actor.Name)
	defer s.doneWaiter()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	began := time.Now()
	for {
		s.mu.Lock()
		start, hasC, done := s.commandSpan(from)
		if done != nil {
			text := plainText(s.out.between(start, done.offset))
			s.mu.Unlock()
			if hasC {
				text = strings.Trim(text, "\n")
			} else {
				text = dropEcho(text)
			}
			code := done.code
			return RunResult{Output: text, ExitCode: &code, Cursor: done.offset}, nil
		}
		if s.info.State == domain.Exited {
			s.mu.Unlock()
			return RunResult{}, domain.ErrExited
		}
		waitingForHuman := s.info.Attention != nil || s.info.PasswordPrompt
		quiet := time.Since(s.lastOutput)
		// Count from the latest of: last output, last hand-off, start of this wait.
		for _, t := range []time.Time{s.lastAttn, began} {
			if q := time.Since(t); q < quiet {
				quiet = q
			}
		}
		last := s.lastLine()
		// A password line the person already answered is not a new question.
		looksLikePrompt := promptRE.MatchString(last) && last != s.pwLine
		ch := s.notify
		s.mu.Unlock()

		if idle >= 0 && !waitingForHuman {
			limit := idle
			if looksLikePrompt && promptIdle < limit {
				limit = promptIdle
			}
			if quiet >= limit {
				return s.partial(from, last, looksLikePrompt, false, false), nil
			}
		}
		select {
		case <-ch:
		case <-tick.C:
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return s.partial(from, last, looksLikePrompt, waitingForHuman, true), nil
			}
			return RunResult{}, ctx.Err()
		}
	}
}

// partial builds the result for a command that is still running.
func (s *session) partial(from int64, last string, looksLikePrompt, waitingForHuman, timedOut bool) RunResult {
	s.mu.Lock()
	start, hasC, _ := s.commandSpan(from)
	end := s.out.end
	text := plainText(s.out.between(start, end))
	s.mu.Unlock()
	if hasC {
		text = strings.Trim(text, "\n")
	} else {
		text = dropEcho(text)
	}
	res := RunResult{Output: text, Running: true, TimedOut: timedOut, Cursor: end, LastLine: last,
		LooksLikePrompt: looksLikePrompt, WaitingForHuman: waitingForHuman}
	switch {
	case waitingForHuman:
		res.Next = "The person has been asked to act in this session. Call wait_for_command to keep waiting."
	case looksLikePrompt:
		res.Next = "The command seems to be waiting for an answer (see lastLine). Answer it with type/send_keys if you can, " +
			"or call request_human_input to ask the person; then call wait_for_command."
	default:
		res.Next = "The command is still running. Call wait_for_command to keep waiting, read_output to look, or send_keys ctrl-c to stop it."
	}
	return res
}

// WaitCommand waits for the command running in a session (or the last one, if it has
// finished) and returns its output and exit code. See RunOpts for idle and timeout.
func (m *Manager) WaitCommand(ctx context.Context, actor domain.Actor, id string, opts RunOpts) (RunResult, error) {
	s, err := m.get(id)
	if err != nil {
		return RunResult{}, err
	}
	if err := s.checkAgent(); err != nil {
		return RunResult{}, err
	}
	s.mu.Lock()
	from := int64(-1)
	for i := len(s.marks) - 1; i >= 0; i-- {
		if s.marks[i].kind == 'C' {
			from = s.marks[i].offset
			break
		}
	}
	s.mu.Unlock()
	if from < 0 {
		return RunResult{}, domain.ErrNoCommand
	}
	ctx, cancel := context.WithTimeout(ctx, opts.timeout())
	defer cancel()
	return s.waitCommand(ctx, actor, from, opts.idle())
}

// RequestHuman asks the person to act in a session (answer a prompt, choose in a
// menu, type something the agent can't) and waits until they hand back, the running
// command finishes, or the timeout passes.
func (m *Manager) RequestHuman(ctx context.Context, actor domain.Actor, id, reason string, timeout time.Duration) (HumanResult, error) {
	reason, err := domain.CleanLabel(reason, domain.MaxReasonLen, "The agent needs your help in this session.")
	if err != nil {
		return HumanResult{}, err
	}
	if timeout <= 0 {
		timeout = DefaultHumanTimeout
	}
	if timeout > maxHumanTimeout {
		timeout = maxHumanTimeout
	}
	s, err := m.get(id)
	if err != nil {
		return HumanResult{}, err
	}
	if err := s.checkAgent(); err != nil {
		return HumanResult{}, err
	}
	s.mu.Lock()
	a := s.info.Attention
	switch {
	case a == nil:
		a = s.setAttention(domain.NeedsInput, actor.Name, reason)
	case a.Kind == domain.NeedsInput && a.Agent != actor.Name:
		s.mu.Unlock()
		return HumanResult{}, domain.ErrWaitingForHuman
	}
	start := s.out.end
	s.mu.Unlock()
	s.addWaiter(actor.Name)
	defer s.doneWaiter()

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	werr := s.waitFor(ctx, func() bool { return s.info.Attention != a })

	s.mu.Lock()
	defer s.mu.Unlock()
	by := s.resolvedBy
	if werr != nil {
		if errors.Is(werr, context.DeadlineExceeded) {
			if s.info.Attention == a {
				s.clearAttention("timeout")
			}
			by = "timeout"
		} else if errors.Is(werr, domain.ErrExited) {
			by = "session_ended"
		} else {
			return HumanResult{}, werr
		}
	}
	out, _, _ := s.out.since(start, domain.MaxReadBytes)
	res := HumanResult{ResolvedBy: by, Output: plainText(out), Cursor: s.out.end, Running: s.info.Running}
	if _, _, done := s.commandSpan(start); done != nil {
		code := done.code
		res.ExitCode = &code
	}
	switch {
	case by == "timeout":
		res.Next = "The person did not respond in time. Read the output, then decide whether to ask again."
	case res.Running:
		res.Next = "Call wait_for_command to wait for the running command to finish."
	default:
		res.Next = "Read the output to see what the person did, then continue."
	}
	return res, nil
}

// HandBack is the person telling the waiting agent to carry on.
func (m *Manager) HandBack(id string) error {
	s, err := m.get(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.clearAttention("human")
	s.wake()
	s.mu.Unlock()
	m.changed()
	return nil
}

// passwordWatcher checks every session's terminal mode a few times a second. When a
// command an agent is waiting on starts reading a password, the person is asked to
// type it; when the prompt goes away, the agent carries on.
func (m *Manager) passwordWatcher() {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-t.C:
		}
		m.mu.Lock()
		ss := make([]*session, 0, len(m.sessions))
		for _, s := range m.sessions {
			ss = append(ss, s)
		}
		m.mu.Unlock()
		for _, s := range ss {
			m.checkPassword(s)
		}
	}
}

func (m *Manager) checkPassword(s *session) {
	pp := s.proc.PasswordPrompt()
	s.mu.Lock()
	if s.info.State == domain.Exited {
		s.mu.Unlock()
		return
	}
	changed := pp != s.info.PasswordPrompt
	if pp && changed {
		s.pwLine = s.lastLine()
	}
	s.info.PasswordPrompt = pp
	a := s.info.Attention
	switch {
	case pp && a == nil && s.waiters > 0:
		s.setAttention(domain.NeedsPassword, s.waiter, "A command is asking for a password. Type it in the terminal; the agent can't see it and will carry on once it's accepted.")
		changed = true
	case !pp && a != nil && a.Kind == domain.NeedsPassword:
		s.clearAttention("password_entered")
		changed = true
	}
	if changed {
		s.wake()
	}
	s.mu.Unlock()
	if changed {
		m.changed()
	}
}
