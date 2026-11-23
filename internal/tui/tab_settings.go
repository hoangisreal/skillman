package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
)

func loadLanguage(prefs map[string]any) language {
	if prefs["language"] == string(vietnamese) {
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

const (
	setBudget = iota + 2
	setMouse
	setLayout
	setClaude
)

type settingsTab struct {
	tbl          *table
	mode         string
	budget       int
	budgetSource string
}

func newSettingsTab(m *Model) *settingsTab {
	t := &settingsTab{tbl: newTable(column{width: 1}, column{title: "Language and preferences", flex: 1})}
	t.tbl.n = setClaude + 1
	t.tbl.focused = true
	for i, entry := range languages {
		if entry.id == m.lang {
			t.tbl.cursor = i
			break
		}
	}
	t.tbl.cell = func(i, c int) string {
		if i >= len(languages) {
			if c == 0 {
				return ""
			}
			switch i {
			case setBudget:
				return m.lang.format("Description token budget · %d (%s)", t.budget, m.lang.text(t.budgetSource))
			case setMouse:
				return m.lang.text("Mouse control") + " · " + filterState(m.lang, m.mouseEnabled)
			case setLayout:
				return m.lang.text("Reset Library layout")
			case setClaude:
				return m.lang.text("Claude installation mode") + " · " + m.lang.text(t.mode)
			}
			return ""
		}
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

func (t *settingsTab) title() string { return "Settings" }
func (t *settingsTab) typing() bool  { return false }
func (t *settingsTab) refresh(m *Model) {
	t.budget, t.budgetSource = core.DescriptionBudget()
	var err error
	t.mode, err = core.ClaudeMode(m.project)
	if err != nil {
		t.mode = "unavailable"
	}
}

func (t *settingsTab) apply(m *Model) tea.Cmd {
	switch t.tbl.cursor {
	case setBudget:
		budget, source := core.DescriptionBudget()
		if source == "SKILL_TOKEN_BUDGET" {
			return m.notify(m.lang.text("Change SKILL_TOKEN_BUDGET in the shell to override this value"), false)
		}
		m.push(ask(m.lang.text("Description token budget"), strconv.Itoa(budget), "2000", func(m *Model, value string) tea.Cmd {
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return m.notify(m.lang.text("Enter a positive whole number"), true)
			}
			return m.savePreference("token_budget", n, func() {
				t.refresh(m)
				m.tabs[3].refresh(m)
			})
		}))
		return nil
	case setMouse:
		return m.savePreference("mouse", !m.mouseEnabled, func() { m.mouseEnabled = !m.mouseEnabled })
	case setLayout:
		return m.savePreference("library_split", defaultLibrarySplit, func() { m.tabs[0].(*libraryTab).split = defaultLibrarySplit })
	case setClaude:
		return t.chooseMode(m)
	}
	entry := languages[t.tbl.cursor]
	if err := core.SavePreference("language", string(entry.id)); err != nil {
		return m.notify(m.lang.format("Could not save language: %v", err), true)
	}
	m.lang = entry.id
	return m.notify(m.lang.format("Language set to %s", entry.name), false)
}

func (t *settingsTab) actions(m *Model) []action {
	return []action{{key: "enter", label: "apply setting", run: t.apply}}
}

func (t *settingsTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return nil, t.tbl.keyNav(msg.String())
}

func (t *settingsTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	cmd, _ := listMouse(m, t.tbl, msg, func() tea.Cmd { return t.apply(m) }, false)
	return cmd
}

func (t *settingsTab) view(m *Model, r rect) []string {
	head := m.lang.text("Project") + " · " + m.project
	return append([]string{stDim.Render(head)}, t.tbl.render(m.lang, rect{r.x, r.y + 1, r.w, r.h - 1})...)
}

func preferenceBool(prefs map[string]any, key string, fallback bool) bool {
	if value, ok := prefs[key].(bool); ok {
		return value
	}
	return fallback
}

func filterState(lang language, enabled bool) string {
	if enabled {
		return lang.text("on")
	}
	return lang.text("off")
}

func (m *Model) savePreference(key string, value any, apply func()) tea.Cmd {
	if err := core.SavePreference(key, value); err != nil {
		return m.notify(m.lang.format("Could not save setting: %v", err), true)
	}
	apply()
	return m.notify(m.lang.text("Setting saved"), false)
}

func (t *settingsTab) chooseMode(m *Model) tea.Cmd {
	project := m.project
	copyLabel, linkLabel := m.lang.text("Managed copies"), m.lang.text("Relative symlinks")
	m.push(pick(m.lang.text("Claude installation mode"), []option{{copyLabel, ".claude/skills"}, {linkLabel, ".claude/skills"}}, func(m *Model, value string) tea.Cmd {
		mode := "copy"
		if value == linkLabel {
			mode = "symlink"
		}
		lang := m.lang
		return m.async(lang.text("checking Claude paths …"), func(func(string)) tea.Msg {
			paths, err := core.ConvertClaudeMode(project, mode, true)
			if err != nil {
				return doneMsg{note: err.Error(), isErr: true}
			}
			return doneMsg{then: func(m *Model) tea.Cmd {
				if m.project != project || m.tab() != t {
					return nil
				}
				body := lang.format("Set Claude installation mode to %s?\n%s\n.agents/%s", lang.text(mode), project, core.Manifest)
				if len(paths) > 0 {
					body += "\n\n" + strings.Join(paths, "\n")
				}
				m.push(confirm(body, true, func(m *Model) tea.Cmd {
					return m.async(lang.text("converting Claude paths …"), func(func(string)) tea.Msg {
						if _, err := core.ConvertClaudeMode(project, mode, false); err != nil {
							return doneMsg{note: err.Error(), isErr: true, reload: true}
						}
						results := core.Verify(project)
						for _, result := range results {
							if !result.OK {
								return doneMsg{text: fmtResults(lang, results), title: "verify (CI frozen check)", reload: true}
							}
						}
						return doneMsg{note: lang.text("Claude mode saved and verified"), reload: true}
					})
				}))
				return nil
			}}
		})
	}))
	return nil
}
