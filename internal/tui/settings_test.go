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

func TestSettingsAndExportWorkflow(t *testing.T) {
	project := settingsFixture(t)
	t.Setenv("SKILL_TOKEN_BUDGET", "")
	d := newDriver(t, project)
	d.screen()
	lib := d.m.tabs[0].(*libraryTab)
	x, y := lib.divX, lib.q.area.y+2
	d.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	d.send(tea.MouseMotionMsg{X: x - 20, Y: y, Button: tea.MouseLeft})
	if core.LoadMap(core.SettingsPath())["library_split"] != nil || lib.split == defaultLibrarySplit {
		t.Fatal("drag should resize the Library before persisting")
	}
	d.send(tea.MouseReleaseMsg{X: x - 20, Y: y, Button: tea.MouseLeft})
	split := lib.split
	d = newDriver(t, project)
	if d.m.tabs[0].(*libraryTab).split != split {
		t.Fatal("reopening Skillman should restore the Library divider")
	}
	d.press("9", "down", "down", "enter", "ctrl+u")
	d.typeText("120")
	d.press("enter", "down", "enter")
	if d.m.View().MouseMode != tea.MouseModeNone || !strings.Contains(ansi.Strip(d.screen()), "120 (personal settings)") {
		t.Fatal("Settings should update the active budget and mouse mode")
	}
	d.press("down", "enter", "down", "enter", "enter", "y")
	if d.m.tabs[0].(*libraryTab).split != defaultLibrarySplit {
		t.Fatal("Settings should reset the Library divider")
	}
	if mode, err := core.ClaudeMode(project); err != nil || mode != "copy" {
		t.Fatalf("Claude mode %q: %v", mode, err)
	}
	d.press("3", "v", "5", "f")
	d = newDriver(t, project)
	if d.m.mouseEnabled || !d.m.tabs[2].(*packsTab).showVendor || d.m.tabs[4].(*updatesTab).onlyChanges {
		t.Fatal("mouse and filter preferences did not persist")
	}
	d.press("i", "y", "4", "E")
	if preview, ok := d.m.modals[0].(*mdModal); !ok || !strings.Contains(preview.raw, ".agents/skills/example/SKILL.md") {
		t.Fatal("missing project skill export preview")
	}
	d.press("enter", "enter")
	file := filepath.Join(project, "project-skills.md")
	if body, err := os.ReadFile(file); err != nil || !strings.Contains(string(body), "Fixture description") {
		t.Fatalf("markdown export %q: %v", body, err)
	}
	d.press("E", "enter", "enter")
	if _, ok := d.m.modals[0].(*confirmModal); !ok {
		t.Fatal("replacing the export should require confirmation")
	}
	d.press("esc")
}
