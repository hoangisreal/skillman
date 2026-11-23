package tui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
)

// ───────────── 5 Updates ─────────────

type updatesTab struct {
	tbl         *table
	items       map[string]core.UpdateItem
	staged      map[string]core.Staged
	keys        []string
	onlyChanges bool
	info        string
}

func newUpdatesTab(m *Model) *updatesTab {
	t := &updatesTab{items: map[string]core.UpdateItem{}, staged: map[string]core.Staged{}, onlyChanges: true,
		info: "c = check all upstream · k = one pack · L = skills selected in Library (read-only, nothing changes)"}
	for _, p := range core.LoadPending() {
		t.staged[p.Name] = p
		it := p.UpdateItem
		it.Status = "pending-risky"
		t.items[p.Name] = it
	}
	t.tbl = newTable(column{title: " ", width: 1}, column{title: "skill", flex: 3}, column{title: "status", width: 16},
		column{title: "repo", flex: 3}, column{title: "commit", width: 17}, column{title: "risk", flex: 4})
	t.tbl.focused = true
	t.tbl.empty = "no changes listed — press c to check upstream"
	t.tbl.keyOf = func(i int) string { return t.keys[i] }
	t.tbl.cell = func(i, c int) string {
		it := t.items[t.keys[i]]
		switch c {
		case 0:
			return t.tbl.mark(i)
		case 1:
			return it.Name
		case 2:
			return styled(it.Status)
		case 3:
			return stDim.Render(it.Repo)
		case 4:
			return core.Short(it.Commit, 8) + "→" + core.Short(it.NewCommit, 8)
		case 5:
			if s, ok := t.staged[it.Name]; ok {
				if len(s.Risk) > 0 {
					return stateStyle["modified"].Render(strings.Join(s.Risk, "; "))
				}
				return stOK.Render("safe")
			}
		}
		return ""
	}
	t.fill()
	return t
}

func (t *updatesTab) title() string    { return "Updates" }
func (t *updatesTab) typing() bool     { return false }
func (t *updatesTab) refresh(m *Model) {}

func (t *updatesTab) fill() {
	t.keys = t.keys[:0]
	for k, it := range t.items {
		if t.onlyChanges && it.Status == "up-to-date" {
			continue
		}
		t.keys = append(t.keys, k)
	}
	sort.Slice(t.keys, func(i, j int) bool {
		a, b := t.items[t.keys[i]], t.items[t.keys[j]]
		if (a.Status == "up-to-date") != (b.Status == "up-to-date") {
			return b.Status == "up-to-date"
		}
		return a.Name < b.Name
	})
	t.tbl.n = len(t.keys)
	t.tbl.clamp()
}

type checkedMsg struct {
	rep   *core.UpdateReport
	label string
}

type stagedMsg struct {
	names []string
	then  string
	errs  []string
	got   []core.Staged
}

func (t *updatesTab) check(m *Model, names []string, label string) tea.Cmd {
	return m.async("checking "+label+" …", func(progress func(string)) tea.Msg {
		rep := core.CheckUpdates(names, 8, func(repo string, done, total int) {
			progress(fmt.Sprintf("checking %s … %d/%d repos (%s)", label, done, total, repo))
		})
		return checkedMsg{rep, label}
	})
}

func (t *updatesTab) stage(m *Model, names []string, then string) tea.Cmd {
	var todo []core.UpdateItem
	for _, n := range names {
		it, ok := t.items[n]
		if _, done := t.staged[n]; ok && !done && (it.Status == "changed" || it.Status == "pending-risky") {
			todo = append(todo, it)
		}
	}
	return m.async("staging …", func(progress func(string)) tea.Msg {
		var out stagedMsg
		out.names, out.then = names, then
		for _, it := range todo {
			progress("staging " + it.Name + " …")
			s, err := core.StageUpdate(it)
			if err != nil {
				out.errs = append(out.errs, it.Name+": "+err.Error())
				continue
			}
			out.got = append(out.got, s)
		}
		return out
	})
}

