// Package app holds the use cases of the shared terminal: creating sessions, typing
// into them as a person or an agent, reading their output and running commands.
// It talks to the outside world only through the ports declared in this file.
package app

import (
	"io"
	"time"

	"github.com/naseebnaushad/turttyl/internal/domain"
)

// Process is a running shell attached to a pseudo-terminal.
type Process interface {
	io.Reader
	io.Writer
	// Resize changes the terminal size seen by the shell.
	Resize(cols, rows int) error
	// Wait blocks until the shell exits and returns its exit code.
	Wait() (int, error)
	// Close hangs up the terminal and stops the shell.
	Close() error
	// PasswordPrompt reports whether the program in the terminal is reading a
	// password right now (echo off while in line mode).
	PasswordPrompt() bool
}

// ShellSpec says what kind of shell to start.
type ShellSpec struct {
	Cwd      string
	Elevated bool
	Cols     int
	Rows     int
}

// Spawner starts shells. The real one uses a PTY; tests use a fake.
type Spawner interface {
	Spawn(spec ShellSpec) (Process, error)
}

// AuditEvent is one line of the audit log.
type AuditEvent struct {
	Time    time.Time    `json:"time"`
	Type    string       `json:"type"`
	Session string       `json:"session,omitempty"`
	Actor   domain.Actor `json:"actor"`
	// Detail is agent input or a short note. Never a person's keystrokes.
	Detail string `json:"detail,omitempty"`
}

// AuditLog records what happens in sessions.
type AuditLog interface {
	Record(AuditEvent)
}

// Listener is told about output and session changes, for the UI.
// Calls come from session goroutines and must not block for long.
type Listener interface {
	Output(sessionID string, offset int64, data []byte)
	SessionsChanged()
	// NeedsAttention is called when an agent starts waiting for the person.
	NeedsAttention(info domain.SessionInfo)
}

// Config is the part of the app configuration the core needs.
type Config struct {
	ScrollbackBytes  int
	TypingDelayMs    int
	ElevationEnabled bool
	ElevationIdle    time.Duration
	DefaultCwd       string
}
