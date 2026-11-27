// Package mcpserver is the MCP (Model Context Protocol) server that agents such as
// Claude Code start with `turttyl mcp`. It forwards each tool call to the
// running app over the control socket, so agents work in the same sessions the person
// sees in the app window.
package mcpserver

import (
	"context"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/naseebnaushad/turttyl/internal/adapters/control"
	"github.com/naseebnaushad/turttyl/internal/app"
	"github.com/naseebnaushad/turttyl/internal/domain"
)

// Inputs and outputs of the tools. The jsonschema tags become the tool descriptions
// agents read.
type (
	listIn  struct{}
	listOut struct {
		Sessions []domain.SessionInfo `json:"sessions"`
	}
	createIn struct {
		Name string `json:"name,omitempty" jsonschema:"short name shown on the session tab"`
		Cwd  string `json:"cwd,omitempty" jsonschema:"absolute folder to start in (default: home or the configured folder)"`
	}
	typeIn struct {
		Session string `json:"session" jsonschema:"session id from list_sessions or create_session"`
		Text    string `json:"text" jsonschema:"text to type, character by character, so the person watching sees it"`
		Submit  bool   `json:"submit,omitempty" jsonschema:"press Enter after the text"`
		DelayMs *int   `json:"delay_ms,omitempty" jsonschema:"pause between characters in ms (0-200); default from the app config"`
	}
	keysIn struct {
		Session string   `json:"session" jsonschema:"session id"`
		Keys    []string `json:"keys" jsonschema:"keys to press in order: enter tab escape backspace up down left right home end pageup pagedown ctrl-a ctrl-c ctrl-d ctrl-e ctrl-k ctrl-l ctrl-r ctrl-u ctrl-w ctrl-z"`
	}
	readIn struct {
		Session  string `json:"session" jsonschema:"session id"`
		Cursor   *int64 `json:"cursor,omitempty" jsonschema:"cursor returned by an earlier call; omit for the most recent output"`
		MaxBytes int    `json:"max_bytes,omitempty" jsonschema:"maximum bytes of output to return (default 65536)"`
	}
	runIn struct {
		Session        string `json:"session" jsonschema:"session id"`
		Command        string `json:"command" jsonschema:"one command line; it is typed into the terminal and Enter is pressed"`
		TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"how long to wait for the command to finish (default 120, max 1800); on timeout it keeps running"`
		IdleSeconds    *int   `json:"idle_seconds,omitempty" jsonschema:"return early after this many seconds with no output, so you can react to a prompt (default 30; about 2 when the last line looks like a question; 0 = never)"`
		DelayMs        *int   `json:"delay_ms,omitempty" jsonschema:"pause between characters in ms (0-200)"`
	}
	waitIn struct {
		Session        string `json:"session" jsonschema:"session id"`
		TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"how long to wait (default 120, max 1800)"`
		IdleSeconds    *int   `json:"idle_seconds,omitempty" jsonschema:"return early after this many seconds with no output (default 30; 0 = never)"`
	}
	askIn struct {
		Session        string `json:"session" jsonschema:"session id"`
		Reason         string `json:"reason" jsonschema:"one short sentence telling the person what to do, e.g. 'Choose the region in the installer menu' (max 300 characters)"`
		TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"how long to wait for the person (default 300, max 3600)"`
	}
	sessionIn struct {
		Session string `json:"session" jsonschema:"session id"`
	}
	okOut struct {
		OK bool `json:"ok"`
	}
)

// Run serves MCP over stdin/stdout until the client disconnects.
func Run(ctx context.Context, socketPath, version string) error {
	c := &control.Client{Path: socketPath}
	s := mcp.NewServer(&mcp.Implementation{Name: "turttyl", Version: version}, &mcp.ServerOptions{
		Instructions: "These tools drive real terminal sessions that a person is watching live in the Turttyl app. " +
			"Input is typed character by character. If a person types while you are typing, your input is stopped (interrupted_by_human): " +
			"read the output to see what they did before going on. Sessions marked elevated run as root: be careful. " +
			"Use run_command for commands that finish; use type, send_keys and read_output for interactive programs. " +
			"If a command asks for a password, never try to type one: the person is asked automatically and run_command/wait_for_command keeps waiting. " +
			"If run_command returns running=true, the command went quiet: if looksLikePrompt is true, answer the question yourself when you safely can, " +
			"or call request_human_input with a short reason so the person can answer or take over; then call wait_for_command to get the final output and exit code.",
	})

	// who returns the client for this call, named after the connected agent.
	who := func(req *mcp.CallToolRequest) *control.Client {
		cc := *c
		cc.Actor = actorName(req)
		return &cc
	}

	mcp.AddTool(s, &mcp.Tool{Name: "list_sessions", Description: "List the terminal sessions: id, name, folder, state, whether elevated (root), and who is typing."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ listIn) (*mcp.CallToolResult, listOut, error) {
			var out listOut
			err := who(req).Call(ctx, "list", nil, &out.Sessions)
			return nil, out, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "create_session", Description: "Open a new (normal, non-root) terminal session. It appears as a tab in the app."},
		func(ctx context.Context, req *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, domain.SessionInfo, error) {
			var out domain.SessionInfo
			err := who(req).Call(ctx, "create", control.CreateParams{Name: in.Name, Cwd: in.Cwd}, &out)
			return nil, out, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "type", Description: "Type text into a session character by character, optionally pressing Enter. Does not wait for output."},
		func(ctx context.Context, req *mcp.CallToolRequest, in typeIn) (*mcp.CallToolResult, okOut, error) {
			err := who(req).Call(ctx, "type", control.TypeParams{Session: in.Session, Text: in.Text, Submit: in.Submit, DelayMs: in.DelayMs}, nil)
			return nil, okOut{OK: err == nil}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "send_keys", Description: "Press special keys in a session, such as ctrl-c to stop a program or arrows in a menu."},
		func(ctx context.Context, req *mcp.CallToolRequest, in keysIn) (*mcp.CallToolResult, okOut, error) {
			err := who(req).Call(ctx, "keys", control.KeysParams{Session: in.Session, Keys: in.Keys}, nil)
			return nil, okOut{OK: err == nil}, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "read_output", Description: "Read a session's output as plain text. Pass the returned cursor next time to get only new output."},
		func(ctx context.Context, req *mcp.CallToolRequest, in readIn) (*mcp.CallToolResult, app.ReadResult, error) {
			var out app.ReadResult
			err := who(req).Call(ctx, "read", control.ReadParams{Session: in.Session, Cursor: in.Cursor, MaxBytes: in.MaxBytes}, &out)
			return nil, out, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "run_command", Description: "Type a command into a session, press Enter, wait for it to finish, and return its output and exit code. If it asks for a password, the person is asked to type it and the wait continues. If it goes quiet (for example at a y/n question), it returns early with running=true, lastLine and looksLikePrompt so you can answer, ask the person (request_human_input), or keep waiting (wait_for_command)."},
		func(ctx context.Context, req *mcp.CallToolRequest, in runIn) (*mcp.CallToolResult, app.RunResult, error) {
			var out app.RunResult
			err := who(req).Call(ctx, "run", control.RunParams{Session: in.Session, Command: in.Command, TimeoutSeconds: in.TimeoutSeconds, IdleSeconds: in.IdleSeconds, DelayMs: in.DelayMs}, &out)
			return nil, out, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "wait_for_command", Description: "Wait for the command running in a session to finish and return its output and exit code. Use it after run_command returned running=true or after request_human_input."},
		func(ctx context.Context, req *mcp.CallToolRequest, in waitIn) (*mcp.CallToolResult, app.RunResult, error) {
			var out app.RunResult
			err := who(req).Call(ctx, "wait", control.WaitParams{Session: in.Session, TimeoutSeconds: in.TimeoutSeconds, IdleSeconds: in.IdleSeconds}, &out)
			return nil, out, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "request_human_input", Description: "Ask the person watching to act in a session: answer a prompt, pick from a menu, approve something, or type something you must not or cannot type. The session is highlighted for them; this waits until they hand back to you, the command finishes, or the timeout passes, and returns what happened in the terminal meanwhile."},
		func(ctx context.Context, req *mcp.CallToolRequest, in askIn) (*mcp.CallToolResult, app.HumanResult, error) {
			var out app.HumanResult
			err := who(req).Call(ctx, "ask", control.AskParams{Session: in.Session, Reason: in.Reason, TimeoutSeconds: in.TimeoutSeconds}, &out)
			return nil, out, err
		})
	mcp.AddTool(s, &mcp.Tool{Name: "close_session", Description: "Close a session this agent created."},
		func(ctx context.Context, req *mcp.CallToolRequest, in sessionIn) (*mcp.CallToolResult, okOut, error) {
			err := who(req).Call(ctx, "close", control.SessionParams{Session: in.Session}, nil)
			return nil, okOut{OK: err == nil}, err
		})

	return s.Run(ctx, &mcp.StdioTransport{})
}

// actorName names the agent: TURTTYL_AGENT_NAME if set, else the MCP
// client's own name (for example "claude-code"). The app cleans and limits it.
func actorName(req *mcp.CallToolRequest) string {
	if n := os.Getenv("TURTTYL_AGENT_NAME"); n != "" {
		return n
	}
	if req != nil && req.Session != nil {
		if p := req.Session.InitializeParams(); p != nil && p.ClientInfo != nil && p.ClientInfo.Name != "" {
			return p.ClientInfo.Name
		}
	}
	return "agent"
}