func (t *updatesTab) handle(m *Model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case checkedMsg:
		for _, it := range msg.rep.Items {
			if staged, ok := t.staged[it.Name]; ok &&
				(it.Status != "changed" || staged.Commit != it.Commit || staged.NewCommit != it.NewCommit || !core.IsDir(staged.Staged)) {
				delete(t.staged, it.Name)
			}
			t.items[it.Name] = it
		}
		cnt := map[string]int{}
		for _, it := range msg.rep.Items {
			cnt[it.Status]++
		}
		var parts []string
		for k, v := range cnt {
			parts = append(parts, k+"="+itoa(v))
		}
		sort.Strings(parts)
		t.info = fmt.Sprintf("%s: %s · no upstream: %d · checked %s", msg.label, strings.Join(parts, ", "), len(msg.rep.NoUpstream), msg.rep.CheckedAt)
		if len(msg.rep.Errors) > 0 {
			t.info += "\nerrors: " + strings.Join(msg.rep.Errors[:min(3, len(msg.rep.Errors))], "; ")
		}
		t.info += "\nenter = changelog + diff · a = apply selected · A = apply all safe"
		t.fill()
		return m.notify("update check done: "+strings.Join(parts, ", "), false)
	case stagedMsg:
		for _, s := range msg.got {
			t.staged[s.Name] = s
		}
		t.fill()
		var cmds []tea.Cmd
		if len(msg.errs) > 0 {
			cmds = append(cmds, m.notify(strings.Join(msg.errs, "; "), true))
		}
		switch msg.then {
		case "details":
			if len(msg.names) > 0 {
				if s, ok := t.staged[msg.names[0]]; ok {
					cmds = append(cmds, t.details(m, s))
				}
			}
		case "apply", "apply-safe":
			var ss []core.Staged
			for _, n := range msg.names {
				if s, ok := t.staged[n]; ok {
					ss = append(ss, s)
				}
			}
			cmds = append(cmds, t.apply(m, ss, msg.then == "apply-safe"))
		}
		return tea.Batch(cmds...)
	}
	return nil
}

func (t *updatesTab) details(m *Model, s core.Staged) tea.Cmd {
	return m.async("loading changelog …", func(func(string)) tea.Msg {
		risk := strings.Join(s.Risk, "; ")
		if risk == "" {
			risk = "none (safe)"
		}
		text := fmt.Sprintf("%s  %s  %s → %s\nrisk: %s\nadded: %s\nremoved: %s\nchanged: %s\n\nupstream changelog:\n%s\n\n%s",
			s.Name, s.Repo, core.Short(s.Commit, 12), core.Short(s.NewCommit, 12), risk, orDash(strings.Join(s.Added, ", ")),
			orDash(strings.Join(s.Removed, ", ")), orDash(strings.Join(s.ChangedFiles, ", ")), core.Changelog(s.UpdateItem, 30),
			core.DiffText(filepath.Join(core.SKILLS, s.Name), s.Staged, "current", "upstream", 0))
		return doneMsg{title: "update: " + s.Name, text: text}
	})
}

