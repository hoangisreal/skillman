package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavePreferenceKeepsCorruptFile(t *testing.T) {
	t.Setenv("SKILLMAN_STATE", t.TempDir())
	Configure()
	must(t, SavePreference("language", "vi"))
	must(t, SavePreference("mouse", false))
	if m := LoadMap(SettingsPath()); m["language"] != "vi" || m["mouse"] != false {
		t.Fatalf("saved: %v", m)
	}
	broken := []byte("{\"language\": \"vi\",}")
	must(t, os.WriteFile(SettingsPath(), broken, 0o644))
	if err := SavePreference("mouse", true); err == nil {
		t.Fatal("saved over an invalid settings file")
	}
	if b, _ := os.ReadFile(SettingsPath()); string(b) != string(broken) {
		t.Fatalf("settings file changed: %q", b)
	}
	if _, err := os.Stat(filepath.Dir(SettingsPath())); err != nil {
		t.Fatal(err)
	}
}
