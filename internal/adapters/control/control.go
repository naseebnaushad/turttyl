// Package control is the local Unix-socket API that agents use (through the MCP
// server) to reach the sessions owned by the running app. Every caller is treated as
// an agent: it cannot start elevated sessions or act as the person at the app.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/naseebnaushad/turttyl/internal/app"
	"github.com/naseebnaushad/turttyl/internal/domain"
)

const maxLine = 1 << 20

// ErrAlreadyRunning means another copy of the app owns the socket.
var ErrAlreadyRunning = errors.New("another Turttyl app is already running")

// ErrAppNotRunning means nothing is listening on the socket.
var ErrAppNotRunning = errors.New("the Turttyl app is not running; open it, then try again")

// Request is one call over the socket.
type Request struct {
	Method string          `json:"method"`
	Actor  string          `json:"actor"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response answers a Request.
type Response struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Param types, shared by server and client.
type (
	CreateParams struct {
		Name string `json:"name,omitempty"`
		Cwd  string `json:"cwd,omitempty"`
	}
	TypeParams struct {
		Session string `json:"session"`
		Text    string `json:"text"`
		Submit  bool   `json:"submit,omitempty"`
		DelayMs *int   `json:"delayMs,omitempty"`
	}
	KeysParams struct {
		Session string   `json:"session"`
		Keys    []string `json:"keys"`
	}
	ReadParams struct {
		Session  string `json:"session"`
		Cursor   *int64 `json:"cursor,omitempty"`
		MaxBytes int    `json:"maxBytes,omitempty"`
	}
	RunParams struct {
		Session        string `json:"session"`
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
		IdleSeconds    *int   `json:"idleSeconds,omitempty"`
		DelayMs        *int   `json:"delayMs,omitempty"`
	}
	WaitParams struct {
		Session        string `json:"session"`
		TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
		IdleSeconds    *int   `json:"idleSeconds,omitempty"`
	}
	AskParams struct {
		Session        string `json:"session"`
		Reason         string `json:"reason"`
		TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	}
	SessionParams struct {
		Session string `json:"session"`
	}
)

// Server serves the socket.
type Server struct {
	m    *app.Manager
	log  *slog.Logger
	ln   net.Listener
	path string
	wg   sync.WaitGroup
	ctx  context.Context
	stop context.CancelFunc
}

// Listen creates the socket (mode 0600). A stale socket file is replaced; a live one
// means the app is already running.
func Listen(path string, m *app.Manager, log *slog.Logger) (*Server, error) {
	if len(path) > 100 { // sun_path is 104 bytes on macOS, 108 on Linux
		return nil, fmt.Errorf("socket path %q is too long; set TURTTYL_HOME to a shorter folder", path)
	}
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, ErrAlreadyRunning
	}
	_ = os.Remove(path)
	old := umask(0o177)
	ln, err := net.Listen("unix", path)
	umask(old)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	ctx, stop := context.WithCancel(context.Background())
	s := &Server{m: m, log: log, ln: ln, path: path, ctx: ctx, stop: stop}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Close stops the server and removes the socket file.
func (s *Server) Close() error {
	s.stop()
	err := s.ln.Close()
	s.wg.Wait()
	_ = os.Remove(s.path)
	return err
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		uc := c.(*net.UnixConn)
		if err := checkPeer(uc); err != nil {
			s.log.Warn("control socket: rejected connection", "reason", err.Error())
			c.Close()
			continue
		}
		s.wg.Add(1)
		go s.serve(uc)
	}
}

func (s *Server) serve(c net.Conn) {
	defer s.wg.Done()
	defer c.Close()
	go func() { <-s.ctx.Done(); c.Close() }()
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	enc := json.NewEncoder(c)
	for {
		_ = c.SetReadDeadline(time.Now().Add(15 * time.Minute))
		if !sc.Scan() {
			return
		}
		var req Request
		var resp Response
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			resp = Response{Error: "bad request"}
		} else {
			resp = s.handle(req)
		}
		_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if err := enc.Encode(resp); err != nil {
			return
		}
	}
}

func (s *Server) handle(req Request) Response {
	name, err := domain.CleanLabel(req.Actor, domain.MaxActorLen, "agent")
	if err != nil {
		return Response{Error: err.Error()}
	}
	actor := domain.Actor{Kind: domain.Agent, Name: name}
	result, err := s.dispatch(actor, req)
	if err != nil {
		return Response{Error: s.publicError(req.Method, err)}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return Response{Error: "internal error"}
	}
	return Response{OK: true, Result: raw}
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, &domain.ValidationError{Msg: "invalid parameters: " + err.Error()}
	}
	return v, nil
}

func (s *Server) dispatch(actor domain.Actor, req Request) (any, error) {
	ctx := s.ctx
	switch req.Method {
	case "list":
		return s.m.List(), nil
	case "create":
		p, err := decode[CreateParams](req.Params)
		if err != nil {
			return nil, err
		}
		return s.m.Create(actor, app.CreateRequest{Name: p.Name, Cwd: p.Cwd})
	case "type":
		p, err := decode[TypeParams](req.Params)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.m.Type(ctx, actor, p.Session, p.Text, p.Submit, p.DelayMs)
	case "keys":
		p, err := decode[KeysParams](req.Params)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.m.SendKeys(actor, p.Session, p.Keys)
	case "read":
		p, err := decode[ReadParams](req.Params)
		if err != nil {
			return nil, err
		}
		cursor := int64(-1)
		if p.Cursor != nil {
			cursor = *p.Cursor
		}
		return s.m.Read(p.Session, cursor, p.MaxBytes)
	case "run":
		p, err := decode[RunParams](req.Params)
		if err != nil {
			return nil, err
		}
		return s.m.Run(ctx, actor, p.Session, p.Command, app.RunOpts{
			Timeout: seconds(p.TimeoutSeconds), Idle: idle(p.IdleSeconds), DelayMs: p.DelayMs})
	case "wait":
		p, err := decode[WaitParams](req.Params)
		if err != nil {
			return nil, err
		}
		return s.m.WaitCommand(ctx, actor, p.Session, app.RunOpts{Timeout: seconds(p.TimeoutSeconds), Idle: idle(p.IdleSeconds)})
	case "ask":
		p, err := decode[AskParams](req.Params)
		if err != nil {
			return nil, err
		}
		return s.m.RequestHuman(ctx, actor, p.Session, p.Reason, seconds(p.TimeoutSeconds))
	case "close":
		p, err := decode[SessionParams](req.Params)
		if err != nil {
			return nil, err
		}
		return struct{}{}, s.m.Close(actor, p.Session)
	}
	return nil, &domain.ValidationError{Msg: "unknown method"}
}

func seconds(n int) time.Duration { return time.Duration(n) * time.Second }

// idle maps the idle_seconds parameter: omitted = default, 0 = never return early.
func idle(n *int) time.Duration {
	switch {
	case n == nil:
		return 0
	case *n <= 0:
		return -1
	case *n > 3600:
		return time.Hour
	}
	return seconds(*n)
}

// publicError returns a message safe to show the caller; anything unexpected is
// logged and replaced with a generic message.
func (s *Server) publicError(method string, err error) string {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return ve.Msg
	}
	for _, known := range []error{
		domain.ErrNotFound, domain.ErrExited, domain.ErrAuthenticating, domain.ErrElevationDisabled,
		domain.ErrElevationHumanOnly, domain.ErrInterruptedByHuman, domain.ErrBusy, domain.ErrTimeout,
		domain.ErrNotOwner, domain.ErrNoShellIntegration, domain.ErrPasswordPrompt, domain.ErrWaitingForHuman,
		domain.ErrNoCommand,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	s.log.Error("control call failed", "method", method, "err", err)
	return "internal error"
}

// Client calls the socket. Each call uses its own connection, so calls can run in
// parallel (for example a long run_command and a read_output).
type Client struct {
	Path  string
	Actor string
}

// Call sends one request and decodes the result into out (which may be nil).
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Path)
	if err != nil {
		return ErrAppNotRunning
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(conn).Encode(Request{Method: method, Actor: c.Actor, Params: raw}); err != nil {
		return fmt.Errorf("send to app: %w", err)
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), maxLine*2)
	if !sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("the app closed the connection")
	}
	var resp Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return errors.New("bad response from the app")
	}
	if !resp.OK {
		return errors.New(resp.Error)
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}
