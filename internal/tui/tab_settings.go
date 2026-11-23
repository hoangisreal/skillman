package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
)

func settingsPath() string { return filepath.Join(core.STATE, "settings.json") }

func loadLanguage() language {
	if core.LoadMap(settingsPath())["language"] == string(vietnamese) {
		return vietnamese
	}
	return english
}

var languages = [...]struct {
	id   language
	name string
}{
	{english, "English"},
	{vietnamese, "Tiếng Việt"},
}

type settingsTab struct{ tbl *table }

func newSettingsTab(m *Model) *settingsTab {
	t := &settingsTab{tbl: newTable(column{width: 1}, column{title: "Language", flex: 1})}
	t.tbl.n = len(languages)
	t.tbl.focused = true
	for i, entry := range languages {
		if entry.id == m.lang {
			t.tbl.cursor = i
			break
		}
	}
	t.tbl.cell = func(i, c int) string {
		entry := languages[i]
		if c == 1 {
			return entry.name
		}
		if entry.id == m.lang {
			return stOK.Render("✓")
		}
		return ""
	}
	return t
}

func (t *settingsTab) title() string  { return "Settings" }
func (t *settingsTab) typing() bool   { return false }
func (t *settingsTab) refresh(*Model) {}

func (t *settingsTab) apply(m *Model) tea.Cmd {
	entry := languages[t.tbl.cursor]
	path := settingsPath()
	prefs := core.LoadMap(path)
	prefs["language"] = string(entry.id)
	if err := core.WriteJSON(path, prefs); err != nil {
		return m.notify(m.lang.format("Could not save language: %v", err), true)
	}
	m.lang = entry.id
	return m.notify(m.lang.format("Language set to %s", entry.name), false)
}

func (t *settingsTab) actions(m *Model) []action {
	return []action{{key: "enter", label: "apply language", run: t.apply}}
}

func (t *settingsTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return nil, t.tbl.keyNav(msg.String())
}

func (t *settingsTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	cmd, _ := listMouse(m, t.tbl, msg, func() tea.Cmd { return t.apply(m) }, false)
	return cmd
}

func (t *settingsTab) view(m *Model, r rect) []string { return t.tbl.render(m.lang, r) }
