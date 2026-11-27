// Package domain holds the core types of the shared terminal: sessions, the actors
// who use them, and the rules for validating what those actors send in.
// It has no dependencies outside the standard library.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Limits applied at every trust boundary (UI bridge and control socket).
const (
	MaxNameLen       = 64
	MaxActorLen      = 64
	MaxTypeTextBytes = 8 * 1024
	MaxWriteBytes    = 64 * 1024 // one paste from the UI
	MaxReadBytes     = 256 * 1024
	MaxCommandBytes  = 8 * 1024
	MaxTypingDelayMs = 200
	MinCols, MaxCols = 2, 1000
	MinRows, MaxRows = 2, 500
)

// ActorKind says who is acting on a session.
type ActorKind string

const (
	// Human is the person using the app window.
	Human ActorKind = "human"
	// Agent is an AI agent connected through MCP (or any control-socket client).
	Agent ActorKind = "agent"
)

// Actor identifies who sent input or created a session.
type Actor struct {
	Kind ActorKind `json:"kind"`
	Name string    `json:"name"`
}

// HumanActor is the person at the app.
var HumanActor = Actor{Kind: Human, Name: "you"}

// State is a session's lifecycle state.
type State string

const (
	// Starting: the shell is running but has not shown its first prompt yet.
	Starting State = "starting"
	// Authenticating: an elevated session is waiting for the person to answer sudo.
	Authenticating State = "authenticating"
	// Ready: the shell has shown a prompt and accepts input from anyone.
	Ready State = "ready"
	// Exited: the shell has ended. The output stays readable until the session is closed.
	Exited State = "exited"
)

// SessionInfo is the public description of a session.
type SessionInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	Elevated  bool   `json:"elevated"`
	State     State  `json:"state"`
	CreatedBy Actor  `json:"createdBy"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	// Typing names the agent typing into the session right now, if any.
	Typing string `json:"typing,omitempty"`
	// Running is true while a command is running (between OSC 133 C and D).
	Running bool `json:"running"`
	// PasswordPrompt is true while the program in the terminal is reading a password
	// (echo off, line mode on). Agents can't type then.
	PasswordPrompt bool `json:"passwordPrompt,omitempty"`
	// Attention is set while an agent is waiting for the person to do something.
	Attention *Attention `json:"attention,omitempty"`
}

// AttentionKind says why an agent needs the person.
type AttentionKind string

const (
	// NeedsPassword: a command the agent is waiting on asks for a password. It clears
	// by itself once the prompt is gone.
	NeedsPassword AttentionKind = "password"
	// NeedsInput: the agent asked the person to answer a prompt or take a step. It
	// clears when the person hands back or the command finishes.
	NeedsInput AttentionKind = "input"
)

// Attention is a request from an agent for the person to act in a session.
type Attention struct {
	Kind   AttentionKind `json:"kind"`
	Agent  string        `json:"agent"`
	Reason string        `json:"reason"`
	Since  time.Time     `json:"since"`
}

// MaxReasonLen limits the reason an agent gives when asking for help.
const MaxReasonLen = 300

// Errors returned by the core. Adapters map them to user-facing messages.
var (
	ErrNotFound           = errors.New("session not found")
	ErrExited             = errors.New("session has exited")
	ErrAuthenticating     = errors.New("session is waiting for a person to unlock sudo; agents cannot use it yet")
	ErrElevationDisabled  = errors.New("elevated sessions are turned off in the config")
	ErrElevationHumanOnly = errors.New("only a person in the app can start an elevated session")
	ErrInterruptedByHuman = errors.New("interrupted_by_human: a person typed into the session, so the rest of the agent input was dropped")
	ErrBusy               = errors.New("another agent is typing into this session; try again shortly")
	ErrTimeout            = errors.New("timed out waiting for the command to finish")
	ErrNotOwner           = errors.New("agents can only close sessions they created")
	ErrNoShellIntegration = errors.New("the shell has not reported a prompt yet")
	ErrPasswordPrompt     = errors.New("the terminal is asking for a password and agents can't type passwords; call request_human_input so the person types it, or wait_for_command if you are already waiting")
	ErrWaitingForHuman    = errors.New("this session is waiting for the person; wait until they hand back (request_human_input / wait_for_command)")
	ErrNoCommand          = errors.New("no command has run in this session yet")
)

// ValidationError is a bad-input error safe to show to the caller.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &ValidationError{Msg: fmt.Sprintf(format, a...)} }

// CleanLabel normalises a session name or actor name: it trims space, drops control
// characters and enforces a maximum length. An empty result returns fallback.
func CleanLabel(s string, max int, fallback string) (string, error) {
	if len(s) > max*4 {
		return "", invalid("name is too long (max %d characters)", max)
	}
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(out) > max {
		return "", invalid("name is too long (max %d characters)", max)
	}
	if out == "" {
		return fallback, nil
	}
	return out, nil
}

// ValidateText checks text an agent wants typed or run.
func ValidateText(s string, max int) error {
	if len(s) > max {
		return invalid("text is too long (max %d bytes)", max)
	}
	if !utf8.ValidString(s) {
		return invalid("text must be valid UTF-8")
	}
	return nil
}

// ValidateSize checks a terminal size.
func ValidateSize(cols, rows int) error {
	if cols < MinCols || cols > MaxCols || rows < MinRows || rows > MaxRows {
		return invalid("terminal size %dx%d is out of range", cols, rows)
	}
	return nil
}

// ValidateDelay checks a per-character typing delay.
func ValidateDelay(ms int) error {
	if ms < 0 || ms > MaxTypingDelayMs {
		return invalid("delay must be between 0 and %d ms", MaxTypingDelayMs)
	}
	return nil
}

// Keys maps the special key names agents may send to their terminal bytes.
var Keys = map[string]string{
	"enter": "\r", "tab": "\t", "escape": "\x1b", "backspace": "\x7f",
	"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
	"home": "\x1b[H", "end": "\x1b[F", "pageup": "\x1b[5~", "pagedown": "\x1b[6~",
	"ctrl-a": "\x01", "ctrl-c": "\x03", "ctrl-d": "\x04", "ctrl-e": "\x05",
	"ctrl-k": "\x0b", "ctrl-l": "\x0c", "ctrl-r": "\x12", "ctrl-u": "\x15",
	"ctrl-w": "\x17", "ctrl-z": "\x1a",
}

// KeyBytes resolves a list of key names to bytes.
func KeyBytes(names []string) (string, error) {
	if len(names) == 0 || len(names) > 32 {
		return "", invalid("send between 1 and 32 keys")
	}
	var b strings.Builder
	for _, n := range names {
		k, ok := Keys[strings.ToLower(strings.TrimSpace(n))]
		if !ok {
			return "", invalid("unknown key %q", n)
		}
		b.WriteString(k)
	}
	return b.String(), nil
}
