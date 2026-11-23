package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/hoangisreal/skillman/internal/core"
)

func settingsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SKILL_LIBRARY", filepath.Join(root, "library"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	core.Configure()
	mkskill(t, core.SKILLS, "example", "Fixture description", "# User-authored text")
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestLanguageSettings(t *testing.T) {
	project := settingsFixture(t)
	path := filepath.Join(core.STATE, "settings.json")
	d := newDriver(t, project)
	show := func(d *driver, want string) {
		t.Helper()
		screen := ansi.Strip(d.screen())
		if !strings.Contains(screen, want) {
			t.Fatalf("missing %q:\n%s", want, screen)
		}
	}
	show(d, "9 Settings")
	d.press("ctrl+p")
	d.typeText("settings")
	d.press("enter")
	show(d, "Language")
	show(d, "Tiếng Việt")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("opening Settings should not create a preference file: %v", err)
	}
	d.press("down", "enter")
	show(d, "9 Cài đặt")
	show(d, "✓ Tiếng Việt")
	d.press("1", "i")
	show(d, "Xác nhận")
	show(d, "1 sẵn sàng")
	d.send(tea.WindowSizeMsg{Width: 20, Height: 6})
	show(d, "Có y")
	show(d, "Không n")
	confirm := d.m.modals[0].(*confirmModal)
	d.click(confirm.btnNo.x+1, confirm.btnNo.y)
	if len(d.m.modals) != 0 || len(installed(project)) != 0 {
		t.Fatal("the localized cancel button should close the preview without installing")
	}
	d.send(tea.WindowSizeMsg{Width: 160, Height: 45})
	d.press("enter")
	show(d, "User-authored text")
	d.press("esc")
	if prefs := core.LoadMap(path); prefs["language"] != "vi" {
		t.Fatalf("saved preference = %v", prefs)
	}
	d = newDriver(t, project)
	show(d, "Thư viện")
	d.press("ctrl+p")
	d.typeText("cài đặt")
	d.press("enter")
	show(d, "Ngôn ngữ")
	d.send(tea.WindowSizeMsg{Width: 80, Height: 24})
	show(d, "9 Cài đặt")
	d.press("9", "up", "enter")
	show(d, "9 Settings")
	d = newDriver(t, project)
	show(d, "Library")
	if prefs := core.LoadMap(path); prefs["language"] != "en" {
		t.Fatalf("updated preference = %v", prefs)
	}
}

func TestLanguageSettingsFallback(t *testing.T) {
	project := settingsFixture(t)
	path := filepath.Join(core.STATE, "settings.json")
	if err := core.WriteJSON(path, map[string]string{"language": "unknown"}); err != nil {
		t.Fatal(err)
	}
	d := newDriver(t, project)
	if !strings.Contains(ansi.Strip(d.screen()), "9 Settings") {
		t.Fatal("an unsupported preference should open the app in English")
	}
	// A directory at the settings path prevents the atomic rename on all supported platforms.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	d.press("9", "down", "enter")
	screen := ansi.Strip(d.screen())
	if !strings.Contains(screen, "Could not save language") || !strings.Contains(screen, "✓ English") {
		t.Fatalf("failed save must leave the language unchanged and show an error:\n%s", screen)
	}
}
