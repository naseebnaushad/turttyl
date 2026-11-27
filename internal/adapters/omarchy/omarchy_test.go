package omarchy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) Source {
	s := Source{StateDir: t.TempDir(), ConfigDir: t.TempDir()}
	write(t, filepath.Join(s.themeDir(), "colors.toml"), `
# synthetic theme
mode = "dark"
accent = "#7AA2F7"
background = "#1a1b26"
foreground = "#a9b1d6"   # trailing comment
color1 = '#f7768e'
bad = "red; } body { display:none"
Upper = "#ffffff"
`)
	write(t, filepath.Join(s.StateDir, "omarchy", "current", "theme.name"), "tokyo-night\n")
	write(t, filepath.Join(s.ConfigDir, "foot", "foot.ini"), "[main]\nfont=JetBrainsMono Nerd Font:size=12,Noto Color Emoji:size=12\n")
	return s
}

func TestNotInstalled(t *testing.T) {
	th, err := Source{StateDir: t.TempDir(), ConfigDir: t.TempDir()}.Load()
	if th != nil || err != nil {
		t.Fatalf("want nil, nil; got %+v, %v", th, err)
	}
}

func TestLoad(t *testing.T) {
	th, err := fixture(t).Load()
	if err != nil || th == nil {
		t.Fatal(th, err)
	}
	if th.Name != "tokyo-night" || th.Mode != "dark" {
		t.Errorf("name/mode: %q %q", th.Name, th.Mode)
	}
	want := map[string]string{"accent": "#7aa2f7", "background": "#1a1b26", "foreground": "#a9b1d6", "color1": "#f7768e"}
	if len(th.Colors) != len(want) {
		t.Errorf("colours: %v (bad values and keys must be dropped)", th.Colors)
	}
	for k, v := range want {
		if th.Colors[k] != v {
			t.Errorf("%s = %q, want %q", k, th.Colors[k], v)
		}
	}
	if th.FontFamily != "JetBrainsMono Nerd Font" {
		t.Errorf("font %q", th.FontFamily)
	}
	if th.FontPx != 16 { // 12pt in foot → 16px by Omarchy's px × 9/12 rule
		t.Errorf("font px %d", th.FontPx)
	}
}

func TestShellSizeWinsAndLightMode(t *testing.T) {
	s := fixture(t)
	write(t, filepath.Join(s.ConfigDir, "omarchy", "shell.toml"), "[bar]\nsize = 30\n[font]\nbase-size = 14\n")
	write(t, filepath.Join(s.themeDir(), "light.mode"), "")
	th, _ := s.Load()
	if th.FontPx != 14 || th.Mode != "light" {
		t.Fatalf("got px %d mode %s", th.FontPx, th.Mode)
	}
	write(t, filepath.Join(s.ConfigDir, "omarchy", "shell.toml"), "[font]\nbase-size = 99\n")
	if th, _ := s.Load(); th.FontPx != 0 {
		t.Fatalf("out-of-range size must be ignored, got %d", th.FontPx)
	}
}

func TestUnsafeFontRejected(t *testing.T) {
	s := fixture(t)
	write(t, filepath.Join(s.ConfigDir, "foot", "foot.ini"), "[main]\nfont=Evil\"; }:size=9\n")
	if th, _ := s.Load(); th.FontFamily != "" {
		t.Fatalf("font %q", th.FontFamily)
	}
}

func TestWatch(t *testing.T) {
	s := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan *Theme, 1)
	go s.Watch(ctx, 20*time.Millisecond, func(th *Theme) { got <- th })
	time.Sleep(60 * time.Millisecond)
	write(t, filepath.Join(s.themeDir(), "colors.toml"), "mode = \"light\"\naccent = \"#123456\"\n")
	select {
	case th := <-got:
		if th.Mode != "light" || th.Colors["accent"] != "#123456" {
			t.Fatalf("%+v", th)
		}
	case <-ctx.Done():
		t.Fatal("no change reported")
	}
}
