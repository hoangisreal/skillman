package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
	"github.com/hoangisreal/skillman/internal/search"
)

// ───────────── 4 Project ─────────────

type projectTab struct {
	tbl  *table
	rows []core.SkillStatus
	man  *core.ManifestData
	tb   core.Budget
}

func newProjectTab(m *Model) *projectTab {
	t := &projectTab{}
	t.tbl = newTable(column{title: " ", width: 1}, column{title: "skill", flex: 4}, column{title: "state", width: 10},
		column{title: "packs", flex: 3}, column{title: "pin", width: 4}, column{title: "link", width: 6}, column{title: "source", flex: 4})
	t.tbl.focused = true
	t.tbl.empty = "no skills in this project yet — 2 Browse or 1 Library, then i; or s = suggest packs"
	t.tbl.keyOf = func(i int) string { return t.rows[i].Name }
	t.tbl.cell = func(i, c int) string {
		r := t.rows[i]
		switch c {
		case 0:
			return t.tbl.mark(i)
		case 1:
			return r.Name
		case 2:
			return m.lang.styled(r.State)
		case 3:
			if len(r.Packs) == 0 && r.Manual {
				return stDim.Render(m.lang.text("manual"))
			}
			return strings.Join(r.Packs, ",")
		case 4:
			if r.Pinned {
				return "📌"
			}
		case 5:
			if !r.LinkOK && r.State != "missing" {
				return stErr.Render(m.lang.text("broken"))
			}
		case 6:
			if e := t.man.Skills[r.Name]; e != nil {
				return stDim.Render(e.Source)
			}
		}
		return ""
	}
	return t
}

func (t *projectTab) title() string { return "Project" }
func (t *projectTab) typing() bool  { return false }

func (t *projectTab) refresh(m *Model) {
	t.rows = m.d.status
	t.man = core.ReadManifest(m.project)
	t.tb = core.TokenBudgetOf(m.project)
	t.tbl.n = len(t.rows)
	t.tbl.clamp()
}

func (t *projectTab) run(m *Model, label string, fn func() ([]core.Result, error)) tea.Cmd {
	t.tbl.sel = map[string]bool{}
	lang := m.lang
	return m.async(lang.text(label)+" …", func(func(string)) tea.Msg {
		rs, err := fn()
		if err != nil {
			return doneMsg{note: err.Error(), isErr: true, reload: true}
		}
		note, bad := summarize(lang, rs, label)
		d := doneMsg{note: note, isErr: bad, reload: true}
		if bad || len(rs) > 6 {
			d.text, d.title = fmtResults(lang, rs), label
		}
		return d
	})
}

