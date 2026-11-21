package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
)

// listMouse: shared click/wheel/double-click handling for a single table.
func listMouse(m *Model, t *table, msg tea.MouseMsg, onDouble func() tea.Cmd, toggleCol bool) (tea.Cmd, bool) {
	mm := msg.Mouse()
	if d, ok := isWheel(msg); ok {
		if t.area.has(mm.X, mm.Y) {
			t.scroll(d)
			return nil, true
		}
		return nil, false
	}
	c, ok := isClick(msg)
	if !ok {
		return nil, false
	}
	i := t.rowAt(c.X, c.Y)
	if i < 0 {
		return nil, t.area.has(c.X, c.Y)
	}
	dbl := m.isDouble(c.X, c.Y)
	t.cursor = i
	if toggleCol && c.X <= t.area.x+1 {
		k := t.current()
		if t.sel[k] {
			delete(t.sel, k)
		} else {
			t.sel[k] = true
		}
		return nil, true
	}
	if dbl && onDouble != nil {
		return onDouble(), true
	}
	return nil, true
}

// ───────────── 3 Packs ─────────────

type packsTab struct {
	sums       []core.PackSummary
	tbl, mem   *table
	members    []string
	own        map[string]bool
	focus      int // 0 packs, 1 members
	showVendor bool
	info       string
	inst       map[string]bool
}

func newPacksTab(m *Model) *packsTab {
	t := &packsTab{}
	t.tbl = newTable(column{title: "pack", flex: 3}, column{title: "skills", width: 6}, column{title: "project", width: 9},
		column{title: "description", flex: 6})
	t.tbl.focused = true
	t.tbl.keyOf = func(i int) string { return t.sums[i].Name }
	t.tbl.cell = func(i, c int) string {
		s := t.sums[i]
		switch c {
		case 0:
			return "@" + s.Name
		case 1:
			if s.Error != "" {
				return stErr.Render("ERR")
			}
			return itoa(s.Count)
		case 2:
			if t.inst[s.Name] {
				return styled("ok")
			}
			return ""
		case 3:
			if s.Error != "" {
				return stErr.Render(s.Error)
			}
			return stDim.Render(s.Description)
		}
		return ""
	}
	t.mem = newTable(column{title: "skill", flex: 3}, column{title: "project", width: 9}, column{title: "description", flex: 5})
	t.mem.keyOf = func(i int) string { return t.members[i] }
	t.mem.cell = func(i, c int) string {
		n := t.members[i]
		switch c {
		case 0:
			if t.own[n] {
				return n
			}
			return n + stDim.Render(" (extends)")
		case 1:
			return styled(m.d.state(n))
		case 2:
			if d := m.d.ix.Get(n); d != nil {
				return stDim.Render(d.Row.Description)
			}
			return stWarn.Render("⚠ not in library")
		}
		return ""
	}
	return t
}

func (t *packsTab) title() string { return "Packs" }
func (t *packsTab) typing() bool  { return false }

func (t *packsTab) refresh(m *Model) {
	t.sums = core.PackSummaries(t.showVendor)
	t.inst = map[string]bool{}
	for _, p := range core.ReadManifest(m.project).PackNames() {
		t.inst[p] = true
	}
	t.tbl.n = len(t.sums)
	t.tbl.clamp()
	t.loadMembers(m)
}

