package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
)

func fmtResults(rs []core.Result) string {
	if len(rs) == 0 {
		return "(nothing to do)"
	}
	var b strings.Builder
	for _, r := range rs {
		if r.OK {
			b.WriteString("✓ ")
		} else {
			b.WriteString("✗ ")
		}
		b.WriteString(r.Name + ": " + r.Message + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func summarize(rs []core.Result, verb string) (string, bool) {
	bad := 0
	for _, r := range rs {
		if !r.OK {
			bad++
		}
	}
	if bad > 0 {
		var first string
		for _, r := range rs {
			if !r.OK {
				first = r.Name + ": " + r.Message
				break
			}
		}
		return fmt.Sprintf("%d %s, %d failed — %s", len(rs)-bad, verb, bad, first), true
	}
	return fmt.Sprintf("%d %s", len(rs), verb), false
}

// installItems confirms (with the token cost) and installs into the current project.
func (m *Model) installItems(items []core.Item, label string, after func()) tea.Cmd {
	if len(items) == 0 {
		return m.notify("no skills to install", true)
	}
	proj, originTab := m.project, m.tab()
	return m.async("checking install …", func(func(string)) tea.Msg {
		preview, err := core.PreviewInstall(proj, items, false)
		if err != nil {
			return doneMsg{note: err.Error(), isErr: true}
		}
		return doneMsg{then: func(m *Model) tea.Cmd {
			if m.project != proj || m.tab() != originTab {
				return nil
			}
			return m.confirmInstall(preview, items, label, after)
		}}
	})
}

func (m *Model) confirmInstall(plan core.InstallPreview, items []core.Item, label string, after func()) tea.Cmd {
	var ready, problems []string
	for _, s := range plan.Skills {
		line := fmt.Sprintf("%s · %s", s.Name, s.Reason)
		if s.Action == "blocked" || s.Action == "skipped" {
			problems = append(problems, line)
		} else {
			ready = append(ready, line)
		}
	}
	if len(ready) == 0 {
		m.push(newTextModal("No skills can be installed", strings.Join(problems, "\n")))
		return nil
	}
	msg := fmt.Sprintf("Install %s into\n%s\n\n%d ready · %d blocked or skipped\n≈%d added description tokens → %d/%d",
		label, plan.Project, len(ready), len(problems), plan.AddedTokens, plan.BudgetAfter, plan.Budget)
	if plan.BudgetAfter > plan.Budget {
		msg += "\n⚠ over the description token budget"
	}
	msg += "\n\n" + strings.Join(append(problems, ready...), "\n")
	m.push(confirm(msg, len(problems) > 0, func(m *Model) tea.Cmd {
		if after != nil {
			after()
		}
		return m.async("installing "+label+" …", func(func(string)) tea.Msg {
			rs, err := core.Install(plan.Project, items, false)
			if err != nil {
				return doneMsg{note: err.Error(), isErr: true}
			}
			s := core.SummarizeInstall(rs)
			note := fmt.Sprintf("%d installed, %d already present, %d skipped, %d failed", s.Installed, s.Present, s.Skipped, s.Failed)
			d := doneMsg{note: note, isErr: !s.OK(), reload: true}
			if d.isErr {
				d.text, d.title = fmtResults(rs), "install "+label
			}
			return d
		})
	}))
	return nil
}

func (m *Model) uninstallSkills(names []string, after func()) tea.Cmd {
	if len(names) == 0 {
		return nil
	}
	proj := m.project
	m.push(confirm(fmt.Sprintf("Remove %d skill(s) from the project?\n%s\n(moved to trash, restorable in tab 8)",
		len(names), strings.Join(names, ", ")), true, func(m *Model) tea.Cmd {
		if after != nil {
			after()
		}
		return m.async("removing …", func(func(string)) tea.Msg {
			rs, err := core.Uninstall(proj, names)
			if err != nil {
				return doneMsg{note: err.Error(), isErr: true}
			}
			note, bad := summarize(rs, "removed")
			return doneMsg{note: note, isErr: bad, reload: true}
		})
	}))
	return nil
}

func (m *Model) uninstallPack(pack string) tea.Cmd {
	proj := m.project
	m.push(confirm("Remove @"+pack+" from the project? Skills still used by another pack or added manually are kept; "+
		"others go to the trash.", true, func(m *Model) tea.Cmd {
		return m.async("removing @"+pack+" …", func(func(string)) tea.Msg {
			rs, err := core.UninstallPack(proj, pack)
			if err != nil {
				return doneMsg{note: err.Error(), isErr: true}
			}
			return doneMsg{note: "remove @" + pack + ": " + strings.ReplaceAll(fmtResults(rs), "\n", " · "), reload: true}
		})
	}))
	return nil
}

// addToPack picks a pack (or creates one) and adds names to it.
func (m *Model) addToPack(names []string, after func()) tea.Cmd {
	if len(names) == 0 {
		return nil
	}
	opts := []option{{"+ new pack…", "create a pack from these skills"}}
	for _, s := range core.PackSummaries(false) {
		opts = append(opts, option{s.Name, s.Description})
	}
	m.push(pick(fmt.Sprintf("Add %d skill(s) to pack", len(names)), opts, func(m *Model, p string) tea.Cmd {
		if p == "+ new pack…" {
			return m.newPackFrom(names, after)
		}
		pk, err := core.LoadPack(p)
		if err != nil {
			return m.notify(err.Error(), true)
		}
		for _, n := range names {
			if !core.Contains(pk.Skills, n) {
				pk.Skills = append(pk.Skills, n)
			}
		}
		if err := core.SavePack(pk); err != nil {
			return m.notify(err.Error(), true)
		}
		if after != nil {
			after()
		}
		return tea.Batch(m.notify(fmt.Sprintf("added %d skill(s) to @%s (commit: git -C %s commit packs)", len(names), p, core.LIB), false), m.reload(false))
	}))
	return nil
}

func (m *Model) newPackFrom(names []string, after func()) tea.Cmd {
	m.push(ask(fmt.Sprintf("New pack with %d skill(s) — name (kebab-case, optional group/):", len(names)), "", "my-stack",
		func(m *Model, name string) tea.Cmd {
			if name == "" {
				return nil
			}
			if p, err := core.PackPath(name); err != nil {
				return m.notify(err.Error(), true)
			} else if core.Exists(p) {
				return m.notify("pack exists: "+name, true)
			}
			if err := core.SavePack(&core.Pack{Name: name, Skills: names}); err != nil {
				return m.notify(err.Error(), true)
			}
			if after != nil {
				after()
			}
			return tea.Batch(m.notify(fmt.Sprintf("created @%s with %d skill(s); e = description in Packs tab", name, len(names)), false), m.reload(false))
		}))
	return nil
}

func (m *Model) showWarnings(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	e, w, _ := core.CheckSkill(filepath.Join(core.SKILLS, name), name)
	var lines []string
	for _, x := range e {
		lines = append(lines, "ERROR "+x)
	}
	for _, x := range w {
		lines = append(lines, "WARN  "+x)
	}
	if len(lines) == 0 {
		lines = []string{"clean"}
	}
	m.push(newTextModal("validation: "+name, strings.Join(lines, "\n")))
	return nil
}

func (m *Model) openSkill(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	t, err := core.ReadText(filepath.Join(core.SKILLS, name, "SKILL.md"))
	if err != nil {
		t = "(missing)"
	}
	m.push(newMDModal(name+" · "+m.d.sourceLine(name), t))
	return nil
}

func (d *data) sourceLine(name string) string {
	src := core.LoadMap(filepath.Join(core.SKILLS, name, "SOURCE.json"))
	s := core.SourceOf(filepath.Join(core.SKILLS, name))
	if c, ok := src["commit"].(string); ok && c != "" {
		s += " @ " + core.Short(c, 10)
	}
	return s
}