func (t *updatesTab) apply(m *Model, staged []core.Staged, safeOnly bool) tea.Cmd {
	var safe, risky []core.Staged
	for _, s := range staged {
		if len(s.Risk) == 0 {
			safe = append(safe, s)
		} else if !safeOnly {
			risky = append(risky, s)
		}
	}
	go1 := func(m *Model, allow bool) tea.Cmd {
		todo := append([]core.Staged{}, safe...)
		if allow {
			todo = append(todo, risky...)
		}
		return m.async("applying "+itoa(len(todo))+" update(s) …", func(func(string)) tea.Msg {
			rs := core.ApplyUpdates(todo, allow, true)
			var pend []core.Staged
			for _, s := range staged {
				if len(s.Risk) > 0 {
					pend = append(pend, s)
				}
			}
			if err := core.QueuePending(pend); err != nil {
				rs = append(rs, core.Result{Name: "pending updates", Message: err.Error()})
			}
			return doneMsg{reindex: true, title: "apply updates", text: fmtResults(rs), then: func(m *Model) tea.Cmd {
				for _, r := range rs {
					if r.OK {
						delete(t.staged, r.Name)
						if it, ok := t.items[r.Name]; ok {
							it.Status = "up-to-date"
							t.items[r.Name] = it
						}
					}
				}
				t.fill()
				return nil
			}}
		})
	}
	switch {
	case len(risky) > 0:
		var rs []string
		for _, s := range risky {
			rs = append(rs, s.Name+": "+strings.Join(s.Risk, "; "))
		}
		opts := []option{{"safe", fmt.Sprintf("apply only the %d safe update(s)", len(safe))},
			{"all", "apply ALL incl. " + itoa(len(risky)) + " risky: " + core.Short(strings.Join(rs, " | "), 300)}, {"cancel", "do nothing"}}
		m.push(pick("Risky updates staged — choose (review with enter first)", opts, func(m *Model, v string) tea.Cmd {
			if v == "safe" || v == "all" {
				return go1(m, v == "all")
			}
			return nil
		}))
	case len(safe) > 0:
		m.push(confirm(fmt.Sprintf("Apply %d safe update(s) to the library and commit?", len(safe)), false, func(m *Model) tea.Cmd { return go1(m, false) }))
	default:
		return m.notify("nothing safe to apply", false)
	}
	return nil
}

