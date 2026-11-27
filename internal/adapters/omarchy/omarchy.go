// Package omarchy reads the desktop look of Omarchy (the Arch + Hyprland setup), so the
// window can match it: the active theme's colours, light or dark mode, the terminal
// font and the shared text size.
//
// Omarchy 4 ("Quattro") keeps the active theme in
// $XDG_STATE_HOME/omarchy/current/theme/colors.toml, the text size in the [font]
// section of $XDG_CONFIG_HOME/omarchy/shell.toml, and the terminal font in foot's
// foot.ini. All three are optional; on a machine without Omarchy, Load returns nil.
//
// Everything read here ends up in CSS, so values are strictly validated: colours must
// be #rrggbb or #rrggbbaa, and the font name may not contain quotes or CSS syntax.
package omarchy

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Theme is the desktop look the window follows.
type Theme struct {
	Name       string            `json:"name"`
	Mode       string            `json:"mode"`                 // "dark" or "light"
	Colors     map[string]string `json:"colors"`               // colors.toml keys → #rrggbb[aa]
	FontFamily string            `json:"fontFamily,omitempty"` // the terminal font, if set
	FontPx     int               `json:"fontPx,omitempty"`     // the shared text size, 9–20
}

// Source says where Omarchy keeps its files.
type Source struct {
	StateDir  string // usually ~/.local/state
	ConfigDir string // usually ~/.config
}

// DefaultSource follows the XDG variables, as Omarchy does.
func DefaultSource() Source {
	home, _ := os.UserHomeDir()
	s := Source{StateDir: os.Getenv("XDG_STATE_HOME"), ConfigDir: os.Getenv("XDG_CONFIG_HOME")}
	if s.StateDir == "" {
		s.StateDir = filepath.Join(home, ".local", "state")
	}
	if s.ConfigDir == "" {
		s.ConfigDir = filepath.Join(home, ".config")
	}
	return s
}

const maxFile = 64 << 10 // theme files are tiny; refuse anything larger

var (
	colourRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)
	keyRE    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	fontRE   = regexp.MustCompile(`^[A-Za-z0-9 ._+-]{1,64}$`)
)

func (s Source) themeDir() string { return filepath.Join(s.StateDir, "omarchy", "current", "theme") }

// Load reads the current look. It returns nil, nil when Omarchy isn't installed.
func (s Source) Load() (*Theme, error) {
	raw, err := readSmall(filepath.Join(s.themeDir(), "colors.toml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t := &Theme{Mode: "dark", Colors: map[string]string{}}
	for k, v := range parse(raw)[""] {
		switch {
		case k == "mode":
			if v == "light" {
				t.Mode = "light"
			}
		case keyRE.MatchString(k) && colourRE.MatchString(v):
			t.Colors[k] = strings.ToLower(v)
		}
	}
	// Older themes mark light mode with an empty light.mode file.
	if _, err := os.Stat(filepath.Join(s.themeDir(), "light.mode")); err == nil {
		t.Mode = "light"
	}
	if name, err := readSmall(filepath.Join(s.StateDir, "omarchy", "current", "theme.name")); err == nil {
		if n := strings.TrimSpace(string(name)); fontRE.MatchString(n) {
			t.Name = n
		}
	}

	var footPt float64
	if foot, err := readSmall(filepath.Join(s.ConfigDir, "foot", "foot.ini")); err == nil {
		t.FontFamily, footPt = footFont(parse(foot)["main"]["font"])
	}
	if shell, err := readSmall(filepath.Join(s.ConfigDir, "omarchy", "shell.toml")); err == nil {
		f := parse(shell)["font"]
		for _, k := range []string{"base-size", "base_size", "size"} {
			if n, err := strconv.Atoi(f[k]); err == nil {
				t.FontPx = n
				break
			}
		}
	}
	if t.FontPx == 0 && footPt > 0 {
		t.FontPx = int(footPt*12/9 + 0.5) // Omarchy's own rule: terminal pt = px × 9/12
	}
	if t.FontPx < 9 || t.FontPx > 20 {
		t.FontPx = 0
	}
	return t, nil
}

// Watch calls fn with the new look whenever the theme, text size or font changes.
// Omarchy swaps the theme folder in place, so polling file stamps is the reliable way.
func (s Source) Watch(ctx context.Context, every time.Duration, fn func(*Theme)) {
	paths := []string{
		filepath.Join(s.themeDir(), "colors.toml"),
		filepath.Join(s.themeDir(), "light.mode"),
		filepath.Join(s.ConfigDir, "omarchy", "shell.toml"),
		filepath.Join(s.ConfigDir, "foot", "foot.ini"),
	}
	last := stamp(paths)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if now := stamp(paths); now != last {
				last = now
				if th, err := s.Load(); err == nil {
					fn(th)
				}
			}
		}
	}
}

func stamp(paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil {
			b.WriteString(strconv.FormatInt(fi.ModTime().UnixNano(), 36))
			b.WriteString(strconv.FormatInt(fi.Size(), 36))
		}
		b.WriteByte('|')
	}
	return b.String()
}

func readSmall(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxFile))
}

// parse reads the flat TOML and INI that Omarchy and foot use: [section] headers and
// key = value lines. Values may be quoted. It is enough for these files, not general TOML.
func parse(data []byte) map[string]map[string]string {
	out := map[string]map[string]string{"": {}}
	sec := ""
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && strings.HasSuffix(line, "]") {
			sec = strings.TrimSpace(line[1 : len(line)-1])
			if out[sec] == nil {
				out[sec] = map[string]string{}
			}
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') {
			if end := strings.IndexByte(v[1:], v[0]); end >= 0 {
				v = v[1 : end+1]
			}
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out[sec][strings.TrimSpace(k)] = v
	}
	return out
}

// footFont splits foot's "Family:size=9" into the family and the point size.
func footFont(v string) (string, float64) {
	first, _, _ := strings.Cut(v, ",") // foot allows fallback fonts after a comma
	parts := strings.Split(first, ":")
	family := strings.TrimSpace(parts[0])
	if !fontRE.MatchString(family) {
		family = ""
	}
	var pt float64
	for _, p := range parts[1:] {
		if n, ok := strings.CutPrefix(strings.TrimSpace(p), "size="); ok {
			pt, _ = strconv.ParseFloat(n, 64)
		}
	}
	return family, pt
}