func (t *packsTab) loadMembers(m *Model) {
	p := t.tbl.current()
	t.members, t.own, t.info = nil, map[string]bool{}, ""
	if p == "" {
		t.mem.n = 0
		return
	}
	pk, err := core.LoadPack(p)
	if err != nil {
		t.info = err.Error()
		t.mem.n = 0
		return
	}
	all, err := core.ExpandPack(p)
	if err != nil {
		t.info = err.Error()
	}
	t.members = all
	for _, s := range pk.Skills {
		t.own[s] = true
	}
	tok := 0
	for _, s := range all {
		if core.IsDir(filepath.Join(core.SKILLS, s)) {
			tok += core.SkillTokens(filepath.Join(core.SKILLS, s))
		}
	}
	stacks := m.d.tax.StacksForPack(p)
	var st []string
	for _, s := range stacks {
		st = append(st, s.Title)
	}
	t.info = fmt.Sprintf("@%s: %s\nextends: %s · %d skills · ≈%d tokens · stacks: %s · install: skillman add @%s",
		p, pk.Description, orDash(strings.Join(pk.Extends, ", ")), len(all), tok, orDash(strings.Join(st, ", ")), p)
	t.mem.n = len(all)
	t.mem.clamp()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func (t *packsTab) actions(m *Model) []action {
	cur := func() string { return t.tbl.current() }
	save := func(m *Model, pk *core.Pack, note string) tea.Cmd {
		if err := core.SavePack(pk); err != nil {
			return m.notify(err.Error(), true)
		}
		return tea.Batch(m.notify(note, false), m.reload(false))
	}
	return []action{
		{key: "i", label: "install", run: func(m *Model) tea.Cmd {
			p := cur()
			if p == "" {
				return nil
			}
			items, err := core.ExpandItems([]string{"@" + p})
			if err != nil {
				return m.notify(err.Error(), true)
			}
			return m.installItems(items, "@"+p, nil)
		}},
		{key: "u", label: "remove from project", run: func(m *Model) tea.Cmd {
			if p := cur(); p != "" {
				return m.uninstallPack(p)
			}
			return nil
		}},
		{key: "n", label: "new", run: func(m *Model) tea.Cmd {
			m.push(ask("New pack name (kebab-case, optional group/):", "", "my-stack", func(m *Model, name string) tea.Cmd {
				if name == "" {
					return nil
				}
				if p, err := core.PackPath(name); err != nil {
					return m.notify(err.Error(), true)
				} else if core.Exists(p) {
					return m.notify("pack exists", true)
				}
				return save(m, &core.Pack{Name: name}, "created @"+name+"; add skills with a here or p in Library/Browse")
			}))
			return nil
		}},
		{key: "a", label: "add skill", run: func(m *Model) tea.Cmd {
			p := cur()
			pk, err := core.LoadPack(p)
			if err != nil {
				return m.notify(err.Error(), true)
			}
			var opts []option
			for _, d := range m.d.ix.Docs {
				if !core.Contains(pk.Skills, d.Row.Name) {
					opts = append(opts, option{d.Row.Name, "[" + d.Row.Domain + "] " + d.Row.Description})
				}
			}
			m.push(pick("Add skill to @"+p, opts, func(m *Model, s string) tea.Cmd {
				pk.Skills = append(pk.Skills, s)
				return save(m, pk, s+" → @"+p)
			}))
			return nil
		}},
		{key: "x", label: "remove skill", run: func(m *Model) tea.Cmd {
			p, s := cur(), t.mem.current()
			if t.focus != 1 || s == "" {
				return m.notify("focus a skill in the member list (tab) first", true)
			}
			pk, err := core.LoadPack(p)
			if err != nil {
				return m.notify(err.Error(), true)
			}
			if !core.Contains(pk.Skills, s) {
				return m.notify(s+" comes from an extended pack; edit that pack instead", true)
			}
			var keep []string
			for _, x := range pk.Skills {
				if x != s {
					keep = append(keep, x)
				}
			}
			pk.Skills = keep
			return save(m, pk, s+" removed from @"+p)
		}},
		{key: "e", label: "description", run: func(m *Model) tea.Cmd {
			p := cur()
			pk, err := core.LoadPack(p)
			if err != nil {
				return m.notify(err.Error(), true)
			}
			m.push(ask("Description of @"+p+":", pk.Description, "", func(m *Model, v string) tea.Cmd {
				pk.Description = v
				return save(m, pk, "description saved")
			}))
			return nil
		}},
		{key: "E", label: "extends", run: func(m *Model) tea.Cmd {
			p := cur()
			pk, err := core.LoadPack(p)
			if err != nil {
				return m.notify(err.Error(), true)
			}
			m.push(ask("@"+p+" extends (space-separated packs):", strings.Join(pk.Extends, " "), "web-vue postgres", func(m *Model, v string) tea.Cmd {
				pk.Extends = nil
				for _, x := range strings.Fields(strings.ReplaceAll(v, ",", " ")) {
					pk.Extends = append(pk.Extends, strings.TrimPrefix(x, "@"))
				}
				return save(m, pk, "extends saved")
			}))
			return nil
		}},
		{key: "r", label: "rename", run: func(m *Model) tea.Cmd {
			p := cur()
			m.push(ask("Rename @"+p+" to:", p, "", func(m *Model, v string) tea.Cmd {
				if v == "" || v == p {
					return nil
				}
				if err := core.RenamePack(p, v); err != nil {
					return m.notify(err.Error(), true)
				}
				return tea.Batch(m.notify("renamed @"+p+" → @"+v, false), m.reload(false))
			}))
			return nil
		}},
		{key: "d", label: "delete", run: func(m *Model) tea.Cmd {
			p := cur()
			m.push(confirm("Delete pack @"+p+"? (the file goes to the trash; skills are not touched)", true, func(m *Model) tea.Cmd {
				if _, err := core.DeletePack(p); err != nil {
					return m.notify(err.Error(), true)
				}
				return tea.Batch(m.notify("@"+p+" moved to trash", false), m.reload(false))
			}))
			return nil
		}},
		{key: "v", label: "vendor packs", run: func(m *Model) tea.Cmd { t.showVendor = !t.showVendor; t.refresh(m); return nil }},
		{key: "enter", label: "open skill", run: func(m *Model) tea.Cmd {
			if t.focus == 1 {
				return m.openSkill(t.mem.current())
			}
			t.setFocus(1)
			return nil
		}, hidden: true},
	}
}

func (t *packsTab) setFocus(f int) {
	t.focus = f
	t.tbl.focused, t.mem.focused = f == 0, f == 1
}

func (t *packsTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	switch k {
	case "tab", "shift+tab":
		t.setFocus(1 - t.focus)
		return nil, true
	case "left", "h":
		t.setFocus(0)
		return nil, true
	case "right", "l":
		t.setFocus(1)
		return nil, true
	}
	if t.focus == 0 {
		if t.tbl.keyNav(k) {
			t.loadMembers(m)
			return nil, true
		}
	} else if t.mem.keyNav(k) {
		return nil, true
	}
	return nil, false
}

func (t *packsTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	if cmd, ok := listMouse(m, t.tbl, msg, func() tea.Cmd { t.setFocus(1); return nil }, false); ok {
		if _, click := isClick(msg); click {
			t.setFocus(0)
		}
		t.loadMembers(m)
		return cmd
	}
	if cmd, ok := listMouse(m, t.mem, msg, func() tea.Cmd { return m.openSkill(t.mem.current()) }, false); ok {
		if _, click := isClick(msg); click {
			t.setFocus(1)
		}
		return cmd
	}
	return nil
}

func (t *packsTab) view(m *Model, r rect) []string {
	lw := r.w / 2
	left := box("Packs", t.tbl.render(rect{r.x + 1, r.y + 1, lw - 2, r.h - 2}), lw, r.h, t.focus == 0)
	info := wrapPlain(t.info, r.w-lw-2)
	if len(info) > 4 {
		info = info[:4]
	}
	for i := range info {
		info[i] = stDim.Render(info[i])
	}
	inner := append(info, t.mem.render(rect{r.x + lw + 1, r.y + 1 + len(info), r.w - lw - 2, r.h - 2 - len(info)})...)
	right := box("Members", inner, r.w-lw, r.h, t.focus == 1)
	return hjoin(left, right)
}