func (t *updatesTab) actions(m *Model) []action {
	return []action{
		{key: "c", label: "check all", run: func(m *Model) tea.Cmd { return t.check(m, nil, "all skills") }},
		{key: "k", label: "check pack", run: func(m *Model) tea.Cmd {
			var opts []option
			for _, s := range core.PackSummaries(true) {
				opts = append(opts, option{s.Name, s.Description})
			}
			m.push(pick("Check updates for pack", opts, func(m *Model, p string) tea.Cmd {
				names, err := core.ExpandPack(p)
				if err != nil {
					return m.notify(err.Error(), true)
				}
				return t.check(m, names, "@"+p)
			}))
			return nil
		}},
		{key: "L", label: "check Library sel.", run: func(m *Model) tea.Cmd {
			lib := m.tabs[0].(*libraryTab)
			names := lib.tbl.targets()
			if len(lib.tbl.sel) == 0 {
				return m.notify("select skills in Library (tab 1, space) first", true)
			}
			return t.check(m, names, itoa(len(names))+" selected skills")
		}},
		{key: "space", label: "select", run: func(m *Model) tea.Cmd { t.tbl.toggle(); return nil }, hidden: true},
		{key: "enter", label: "details", run: func(m *Model) tea.Cmd {
			k := t.tbl.current()
			if s, ok := t.staged[k]; ok {
				return t.details(m, s)
			}
			if t.items[k].Status == "changed" {
				return t.stage(m, []string{k}, "details")
			}
			return m.notify(k+": "+t.items[k].Status, false)
		}},
		{key: "a", label: "apply selected", run: func(m *Model) tea.Cmd {
			var names []string
			for _, n := range t.tbl.targets() {
				if s := t.items[n].Status; s == "changed" || s == "pending-risky" {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				return m.notify("select changed skills (space) or put the cursor on one", true)
			}
			return t.stage(m, names, "apply")
		}},
		{key: "A", label: "apply all safe", run: func(m *Model) tea.Cmd {
			var names []string
			for n, it := range t.items {
				if it.Status == "changed" {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				return m.notify("no changed skills — run a check first (c)", false)
			}
			return t.stage(m, names, "apply-safe")
		}},
		{key: "r", label: "rollback", run: func(m *Model) tea.Cmd {
			k := t.tbl.current()
			if k == "" {
				k = m.tabs[0].(*libraryTab).tbl.current()
			}
			if k == "" {
				return nil
			}
			m.push(confirm("Roll back library skill "+k+" to its previous git version and pin it?", false, func(m *Model) tea.Cmd {
				return m.async("rolling back "+k+" …", func(func(string)) tea.Msg {
					msg, err := core.Rollback(k, true)
					if err != nil {
						return doneMsg{note: err.Error(), isErr: true}
					}
					return doneMsg{note: msg, reindex: true}
				})
			}))
			return nil
		}},
		{key: "f", label: "only changes", run: func(m *Model) tea.Cmd { t.onlyChanges = !t.onlyChanges; t.fill(); return nil }},
		{key: "p", label: "update projects", run: func(m *Model) tea.Cmd {
			aff := core.ProjectsAffected(nil)
			if len(aff) == 0 {
				return m.notify("no registered project has outdated skills", false)
			}
			var lines []string
			for _, a := range aff {
				lines = append(lines, a.Project+": "+strings.Join(a.Skills, ", "))
			}
			m.push(confirm("Update outdated skills in:\n"+strings.Join(lines, "\n")+"\n(pinned and locally modified skills are skipped)", false,
				func(m *Model) tea.Cmd {
					return m.async("updating projects …", func(func(string)) tea.Msg {
						var all []core.Result
						for _, a := range aff {
							rs, err := core.UpdateProject(a.Project, a.Skills, false)
							if err != nil {
								all = append(all, core.Result{Name: a.Project, Message: err.Error()})
								continue
							}
							for _, r := range rs {
								r.Name = filepath.Base(a.Project) + "/" + r.Name
								all = append(all, r)
							}
						}
						return doneMsg{title: "update projects", text: fmtResults(all), reload: true}
					})
				}))
			return nil
		}},
	}
}

func (t *updatesTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return nil, t.tbl.keyNav(msg.String())
}

func (t *updatesTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	cmd, _ := listMouse(m, t.tbl, msg, func() tea.Cmd {
		for _, a := range t.actions(m) {
			if a.key == "enter" {
				return a.run(m)
			}
		}
		return nil
	}, true)
	return cmd
}

func (t *updatesTab) view(m *Model, r rect) []string {
	head := strings.Split(t.info, "\n")
	for i := range head {
		head[i] = stDim.Render(head[i])
	}
	return append(head, t.tbl.render(rect{r.x, r.y + len(head), r.w, r.h - len(head)})...)
}

// ───────────── 6 Upstream candidates ─────────────

type upstreamTab struct {
	q          input
	tbl        *table
	cands      []core.Candidate
	shown      []int
	onlyTicked bool
	lib        map[string]bool
	focus      int
}

func newUpstreamTab(m *Model) *upstreamTab {
	t := &upstreamTab{focus: 1}
	t.q.placeholder = "filter candidates: words · repo:owner · new (not in library)"
	t.tbl = newTable(column{title: "✓", width: 1}, column{title: "skill", flex: 3}, column{title: "repo", flex: 3},
		column{title: "flags", flex: 2}, column{title: "description", flex: 5})
	t.tbl.focused = true
	t.tbl.empty = "no candidates — V = verify upstream repos (staging/repos.txt)"
	t.tbl.keyOf = func(i int) string { return itoa(t.cands[t.shown[i]].Line) }
	t.tbl.cell = func(i, c int) string {
		x := t.cands[t.shown[i]]
		switch c {
		case 0:
			if x.Ticked {
				return stSel.Render("x")
			}
			return " "
		case 1:
			return x.Name
		case 2:
			return stDim.Render(x.Repo)
		case 3:
			f := x.Flags
			if t.lib[x.Name] {
				f = "in library " + f
			}
			return stWarn.Render(f)
		case 4:
			return stDim.Render(x.Description)
		}
		return ""
	}
	return t
}

func (t *upstreamTab) title() string { return "Upstream" }
func (t *upstreamTab) typing() bool  { return t.focus == 0 }

func (t *upstreamTab) refresh(m *Model) {
	t.cands = core.ReadCandidates()
	t.lib = map[string]bool{}
	for _, d := range m.d.ix.Docs {
		t.lib[d.Row.Name] = true
	}
	t.apply()
}

func (t *upstreamTab) apply() {
	words := strings.Fields(strings.ToLower(t.q.Value()))
	t.shown = t.shown[:0]
	for i, c := range t.cands {
		ok := !t.onlyTicked || c.Ticked
		for _, w := range words {
			switch {
			case strings.HasPrefix(w, "repo:"):
				ok = ok && strings.Contains(strings.ToLower(c.Repo), w[5:])
			case w == "new":
				ok = ok && !t.lib[c.Name]
			default:
				ok = ok && strings.Contains(strings.ToLower(c.Name+" "+c.Repo+" "+c.Description), w)
			}
		}
		if ok {
			t.shown = append(t.shown, i)
		}
	}
	t.tbl.n = len(t.shown)
	t.tbl.clamp()
}

func (t *upstreamTab) tick(m *Model, i int) tea.Cmd {
	if i < 0 || i >= len(t.shown) {
		return nil
	}
	c := &t.cands[t.shown[i]]
	var note tea.Cmd
	if c.Collides && !c.Ticked {
		note = m.notify(c.Name+" collides with an existing skill; importing needs --rename-collisions", true)
	}
	if err := core.SetTicks(map[int]bool{c.Line: !c.Ticked}); err != nil {
		return m.notify(err.Error(), true)
	}
	c.Ticked = !c.Ticked
	return note
}

func (t *upstreamTab) actions(m *Model) []action {
	return []action{
		{key: "/", label: "filter", run: func(m *Model) tea.Cmd { t.setFocus(0); return nil }},
		{key: "space", label: "tick", run: func(m *Model) tea.Cmd { c := t.tick(m, t.tbl.cursor); t.tbl.move(1); return c }},
		{key: "I", label: "import ticked", run: func(m *Model) tea.Cmd { return t.importRun(m, true) }},
		{key: "t", label: "only ticked", run: func(m *Model) tea.Cmd { t.onlyTicked = !t.onlyTicked; t.apply(); return nil }},
		{key: "V", label: "verify repos", run: func(m *Model) tea.Cmd {
			m.push(confirm("Clone (sparse, blob-less) every repo in staging/repos.txt and list candidate skills? Nothing is imported.", false,
				func(m *Model) tea.Cmd {
					return m.async("verifying upstream repos …", func(progress func(string)) tea.Msg {
						n := 0
						res, err := core.VerifyRepos("", 8, func(r core.RepoResult) {
							n++
							progress(fmt.Sprintf("verified %d repos · %s: %s (%d skills)", n, r.Repo, r.Status, len(r.Skills)))
						})
						if err != nil {
							return doneMsg{note: err.Error(), isErr: true}
						}
						return doneMsg{note: fmt.Sprintf("verified %d repos", len(res)), reload: true}
					})
				}))
			return nil
		}},
		{key: "esc", label: "clear", run: func(m *Model) tea.Cmd { return nil }, hidden: true},
	}
}

func (t *upstreamTab) importRun(m *Model, dry bool, reviewed ...core.ImportLine) tea.Cmd {
	return m.async(map[bool]string{true: "import dry-run …", false: "importing …"}[dry], func(progress func(string)) tea.Msg {
		log, err := core.ImportTicked(core.QueueImportOptions{DryRun: dry, Reviewed: reviewed}, func(l core.ImportLine) { progress(l.Result + " " + l.Name) })
		if err != nil {
			return doneMsg{note: err.Error(), isErr: true}
		}
		if log == nil {
			return doneMsg{note: "nothing ticked"}
		}
		var b strings.Builder
		n := 0
		for _, l := range log {
			fmt.Fprintf(&b, "%-9s %s:%s @ %s  %s\n", l.Result, l.Repo, l.Name, core.Short(l.Commit, 12), l.Detail)
			if l.Result == "DRY-RUN" {
				n++
			}
		}
		if !dry {
			rows, e, w, err := core.RebuildIndex(true)
			if err != nil {
				return doneMsg{note: err.Error(), isErr: true, title: "import result", text: b.String(), reindex: true}
			}
			fmt.Fprintf(&b, "\nvalidate: skills=%d errors=%d warnings=%d\nreview, then commit: git -C %s add -A && git -C %s commit\n", len(rows), len(e), len(w), core.LIB, core.LIB)
			return doneMsg{title: "import result", text: b.String(), reindex: true}
		}
		return doneMsg{then: func(m *Model) tea.Cmd {
			pre := newTextModal("import preview", b.String())
			if n > 0 {
				pre.accept = func(m *Model) tea.Cmd {
					m.push(confirm(fmt.Sprintf("Import %d skill(s) for real?", n), false, func(m *Model) tea.Cmd { return t.importRun(m, false, log...) }))
					return nil
				}
			}
			m.push(pre)
			return nil
		}}
	})
}

func (t *upstreamTab) setFocus(f int) { t.focus = f; t.q.focused = f == 0; t.tbl.focused = f == 1 }

func (t *upstreamTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if t.focus == 0 {
		switch k {
		case "enter", "down", "tab":
			t.setFocus(1)
			return nil, true
		case "esc":
			if t.q.Value() != "" {
				t.q.Set("")
				t.apply()
			} else {
				t.setFocus(1)
			}
			return nil, true
		}
		if ch, ok := t.q.key(msg); ok {
			if ch {
				t.apply()
			}
			return nil, true
		}
		return nil, false
	}
	if k == "tab" || k == "shift+tab" {
		t.setFocus(0)
		return nil, true
	}
	return nil, t.tbl.keyNav(k)
}

func (t *upstreamTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	if c, ok := isClick(msg); ok && c.Y == t.q.area.y {
		t.setFocus(0)
		return nil
	}
	if c, ok := isClick(msg); ok && c.X <= t.tbl.area.x+1 {
		if i := t.tbl.rowAt(c.X, c.Y); i >= 0 {
			t.tbl.cursor = i
			return t.tick(m, i)
		}
	}
	cmd, ok := listMouse(m, t.tbl, msg, nil, false)
	if ok {
		t.setFocus(1)
	}
	return cmd
}

func (t *upstreamTab) view(m *Model, r rect) []string {
	n := 0
	for _, c := range t.cands {
		if c.Ticked {
			n++
		}
	}
	t.q.area = rect{r.x, r.y, r.w, 1}
	head := []string{t.q.render(r.w, stAccentB.Render("/ ")),
		stDim.Render(fmt.Sprintf("%d/%d candidates · %d ticked · add repos to staging/repos.txt, then V", len(t.shown), len(t.cands), n))}
	return append(head, t.tbl.render(rect{r.x, r.y + 2, r.w, r.h - 2})...)
}

// ───────────── 7 Validation ─────────────

type valRow struct{ level, name, msg, cat string }

type validationTab struct {
	q      input
	tbl    *table
	all    []valRow
	shown  []int
	counts string
	head   string
	focus  int
}

var valRx = regexp.MustCompile("^- (ERROR|WARN) `([^`]+)`: (.*)$")

func newValidationTab(m *Model) *validationTab {
	t := &validationTab{focus: 1}
	t.q.placeholder = "filter: words · error · review · link · lines"
	t.tbl = newTable(column{title: "level", width: 6}, column{title: "skill", flex: 2}, column{title: "finding", flex: 6})
	t.tbl.focused = true
	t.tbl.empty = "no findings (v = re-validate)"
	t.tbl.keyOf = func(i int) string { return itoa(t.shown[i]) }
	t.tbl.cell = func(i, c int) string {
		r := t.all[t.shown[i]]
		switch c {
		case 0:
			return styled(r.level)
		case 1:
			return r.name
		case 2:
			return r.msg
		}
		return ""
	}
	return t
}

func (t *validationTab) title() string { return "Validation" }
func (t *validationTab) typing() bool  { return t.focus == 0 }

func (t *validationTab) refresh(m *Model) {
	text, _ := core.ReadText(filepath.Join(core.LIB, "VALIDATION.md"))
	lines := strings.Split(text, "\n")
	t.all = t.all[:0]
	cnt := map[string]int{}
	for _, l := range lines {
		mm := valRx.FindStringSubmatch(l)
		if mm == nil {
			continue
		}
		cat := "other"
		switch {
		case strings.HasPrefix(mm[3], "REVIEW"):
			cat = "review"
		case strings.Contains(mm[3], "broken link"):
			cat = "link"
		case strings.Contains(mm[3], "lines >"):
			cat = "lines"
		}
		cnt[cat]++
		t.all = append(t.all, valRow{mm[1], mm[2], mm[3], cat})
	}
	t.head = "no report"
	if len(lines) > 2 {
		t.head = lines[2]
	}
	var parts []string
	for k, v := range cnt {
		parts = append(parts, k+"="+itoa(v))
	}
	sort.Strings(parts)
	t.counts = strings.Join(parts, " ")
	t.apply()
}

func (t *validationTab) apply() {
	words := strings.Fields(strings.ToLower(t.q.Value()))
	t.shown = t.shown[:0]
	for i, r := range t.all {
		hay := strings.ToLower(r.level + " " + r.name + " " + r.msg + " " + r.cat)
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(hay, w)
		}
		if ok {
			t.shown = append(t.shown, i)
		}
	}
	t.tbl.n = len(t.shown)
	t.tbl.clamp()
}

func (t *validationTab) jump(m *Model) tea.Cmd {
	if t.tbl.cursor >= len(t.shown) {
		return nil
	}
	name := t.all[t.shown[t.tbl.cursor]].name
	lib := m.tabs[0].(*libraryTab)
	lib.q.Set(name)
	lib.apply(m)
	lib.setFocus(1)
	m.setTab(0)
	return nil
}

func (t *validationTab) actions(m *Model) []action {
	return []action{
		{key: "/", label: "filter", run: func(m *Model) tea.Cmd { t.setFocus(0); return nil }},
		{key: "enter", label: "open in Library", run: func(m *Model) tea.Cmd { return t.jump(m) }},
		{key: "v", label: "re-validate", run: func(m *Model) tea.Cmd {
			return m.async("validating library …", func(func(string)) tea.Msg {
				rows, e, w, err := core.RebuildIndex(true)
				if err != nil {
					return doneMsg{note: err.Error(), isErr: true}
				}
				return doneMsg{note: fmt.Sprintf("re-validated: skills=%d errors=%d warnings=%d", len(rows), len(e), len(w)), reindex: true}
			})
		}},
	}
}

func (t *validationTab) setFocus(f int) { t.focus = f; t.q.focused = f == 0; t.tbl.focused = f == 1 }

func (t *validationTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if t.focus == 0 {
		switch k {
		case "enter", "down", "tab":
			t.setFocus(1)
			return nil, true
		case "esc":
			if t.q.Value() != "" {
				t.q.Set("")
				t.apply()
			} else {
				t.setFocus(1)
			}
			return nil, true
		}
		if ch, ok := t.q.key(msg); ok {
			if ch {
				t.apply()
			}
			return nil, true
		}
		return nil, false
	}
	if k == "tab" || k == "shift+tab" {
		t.setFocus(0)
		return nil, true
	}
	return nil, t.tbl.keyNav(k)
}

func (t *validationTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	if c, ok := isClick(msg); ok && c.Y == t.q.area.y {
		t.setFocus(0)
		return nil
	}
	cmd, ok := listMouse(m, t.tbl, msg, func() tea.Cmd { return t.jump(m) }, false)
	if ok {
		t.setFocus(1)
	}
	return cmd
}

func (t *validationTab) view(m *Model, r rect) []string {
	t.q.area = rect{r.x, r.y, r.w, 1}
	head := []string{t.q.render(r.w, stAccentB.Render("/ ")), stDim.Render(t.head + " · " + t.counts)}
	return append(head, t.tbl.render(rect{r.x, r.y + 2, r.w, r.h - 2})...)
}

// ───────────── 8 Trash ─────────────

type trashTab struct {
	tbl     *table
	entries []core.TrashEntry
}

func newTrashTab(m *Model) *trashTab {
	t := &trashTab{}
	t.tbl = newTable(column{title: "when", width: 16}, column{title: "kind", width: 5}, column{title: "name", flex: 3},
		column{title: "reason", flex: 2}, column{title: "project / origin", flex: 5})
	t.tbl.focused = true
	t.tbl.empty = "trash is empty"
	t.tbl.keyOf = func(i int) string { return t.entries[i].S("slot") }
	t.tbl.cell = func(i, c int) string {
		e := t.entries[i]
		switch c {
		case 0:
			return core.Short(e.S("removed_at"), 16)
		case 1:
			return e.S("kind")
		case 2:
			return e.S("name")
		case 3:
			return stDim.Render(e.S("reason"))
		case 4:
			if p := e.S("project"); p != "" {
				return stDim.Render(p)
			}
			return stDim.Render(e.S("origin"))
		}
		return ""
	}
	return t
}

func (t *trashTab) title() string { return "Trash" }
func (t *trashTab) typing() bool  { return false }

func (t *trashTab) refresh(m *Model) {
	t.entries = core.ListTrash(m.project)
	t.tbl.n = len(t.entries)
	t.tbl.clamp()
}

func (t *trashTab) actions(m *Model) []action {
	cur := func() core.TrashEntry {
		if t.tbl.cursor < len(t.entries) {
			return t.entries[t.tbl.cursor]
		}
		return nil
	}
	return []action{
		{key: "R", label: "restore", run: func(m *Model) tea.Cmd {
			e := cur()
			if e == nil {
				return nil
			}
			msg, err := core.RestoreTrash(e.S("slot"))
			if err != nil {
				return m.notify(err.Error(), true)
			}
			return tea.Batch(m.notify(msg, false), m.reload(false))
		}},
		{key: "D", label: "diff vs library", run: func(m *Model) tea.Cmd {
			e := cur()
			if e == nil || e.S("kind") != "skill" {
				return nil
			}
			payload := filepath.Join(e.S("slot"), e.S("name"))
			m.push(newTextModal("trash vs library: "+e.S("name"), core.DiffText(filepath.Join(core.SKILLS, e.S("name")), payload, "library", "trash", 0)))
			return nil
		}},
		{key: "x", label: "delete forever", run: func(m *Model) tea.Cmd {
			e := cur()
			if e == nil {
				return nil
			}
			m.push(confirm("Permanently delete trash entry "+e.S("name")+"?", true, func(m *Model) tea.Cmd {
				return t.empty(m, []string{e.S("slot")})
			}))
			return nil
		}},
		{key: "E", label: "empty trash", run: func(m *Model) tea.Cmd {
			if len(t.entries) == 0 {
				return nil
			}
			var slots []string
			for _, e := range t.entries {
				slots = append(slots, e.S("slot"))
			}
			m.push(confirm(fmt.Sprintf("Permanently delete ALL %d trash entries?", len(slots)), true, func(m *Model) tea.Cmd {
				return t.empty(m, slots)
			}))
			return nil
		}},
	}
}

func (t *trashTab) empty(m *Model, slots []string) tea.Cmd {
	return m.async("deleting trash entries …", func(func(string)) tea.Msg {
		n, err := core.EmptyTrash(slots)
		note := fmt.Sprintf("deleted %d entries", n)
		if err != nil {
			return doneMsg{title: note, text: err.Error(), reload: true}
		}
		return doneMsg{note: note, reload: true}
	})
}

func (t *trashTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	return nil, t.tbl.keyNav(msg.String())
}

func (t *trashTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	cmd, _ := listMouse(m, t.tbl, msg, nil, false)
	return cmd
}

func (t *trashTab) view(m *Model, r rect) []string {
	head := []string{stDim.Render("removed project skills, replaced copies and deleted packs · R restore · D diff · x delete")}
	return append(head, t.tbl.render(rect{r.x, r.y + 1, r.w, r.h - 1})...)
}