func (t *projectTab) actions(m *Model) []action {
	p := m.project
	return []action{
		{key: "space", label: "select", run: func(m *Model) tea.Cmd { t.tbl.toggle(); return nil }, hidden: true},
		{key: "u", label: "update", run: func(m *Model) tea.Cmd {
			names := t.tbl.targets()
			if len(names) == 0 {
				return nil
			}
			var risky []string
			for _, n := range names {
				if s := m.d.st[n].State; s == "modified" || s == "conflict" {
					risky = append(risky, n)
				}
			}
			if len(risky) > 0 {
				m.push(confirm(m.lang.format("%s modified in the project. Replace with the library version? (old copy goes to trash)", strings.Join(risky, ", ")), true,
					func(m *Model) tea.Cmd {
						return t.run(m, "updated", func() ([]core.Result, error) { return core.UpdateProject(p, names, true) })
					}))
				return nil
			}
			return t.run(m, "updated", func() ([]core.Result, error) { return core.UpdateProject(p, names, false) })
		}},
		{key: "U", label: "update all", run: func(m *Model) tea.Cmd {
			return t.run(m, "updated", func() ([]core.Result, error) { return core.UpdateProject(p, nil, false) })
		}},
		{key: "D", label: "diff", run: func(m *Model) tea.Cmd {
			k := t.tbl.current()
			if k == "" {
				return nil
			}
			m.push(newTextModal(m.lang.format("diff library ↔ project: %s", k), core.DiffText(filepath.Join(core.SKILLS, k), core.Dest(p, k), "library", "project", 0)))
			return nil
		}},
		{key: "x", label: "remove", run: func(m *Model) tea.Cmd {
			return m.uninstallSkills(t.tbl.targets(), func() { t.tbl.sel = map[string]bool{} })
		}},
		{key: "P", label: "remove pack", run: func(m *Model) tea.Cmd {
			ps := t.man.PackNames()
			if len(ps) == 0 {
				return m.notify(m.lang.text("no packs installed"), false)
			}
			var opts []option
			for _, x := range ps {
				opts = append(opts, option{x, ""})
			}
			m.push(pick(m.lang.text("Remove which pack from the project?"), opts, func(m *Model, v string) tea.Cmd { return m.uninstallPack(v) }))
			return nil
		}},
		{key: "a", label: "adopt", run: func(m *Model) tea.Cmd {
			var names []string
			for _, n := range t.tbl.targets() {
				if m.d.st[n].State == "untracked" {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				return m.notify(m.lang.text("select an untracked skill to adopt"), false)
			}
			return t.run(m, "adopted", func() ([]core.Result, error) { return core.Adopt(p, names) })
		}},
		{key: "t", label: "pin", run: func(m *Model) tea.Cmd {
			names := t.tbl.targets()
			pin := false
			for _, n := range names {
				pin = pin || !m.d.st[n].Pinned
			}
			return t.run(m, map[bool]string{true: "pinned", false: "unpinned"}[pin], func() ([]core.Result, error) { return core.SetPin(p, names, pin) })
		}},
		{key: "s", label: "suggest", run: func(m *Model) tea.Cmd {
			m.push(ask(m.lang.text("What are you working on? Leave blank for project suggestions."), "", "Vue component test", func(m *Model, task string) tea.Cmd {
				return t.suggest(m, task, nil)
			}))
			return nil
		}},
		{key: "o", label: "doctor", run: func(m *Model) tea.Cmd {
			lang := m.lang
			return m.async(lang.text("doctor …"), func(func(string)) tea.Msg {
				icon := map[string]string{"ok": "✓", "warn": "!", "error": "✗"}
				var lines []string
				for _, x := range core.Doctor(p) {
					lines = append(lines, icon[x.Level]+" "+lang.text(x.Message))
				}
				return doneMsg{title: "doctor", text: strings.Join(lines, "\n")}
			})
		}},
		{key: "v", label: "verify", run: func(m *Model) tea.Cmd {
			if !core.IsFile(filepath.Join(p, ".agents", core.Manifest)) {
				return m.notify(m.lang.text("no .agents/skill-manifest.json: verify (and CI) would fail"), true)
			}
			lang := m.lang
			return m.async(lang.text("verify …"), func(func(string)) tea.Msg {
				rs := core.Verify(p)
				bad := 0
				for _, r := range rs {
					if !r.OK {
						bad++
					}
				}
				head := lang.format("%d/%d skills match the manifest", len(rs)-bad, len(rs))
				if bad > 0 {
					head += " · " + lang.format("%d would fail CI", bad)
				} else {
					head += " · " + lang.text("CI would pass")
				}
				return doneMsg{title: "verify (CI frozen check)", text: head + "\n\n" + fmtResults(lang, rs)}
			})
		}},
		{key: "A", label: "audit", run: func(m *Model) tea.Cmd {
			lang := m.lang
			return m.async(lang.text("audit …"), func(func(string)) tea.Msg {
				rep, err := core.Audit(p, false)
				if err != nil {
					return doneMsg{note: err.Error(), isErr: true}
				}
				return doneMsg{title: "audit (offline risk scan)", text: auditText(lang, rep)}
			})
		}},
		{key: "l", label: "fix links", run: func(m *Model) tea.Cmd {
			lang := m.lang
			return m.async(lang.text("repairing Claude links and copies …"), func(func(string)) tea.Msg {
				msgs, err := core.RepairLinks(p)
				if err != nil {
					msgs = append(msgs, err.Error())
					return doneMsg{title: "Claude paths needing review", text: strings.Join(msgs, "\n"), reload: true}
				}
				if len(msgs) == 0 {
					msgs = []string{lang.text("nothing to repair")}
				}
				return doneMsg{note: strings.Join(msgs, " · "), reload: true}
			})
		}},
		{key: "c", label: "change project", run: func(m *Model) tea.Cmd {
			var opts []option
			opts = append(opts, option{"…", m.lang.text("type a path")})
			for _, rp := range core.RegisteredProjects() {
				opts = append(opts, option{rp, ""})
			}
			m.push(pick(m.lang.text("Switch project"), opts, func(m *Model, v string) tea.Cmd {
				if v == "…" {
					m.push(ask(m.lang.text("Project directory:"), m.project, "", func(m *Model, v string) tea.Cmd { return m.switchProject(v) }))
					return nil
				}
				return m.switchProject(v)
			}))
			return nil
		}},
		{key: "ctrl+a", label: "select all", run: func(m *Model) tea.Cmd { t.tbl.selectAll(); return nil }, hidden: true},
		{key: "esc", label: "clear selection", run: func(m *Model) tea.Cmd { t.tbl.sel = map[string]bool{}; return nil }, hidden: true},
		{key: "enter", label: "open", run: func(m *Model) tea.Cmd { return m.openSkill(t.tbl.current()) }, hidden: true},
	}
}

func (t *projectTab) suggest(m *Model, task string, stacks []string) tea.Cmd {
	project, rows := m.project, m.d.rows
	return m.async(m.lang.text("suggesting skills …"), func(func(string)) tea.Msg {
		catalog := core.NewRecommendationCatalog()
		declared, err := catalog.DeclaredSignals(stacks)
		if err != nil {
			return doneMsg{note: err.Error(), isErr: true}
		}
		rec := search.Rank(catalog, project, rows, task, 5, 0, true, declared...)
		if rec.Err != nil {
			return doneMsg{note: rec.Err.Error(), isErr: true}
		}
		return doneMsg{then: func(m *Model) tea.Cmd {
			if m.project == project && m.tab() == t {
				t.showSuggestions(m, rec, task)
			}
			return nil
		}}
	})
}

func previewSelection(m *Model, names []string) tea.Cmd {
	items, err := core.ExpandItems(names)
	if err != nil {
		return m.notify(err.Error(), true)
	}
	return m.installItems(items, strings.Join(names, ", "), nil)
}

func (t *projectTab) showSuggestions(m *Model, rec search.Recommendation, task string) {
	manual := m.lang.text("Choose other skills")
	preview := m.lang.text("Preview installation")
	use := m.lang.text("Read installed skill")
	stack := m.lang.text("Declare project stacks")
	baseline := m.lang.text("Preview @core")
	var options []option
	title := m.lang.text("No matching local skills")
	var chosen search.Choice
	if len(rec.Shortlist) > 0 {
		chosen = rec.Shortlist[0]
		title = chosen.Name
		if rec.Budget.Total+chosen.AddedTokens > rec.Budget.Budget {
			title += " · " + m.lang.text("over budget")
		}
		label := fmt.Sprintf("%s (%s) · +%dt %s", chosen.Reason, m.lang.text(chosen.Evidence), chosen.AddedTokens, m.lang.text(chosen.State))
		action := preview
		if chosen.State == "ok" {
			action = use
		}
		options = append(options, option{action, label})
	}
	options = append(options, option{manual, ""})
	if !rec.HasStack {
		options = append(options, option{stack, "vue, postgres"}, option{baseline, ""})
	}
	m.push(pick(title, options, func(m *Model, value string) tea.Cmd {
		switch value {
		case preview:
			return previewSelection(m, []string{chosen.Name})
		case use:
			body, err := core.ReadText(filepath.Join(core.Dest(m.project, chosen.Name), "SKILL.md"))
			if err != nil {
				return m.notify(err.Error(), true)
			}
			m.push(newMDModal(chosen.Name, body))
		case stack:
			m.push(ask(stack, "", "vue, postgres", func(m *Model, ids string) tea.Cmd {
				return t.suggest(m, task, strings.Fields(strings.ReplaceAll(ids, ",", " ")))
			}))
		case baseline:
			return previewSelection(m, []string{"@core"})
		case manual:
			opts := make([]option, 0, len(m.d.rows))
			seen := map[string]bool{}
			for _, c := range rec.Shortlist {
				opts = append(opts, option{c.Name, c.Description})
				seen[c.Name] = true
			}
			for _, row := range m.d.rows {
				if !seen[row.Name] {
					opts = append(opts, option{row.Name, row.Description})
				}
			}
			picker := pick(manual, opts, nil)
			picker.filter.placeholder = "filter · ctrl+x mark · ctrl+o read · enter preview"
			picker.pickMany = previewSelection
			picker.inspect = (*Model).openSkill
			if chosen.Name != "" && core.NeedsProjectCopy(chosen.State) {
				picker.list.sel[chosen.Name] = true
			}
			m.push(picker)
		}
		return nil
	}))
}

func auditText(lang language, rep []core.AuditReport) string {
	cnt := map[string]int{}
	for _, r := range rep {
		cnt[r.Level]++
	}
	var parts []string
	for _, lv := range core.AuditLevels {
		parts = append(parts, fmt.Sprintf("%s=%d", lang.text(lv), cnt[lv]))
	}
	lines := []string{strings.Join(parts, ", "), ""}
	sorted := append([]core.AuditReport{}, rep...)
	rank := map[string]int{"error": 0, "review": 1, "info": 2, "ok": 3}
	sort.SliceStable(sorted, func(i, j int) bool { return rank[sorted[i].Level] < rank[sorted[j].Level] })
	icon := map[string]string{"ok": "✓", "info": "·", "review": "!", "error": "✗"}
	for _, r := range sorted {
		ex := ""
		if len(r.Executables) > 0 {
			ex = "  " + lang.format("(%d executable files)", len(r.Executables))
		}
		lines = append(lines, icon[r.Level]+" "+r.Name+ex)
		for _, e := range r.Errors {
			lines = append(lines, "    "+lang.format("error: %s", e))
		}
		for _, e := range r.Review {
			lines = append(lines, "    "+lang.format("review: %s", e))
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) switchProject(p string) tea.Cmd {
	if p == "" {
		return nil
	}
	d, err := core.ResolveProject(p, false)
	if err != nil {
		return m.notify(err.Error(), true)
	}
	m.project = d
	m.d.loaded = false
	return tea.Batch(m.notify(m.lang.format("project: %s", d), false), m.reload(false))
}

func (t *projectTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return nil, t.tbl.keyNav(msg.String())
}

func (t *projectTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	cmd, _ := listMouse(m, t.tbl, msg, func() tea.Cmd { return m.openSkill(t.tbl.current()) }, true)
	return cmd
}

func (t *projectTab) view(m *Model, r rect) []string {
	cnt := map[string]int{}
	for _, s := range t.rows {
		cnt[s.State]++
	}
	var ks []string
	for k := range cnt {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	var parts []string
	for _, k := range ks {
		parts = append(parts, m.lang.styled(k)+"="+itoa(cnt[k]))
	}
	meter := m.lang.styled("ok")
	if t.tb.Total > t.tb.Budget {
		meter = stErr.Render(m.lang.text("over budget"))
	}
	loading := ""
	if !m.d.loaded {
		loading = stDim.Render(" " + m.lang.text("(computing status…)"))
	}
	head := []string{
		stBold.Render(tilde(m.project)) + loading,
		m.lang.format("packs: %s", orDash(strings.Join(t.man.PackNames(), ", "))) + " · " + strings.Join(parts, " "),
		m.lang.format("descriptions ≈ %d/%d tokens (%d project + %d global skills) ", t.tb.Total, t.tb.Budget, t.tb.ProjectSkills, t.tb.GlobalSkills) + meter,
	}
	out := head
	return append(out, t.tbl.render(m.lang, rect{r.x, r.y + len(head), r.w, r.h - len(head)})...)
}
