package control

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/naseebnaushad/turttyl/internal/app"
	"github.com/naseebnaushad/turttyl/internal/domain"
)

// nullSpawner gives sessions that never print anything.
type nullSpawner struct{}

type nullProc struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (nullSpawner) Spawn(app.ShellSpec) (app.Process, error) {
	r, w := io.Pipe()
	return &nullProc{r, w}, nil
}
func (p *nullProc) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *nullProc) Write(b []byte) (int, error) { return len(b), nil }
func (p *nullProc) Resize(int, int) error       { return nil }
func (p *nullProc) Wait() (int, error)          { return 0, nil }
func (p *nullProc) Close() error                { return p.w.Close() }
func (p *nullProc) PasswordPrompt() bool        { return false }

func start(t *testing.T, elevation bool) (*app.Manager, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "atc") // short path: socket paths are limited
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	m := app.NewManager(app.Config{ElevationEnabled: elevation, DefaultCwd: dir}, nullSpawner{}, nil, nil)
	t.Cleanup(m.Shutdown)
	sock := filepath.Join(dir, "c.sock")
	s, err := Listen(sock, m, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return m, sock
}

func TestSocketPermissionsAndSingleInstance(t *testing.T) {
	m, sock := start(t, false)
	st, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode = %v", st.Mode().Perm())
	}
	if _, err := Listen(sock, m, slog.Default()); err != ErrAlreadyRunning {
		t.Fatalf("second Listen err = %v", err)
	}
}

func TestCallsActAsAgent(t *testing.T) {
	m, sock := start(t, true)
	c := &Client{Path: sock, Actor: "claude\x1b[31m"}
	var info domain.SessionInfo
	if err := c.Call(context.Background(), "create", CreateParams{Name: "x"}, &info); err != nil {
		t.Fatal(err)
	}
	if info.CreatedBy.Kind != domain.Agent || info.CreatedBy.Name != "claude[31m" {
		t.Fatalf("createdBy = %+v", info.CreatedBy)
	}
	// There is no way to ask for an elevated session over the socket.
	err := c.Call(context.Background(), "create", json.RawMessage(`{"elevated":true}`), nil)
	if err == nil || !strings.Contains(err.Error(), "invalid parameters") {
		t.Fatalf("elevated create err = %v", err)
	}
	// Agents can't close the person's sessions.
	mine, _ := m.Create(domain.HumanActor, app.CreateRequest{})
	if err := c.Call(context.Background(), "close", SessionParams{Session: mine.ID}, nil); err == nil || err.Error() != domain.ErrNotOwner.Error() {
		t.Fatalf("close err = %v", err)
	}
}

func TestBadRequests(t *testing.T) {
	_, sock := start(t, false)
	c := &Client{Path: sock}
	if err := c.Call(context.Background(), "nope", nil, nil); err == nil || err.Error() != "unknown method" {
		t.Fatalf("err = %v", err)
	}
	if err := c.Call(context.Background(), "type", TypeParams{Session: "missing", Text: "x"}, nil); err == nil || err.Error() != domain.ErrNotFound.Error() {
		t.Fatalf("err = %v", err)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("not json\n"))
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	if !strings.Contains(string(buf[:n]), "bad request") {
		t.Fatalf("got %q", buf[:n])
	}
}

func TestAppNotRunning(t *testing.T) {
	c := &Client{Path: filepath.Join(t.TempDir(), "none.sock")}
	if err := c.Call(context.Background(), "list", nil, nil); err != ErrAppNotRunning {
		t.Fatalf("err = %v", err)
	}
}
