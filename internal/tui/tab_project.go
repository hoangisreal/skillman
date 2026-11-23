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
		column{title: "packs", flex: 3}, column{title: "pin", width: 3}, column{title: "link", width: 6}, column{title: "source", flex: 4})
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
			return styled(r.State)
		case 3:
			if len(r.Packs) == 0 && r.Manual {
				return stDim.Render("manual")
			}
			return strings.Join(r.Packs, ",")
		case 4:
			if r.Pinned {
				return "📌"
			}
		case 5:
			if !r.LinkOK && r.State != "missing" {
				return stErr.Render("broken")
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
	return m.async(label+" …", func(func(string)) tea.Msg {
		rs, err := fn()
		if err != nil {
			return doneMsg{note: err.Error(), isErr: true, reload: true}
		}
		note, bad := summarize(rs, label)
		d := doneMsg{note: note, isErr: bad, reload: true}
		if bad || len(rs) > 6 {
			d.text, d.title = fmtResults(rs), label
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
				m.push(confirm(strings.Join(risky, ", ")+" modified in the project. Replace with the library version? (old copy goes to trash)", true,
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
			m.push(newTextModal("diff library ↔ project: "+k, core.DiffText(filepath.Join(core.SKILLS, k), core.Dest(p, k), "library", "project", 0)))
			return nil
		}},
		{key: "x", label: "remove", run: func(m *Model) tea.Cmd {
			return m.uninstallSkills(t.tbl.targets(), func() { t.tbl.sel = map[string]bool{} })
		}},
		{key: "P", label: "remove pack", run: func(m *Model) tea.Cmd {
			ps := t.man.PackNames()
			if len(ps) == 0 {
				return m.notify("no packs installed", false)
			}
			var opts []option
			for _, x := range ps {
				opts = append(opts, option{x, ""})
			}
			m.push(pick("Remove which pack from the project?", opts, func(m *Model, v string) tea.Cmd { return m.uninstallPack(v) }))
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
				return m.notify("select an untracked skill to adopt", false)
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
			rows := m.d.rows
			return m.async("suggesting skills …", func(func(string)) tea.Msg {
				rec := search.Rank(core.NewRecommendationCatalog(), p, rows, "", 5, 0, true)
				return doneMsg{then: func(m *Model) tea.Cmd {
					if m.project != p || m.tab() != t {
						return nil
					}
					return t.showSuggestions(m, rec)
				}}
			})
		}},
		{key: "o", label: "doctor", run: func(m *Model) tea.Cmd {
			return m.async("doctor …", func(func(string)) tea.Msg {
				icon := map[string]string{"ok": "✓", "warn": "!", "error": "✗"}
				var lines []string
				for _, x := range core.Doctor(p) {
					lines = append(lines, icon[x.Level]+" "+x.Message)
				}
				return doneMsg{title: "doctor", text: strings.Join(lines, "\n")}
			})
		}},
		{key: "v", label: "verify", run: func(m *Model) tea.Cmd {
			if !core.IsFile(filepath.Join(p, ".agents", core.Manifest)) {
				return m.notify("no .agents/skill-manifest.json: verify (and CI) would fail", true)
			}
			return m.async("verify …", func(func(string)) tea.Msg {
				rs := core.Verify(p)
				bad := 0
				for _, r := range rs {
					if !r.OK {
						bad++
					}
				}
				head := fmt.Sprintf("%d/%d skills match the manifest", len(rs)-bad, len(rs))
				if bad > 0 {
					head += fmt.Sprintf(" · %d would fail CI", bad)
				} else {
					head += " · CI would pass"
				}
				return doneMsg{title: "verify (CI frozen check)", text: head + "\n\n" + fmtResults(rs)}
			})
		}},
		{key: "A", label: "audit", run: func(m *Model) tea.Cmd {
			return m.async("audit …", func(func(string)) tea.Msg {
				rep, err := core.Audit(p, false)
				if err != nil {
					return doneMsg{note: err.Error(), isErr: true}
				}
				return doneMsg{title: "audit (offline risk scan)", text: auditText(rep)}
			})
		}},
		{key: "l", label: "fix links", run: func(m *Model) tea.Cmd {
			msgs, err := core.RepairLinks(p)
			if err != nil {
				return m.notify(err.Error(), true)
			}
			if len(msgs) == 0 {
				msgs = []string{"links ok"}
			}
			return tea.Batch(m.notify(strings.Join(msgs, " · "), false), m.reload(false))
		}},
		{key: "c", label: "change project", run: func(m *Model) tea.Cmd {
			var opts []option
			opts = append(opts, option{"…", "type a path"})
			for _, rp := range core.RegisteredProjects() {
				opts = append(opts, option{rp, ""})
			}
			m.push(pick("Switch project", opts, func(m *Model, v string) tea.Cmd {
				if v == "…" {
					m.push(ask("Project directory:", m.project, "", func(m *Model, v string) tea.Cmd { return m.switchProject(v) }))
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

func (t *projectTab) showSuggestions(m *Model, rec search.Recommendation) tea.Cmd {
	if len(rec.Shortlist) == 0 {
		return m.notify(rec.Message, false)
	}
	var options []option
	for _, c := range rec.Shortlist {
		options = append(options, option{c.Name, fmt.Sprintf("%s (%s) · +%dt %s", c.Reason, c.Evidence, c.AddedTokens, c.State)})
	}
	picker := pick("Suggested project skills", options, nil)
	picker.filter.placeholder = "type to filter · space/click to mark · enter to preview"
	picker.pickMany = func(m *Model, names []string) tea.Cmd {
		items, err := core.ExpandItems(names)
		if err != nil {
			return m.notify(err.Error(), true)
		}
		return m.installItems(items, strings.Join(names, ", "), nil)
	}
	m.push(picker)
	return nil
}

func auditText(rep []core.AuditReport) string {
	cnt := map[string]int{}
	for _, r := range rep {
		cnt[r.Level]++
	}
	var parts []string
	for _, lv := range core.AuditLevels {
		parts = append(parts, fmt.Sprintf("%s=%d", lv, cnt[lv]))
	}
	lines := []string{strings.Join(parts, ", "), ""}
	sorted := append([]core.AuditReport{}, rep...)
	rank := map[string]int{"error": 0, "review": 1, "info": 2, "ok": 3}
	sort.SliceStable(sorted, func(i, j int) bool { return rank[sorted[i].Level] < rank[sorted[j].Level] })
	icon := map[string]string{"ok": "✓", "info": "·", "review": "!", "error": "✗"}
	for _, r := range sorted {
		ex := ""
		if len(r.Executables) > 0 {
			ex = fmt.Sprintf("  (%d executable files)", len(r.Executables))
		}
		lines = append(lines, icon[r.Level]+" "+r.Name+ex)
		for _, e := range r.Errors {
			lines = append(lines, "    error: "+e)
		}
		for _, e := range r.Review {
			lines = append(lines, "    review: "+e)
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
	return tea.Batch(m.notify("project: "+d, false), m.reload(false))
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
		parts = append(parts, styled(k)+"="+itoa(cnt[k]))
	}
	meter := stOK.Render("ok")
	if t.tb.Total > t.tb.Budget {
		meter = stErr.Render("over budget")
	}
	loading := ""
	if !m.d.loaded {
		loading = stDim.Render(" (computing status…)")
	}
	head := []string{
		stBold.Render(tilde(m.project)) + loading,
		"packs: " + orDash(strings.Join(t.man.PackNames(), ", ")) + " · " + strings.Join(parts, " "),
		fmt.Sprintf("descriptions ≈ %d/%d tokens (%d project + %d global skills) ", t.tb.Total, t.tb.Budget, t.tb.ProjectSkills, t.tb.GlobalSkills) + meter,
	}
	out := head
	return append(out, t.tbl.render(rect{r.x, r.y + len(head), r.w, r.h - len(head)})...)
}
