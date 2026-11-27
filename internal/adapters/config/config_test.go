package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingFileGivesDefaultsAndWritesThem(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if f.Elevation.Enabled {
		t.Fatal("elevation must be off by default")
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal("default config not written")
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
}

func TestRejectsUnknownFieldsAndBadValues(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"typo":  `{"elevation":{"enabeld":true}}`,
		"delay": `{"typing_delay_ms":999,"scrollback_kb":2048,"elevation":{"idle_timeout_minutes":30}}`,
		"big":   string(make([]byte, maxConfigBytes+10)),
	} {
		p := filepath.Join(dir, name+".json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := Load(p)
		if err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if f.Elevation.Enabled {
			t.Fatalf("%s: elevation enabled by a bad file", name)
		}
	}
}

func TestEnableElevation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	body := `{"elevation":{"enabled":true,"idle_timeout_minutes":5},"typing_delay_ms":10,"scrollback_kb":1024}`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c := f.AppConfig()
	if !c.ElevationEnabled || c.ElevationIdle.Minutes() != 5 || c.ScrollbackBytes != 1024*1024 || c.TypingDelayMs != 10 {
		t.Fatalf("%+v", c)
	}
}
