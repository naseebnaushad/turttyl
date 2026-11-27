// Package audit writes the audit log as JSON lines in a file only this user can read.
package audit

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"unicode/utf8"

	"github.com/naseebnaushad/turttyl/internal/app"
)

const (
	maxDetail   = 8 * 1024
	rotateBytes = 10 << 20 // keep one previous file of up to 10 MiB
)

// File is an append-only JSONL audit log.
type File struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
	log  *slog.Logger
}

// Open opens (or creates) the log with mode 0600.
func Open(path string, log *slog.Logger) (*File, error) {
	a := &File{path: path, log: log}
	return a, a.open()
}

func (a *File) open() error {
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = f.Chmod(0o600)
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	a.f, a.size = f, st.Size()
	return nil
}

// Record appends one event. Failures are logged, never returned: auditing must not
// break the terminal.
func (a *File) Record(e app.AuditEvent) {
	if len(e.Detail) > maxDetail {
		cut := maxDetail
		for cut > 0 && !utf8.RuneStart(e.Detail[cut]) {
			cut--
		}
		e.Detail = e.Detail[:cut] + "…[truncated]"
	}
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	line = append(line, '\n')
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return
	}
	if a.size+int64(len(line)) > rotateBytes {
		a.f.Close()
		_ = os.Rename(a.path, a.path+".1")
		if err := a.open(); err != nil {
			a.f = nil
			a.log.Error("audit log reopen failed", "err", err)
			return
		}
	}
	n, err := a.f.Write(line)
	a.size += int64(n)
	if err != nil {
		a.log.Error("audit log write failed", "err", err)
	}
}

// Close closes the file.
func (a *File) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return nil
	}
	err := a.f.Close()
	a.f = nil
	return err
}
