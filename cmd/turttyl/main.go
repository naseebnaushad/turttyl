// Command turttyl is the composition root. With no arguments it opens the
// desktop app; `turttyl mcp` runs the MCP server that agents start.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/naseebnaushad/turttyl/frontend"
	"github.com/naseebnaushad/turttyl/internal/adapters/audit"
	"github.com/naseebnaushad/turttyl/internal/adapters/config"
	"github.com/naseebnaushad/turttyl/internal/adapters/control"
	"github.com/naseebnaushad/turttyl/internal/adapters/desktop"
	"github.com/naseebnaushad/turttyl/internal/adapters/mcpserver"
	"github.com/naseebnaushad/turttyl/internal/adapters/ptyshell"
	"github.com/naseebnaushad/turttyl/internal/app"
)

// version is stamped at build time: -ldflags "-X main.version=$(version.sh label)".
var version = "0.1.0-alpha.dev"

const usage = `Turttyl %s: a terminal that people and AI agents share.

Usage:
  turttyl            open the app
  turttyl mcp        run the MCP server (agents start this; the app must be open)
  turttyl version    print the version

Add it to Claude Code:
  claude mcp add turttyl -- %s mcp
`

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	paths, err := config.Resolve()
	if err != nil {
		fail(log, "cannot find the data folder", err)
	}
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "":
		runApp(log, paths)
	case "mcp":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := mcpserver.Run(ctx, paths.Socket, version); err != nil && !errors.Is(err, context.Canceled) {
			fail(log, "mcp server stopped", err)
		}
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		exe, _ := os.Executable()
		fmt.Printf(usage, version, exe)
	default:
		// macOS may pass -psn_… when launched from Finder; treat unknown as "open app".
		if len(cmd) > 4 && cmd[:4] == "-psn" {
			runApp(log, paths)
			return
		}
		exe, _ := os.Executable()
		fmt.Fprintf(os.Stderr, usage, version, exe)
		os.Exit(2)
	}
}

func runApp(log *slog.Logger, paths config.Paths) {
	if err := paths.EnsureHome(); err != nil {
		fail(log, "cannot create the data folder", err)
	}
	cfgFile, err := config.Load(paths.Config)
	if err != nil {
		fail(log, "config file is invalid", err)
	}
	auditLog, err := audit.Open(paths.Audit, log)
	if err != nil {
		fail(log, "cannot open the audit log", err)
	}
	defer auditLog.Close()
	spawner, err := ptyshell.New(paths.ShellDir)
	if err != nil {
		fail(log, "cannot set up shell integration", err)
	}

	var wailsApp atomic.Pointer[application.App] // set once the window exists
	bridge := desktop.NewBridge(func(name string, data any) {
		if a := wailsApp.Load(); a != nil {
			a.Event.Emit(name, data)
		}
	})
	defer bridge.Close()
	mgr := app.NewManager(cfgFile.AppConfig(), spawner, auditLog, bridge)
	bridge.SetManager(mgr)
	defer mgr.Shutdown()

	ctl, err := control.Listen(paths.Socket, mgr, log)
	if err != nil {
		fail(log, "cannot open the agent socket", err)
	}
	defer ctl.Close()

	wails := application.New(application.Options{
		Name:        "Turttyl",
		Description: "A terminal that people and AI agents share",
		Services: []application.Service{
			application.NewService(&desktop.TerminalService{M: mgr, Version: version}),
		},
		Assets: application.AssetOptions{Handler: application.BundledAssetFileServer(frontend.Dist())},
		Mac:    application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		OnShutdown: func() {
			mgr.Shutdown()
		},
	})
	win := wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Turttyl",
		Width:     1280,
		Height:    820,
		MinWidth:  760,
		MinHeight: 480,
		// Transparent so the macOS Liquid Glass backdrop shows behind the sidebar.
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropLiquidGlass,
			TitleBar: application.MacTitleBarHiddenInsetUnified,
			LiquidGlass: application.MacLiquidGlass{
				Style:    application.LiquidGlassStyleAutomatic,
				Material: application.NSVisualEffectMaterialAuto,
			},
			// Terminal programs (vim, less, fzf) need Escape even in full screen.
			DisableEscapeExitsFullscreen: true,
		},
	})
	bridge.Alert = func() { win.Flash(true) }
	wailsApp.Store(wails)
	if cfgFile.Elevation.Enabled {
		log.Warn("elevated sessions are enabled in the config")
	}
	if err := wails.Run(); err != nil {
		fail(log, "app stopped", err)
	}
}

func fail(log *slog.Logger, msg string, err error) {
	log.Error(msg, "err", err)
	fmt.Fprintf(os.Stderr, "turttyl: %s: %v\n", msg, err)
	os.Exit(1)
}
