// Package config finds the app's data folder and loads config.json.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/naseebnaushad/turttyl/internal/app"
)

// Paths are the files the app uses, all inside one folder.
type Paths struct {
	Home     string // folder, mode 0700
	Config   string // config.json
	Socket   string // control.sock: agents (MCP) connect here
	Audit    string // audit.jsonl
	ShellDir string // generated shell integration files
}

// Resolve returns the paths, using TURTTYL_HOME if set, else ~/.turttyl.
func Resolve() (Paths, error) {
	home := os.Getenv("TURTTYL_HOME")
	if home == "" {
		uh, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		home = filepath.Join(uh, ".turttyl")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		Home:     home,
		Config:   filepath.Join(home, "config.json"),
		Socket:   filepath.Join(home, "control.sock"),
		Audit:    filepath.Join(home, "audit.jsonl"),
		ShellDir: filepath.Join(home, "shell"),
	}, nil
}

// EnsureHome creates the folder with mode 0700, tightening it if it is looser.
func (p Paths) EnsureHome() error {
	if err := os.MkdirAll(p.Home, 0o700); err != nil {
		return err
	}
	return os.Chmod(p.Home, 0o700)
}

// File is the JSON config file.
type File struct {
	Elevation struct {
		// Enabled turns on elevated (sudo) sessions. Off by default.
		Enabled bool `json:"enabled"`
		// IdleTimeoutMinutes closes an elevated session after this long with no activity.
		IdleTimeoutMinutes int `json:"idle_timeout_minutes"`
	} `json:"elevation"`
	// TypingDelayMs is the default pause between characters when agents type (0–200).
	TypingDelayMs int `json:"typing_delay_ms"`
	// ScrollbackKB is how much output each session keeps (64–65536 KiB).
	ScrollbackKB int `json:"scrollback_kb"`
	// DefaultCwd is where new sessions start. Empty means the home folder.
	DefaultCwd string `json:"default_cwd"`
}

// Defaults returns the default config.
func Defaults() File {
	var f File
	f.Elevation.Enabled = false
	f.Elevation.IdleTimeoutMinutes = 30
	f.TypingDelayMs = 20
	f.ScrollbackKB = 2048
	return f
}

const maxConfigBytes = 64 * 1024

// Load reads config.json. A missing file gives the defaults and writes them out so
// the file is easy to find and edit.
func Load(path string) (File, error) {
	f := Defaults()
	fh, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		data, _ := json.MarshalIndent(f, "", "  ")
		_ = writeAtomic(path, append(data, '\n'), 0o600)
		return f, nil
	}
	if err != nil {
		return f, err
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, maxConfigBytes+1))
	if err != nil {
		return f, err
	}
	if len(data) > maxConfigBytes {
		return f, fmt.Errorf("%s is larger than %d bytes", path, maxConfigBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Defaults(), fmt.Errorf("%s: %w", path, err)
	}
	return f, f.validate()
}

func (f File) validate() error {
	switch {
	case f.TypingDelayMs < 0 || f.TypingDelayMs > 200:
		return errors.New("typing_delay_ms must be between 0 and 200")
	case f.ScrollbackKB < 64 || f.ScrollbackKB > 65536:
		return errors.New("scrollback_kb must be between 64 and 65536")
	case f.Elevation.IdleTimeoutMinutes < 1 || f.Elevation.IdleTimeoutMinutes > 24*60:
		return errors.New("elevation.idle_timeout_minutes must be between 1 and 1440")
	}
	return nil
}

// AppConfig converts the file into the core's config.
func (f File) AppConfig() app.Config {
	return app.Config{
		ScrollbackBytes:  f.ScrollbackKB * 1024,
		TypingDelayMs:    f.TypingDelayMs,
		ElevationEnabled: f.Elevation.Enabled,
		ElevationIdle:    time.Duration(f.Elevation.IdleTimeoutMinutes) * time.Minute,
		DefaultCwd:       f.DefaultCwd,
	}
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
