// Package desktop connects the core to the Wails app window. TerminalService is bound
// to the frontend (called from JavaScript through the Wails bridge), and Bridge sends
// output and session changes to the window as events.
//
// Everything that arrives here comes from the person at the app, so input is sent as
// the human actor. This is the only way to start an elevated session.
package desktop

import (
	"encoding/base64"
	"sync"
	"time"

	"github.com/naseebnaushad/turttyl/internal/app"
	"github.com/naseebnaushad/turttyl/internal/domain"
)

// Event names the frontend listens to.
const (
	EventOutput    = "terminal:output"
	EventSessions  = "terminal:sessions"
	EventAttention = "terminal:attention"
	// EventAppearance carries a new desktop look (see TerminalService.Appearance).
	EventAppearance = "app:appearance"
)

// OutputEvent carries a batch of output. Offset is where Data starts in the session's
// output stream, so the UI can skip anything it already drew from a snapshot.
type OutputEvent struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset"`
	Data   string `json:"data"` // base64
}

// Emitter sends an event to the window (app.Event.Emit in production).
type Emitter func(name string, data any)

// Bridge batches output (about 60 times a second) so heavy output can't flood the
// window, and implements app.Listener.
type Bridge struct {
	emit Emitter
	m    *app.Manager
	// Alert gets the person's attention (bounces the Dock icon on macOS). It never
	// takes keyboard focus: keystrokes meant for another app must not land in a terminal.
	Alert func()

	mu      sync.Mutex
	pending map[string]*OutputEvent
	raw     map[string][]byte
	listDue bool
	stop    chan struct{}
}

// NewBridge starts the batching loop. Call SetManager before sessions start.
func NewBridge(emit Emitter) *Bridge {
	b := &Bridge{emit: emit, pending: map[string]*OutputEvent{}, raw: map[string][]byte{}, stop: make(chan struct{})}
	go b.loop()
	return b
}

// SetManager gives the bridge the manager it reports on.
func (b *Bridge) SetManager(m *app.Manager) { b.m = m }

// Close stops the batching loop.
func (b *Bridge) Close() { close(b.stop) }

// Output implements app.Listener.
func (b *Bridge) Output(id string, offset int64, data []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pending[id]; !ok {
		b.pending[id] = &OutputEvent{ID: id, Offset: offset}
	}
	b.raw[id] = append(b.raw[id], data...)
}

// SessionsChanged implements app.Listener.
func (b *Bridge) SessionsChanged() {
	b.mu.Lock()
	b.listDue = true
	b.mu.Unlock()
}

// NeedsAttention implements app.Listener.
func (b *Bridge) NeedsAttention(info domain.SessionInfo) {
	b.SessionsChanged()
	b.emit(EventAttention, info)
	if b.Alert != nil {
		b.Alert()
	}
}

func (b *Bridge) loop() {
	t := time.NewTicker(16 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-b.stop:
			return
		case <-t.C:
		}
		b.flush()
	}
}

func (b *Bridge) flush() {
	b.mu.Lock()
	events := make([]*OutputEvent, 0, len(b.pending))
	for id, ev := range b.pending {
		ev.Data = base64.StdEncoding.EncodeToString(b.raw[id])
		events = append(events, ev)
	}
	b.pending = map[string]*OutputEvent{}
	b.raw = map[string][]byte{}
	listDue := b.listDue
	b.listDue = false
	b.mu.Unlock()
	if listDue && b.m != nil {
		b.emit(EventSessions, b.m.List())
	}
	for _, ev := range events {
		b.emit(EventOutput, ev)
	}
}

// Snapshot is a session's kept output for the UI to draw when it opens a tab.
type Snapshot struct {
	Data   string `json:"data"` // base64
	Offset int64  `json:"offset"`
}

// AppInfo tells the UI about the app.
type AppInfo struct {
	Version          string `json:"version"`
	ElevationEnabled bool   `json:"elevationEnabled"`
}

// TerminalService is bound to the frontend. Its methods are called by name from
// JavaScript ("github.com/naseebnaushad/turttyl/internal/adapters/desktop.TerminalService.<Method>").
type TerminalService struct {
	M       *app.Manager
	Version string
	// Look returns the desktop look to follow (the Omarchy theme on Linux), or nil
	// for the app's own look. Optional.
	Look func() any
}

// Appearance returns the desktop look the window should follow, or nil.
func (s *TerminalService) Appearance() any {
	if s.Look == nil {
		return nil
	}
	return s.Look()
}

// Info returns the app version and whether elevated sessions are allowed.
func (s *TerminalService) Info() AppInfo {
	return AppInfo{Version: s.Version, ElevationEnabled: s.M.ElevationEnabled()}
}

// List returns all sessions.
func (s *TerminalService) List() []domain.SessionInfo { return s.M.List() }

// Create starts a normal session for the person at the app.
func (s *TerminalService) Create(name string, cols, rows int) (domain.SessionInfo, error) {
	return s.M.Create(domain.HumanActor, app.CreateRequest{Name: name, Cols: cols, Rows: rows})
}

// CreateElevated starts an elevated session. The person unlocks it by typing their
// sudo password into the terminal; agents can't use it until then.
func (s *TerminalService) CreateElevated(name string, cols, rows int) (domain.SessionInfo, error) {
	return s.M.Create(domain.HumanActor, app.CreateRequest{Name: name, Cols: cols, Rows: rows, Elevated: true})
}

// Snapshot returns the kept output of a session.
func (s *TerminalService) Snapshot(id string) (Snapshot, error) {
	data, from, err := s.M.Snapshot(id)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Data: base64.StdEncoding.EncodeToString(data), Offset: from}, nil
}

// Write sends the person's keystrokes. It stops any agent that is typing.
func (s *TerminalService) Write(id, data string) error { return s.M.WriteHuman(id, data) }

// Resize sets the terminal size.
func (s *TerminalService) Resize(id string, cols, rows int) error { return s.M.Resize(id, cols, rows) }

// HandBack tells the agent waiting on this session that the person is done.
func (s *TerminalService) HandBack(id string) error { return s.M.HandBack(id) }

// Close ends a session.
func (s *TerminalService) Close(id string) error { return s.M.Close(domain.HumanActor, id) }
