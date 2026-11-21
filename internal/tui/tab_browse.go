package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
	"github.com/hoangisreal/skillman/internal/search"
)

// ───────────── 2 Browse: domain › stack tree ─────────────

type treeNode struct {
	kind     string // project | domain | stack | other
	id       string // domain id / stack id
	domain   string
	title    string
	skills   []string
	packs    []string
	detected bool
	depth    int
}

type browseTab struct {
	tree     []core.DomainNode
	nodes    []treeNode // flattened, visible
	expanded map[string]bool
	tt       *table // tree
	lt       *table // skills
	items    []int  // doc indices of the selected node (after filters)
	q        input
	focus    int // 0 tree, 1 list, 2 filter, 3 preview
	onlyNew  bool
	prev     preview
	lastNode string
	areas    struct{ tree, list, prev rect }
}

func newBrowseTab(m *Model) *browseTab {
	t := &browseTab{expanded: map[string]bool{}}
	t.q.placeholder = "filter this group…"
	t.tt = newTable(column{title: "domain › stack", flex: 1}, column{title: "", width: 4})
	t.tt.focused = true
	t.tt.keyOf = func(i int) string { return t.nodes[i].kind + ":" + t.nodes[i].id }
	t.tt.cell = func(i, c int) string {
		n := t.nodes[i]
		if c == 1 {
			return stDim.Render(itoa(len(n.skills)))
		}
		star := " "
		if n.detected {
			star = stStar.Render("★")
		}
		switch n.kind {
		case "project":
			return stStar.Render("★ This project")
		case "domain":
			arrow := "▸ "
			if t.expanded[n.id] {
				arrow = "▾ "
			}
			return star + arrow + stBold.Render(n.title)
		case "other":
			return "     " + stDim.Render("(other)")
		}
		return star + strings.Repeat("  ", n.depth) + "  " + n.title
	}
	t.lt = newTable(column{title: " ", width: 1}, column{title: "skill", flex: 4}, column{title: "", width: 1},
		column{title: "project", width: 9}, column{title: "description", flex: 6})
	t.lt.empty = "no skills here"
	t.lt.keyOf = func(i int) string { return m.d.ix.Docs[t.items[i]].Row.Name }
	t.lt.cell = func(i, c int) string {
		d := &m.d.ix.Docs[t.items[i]]
		switch c {
		case 0:
			return t.lt.mark(i)
		case 1:
			return d.Row.Name
		case 2:
			if n := t.node(); n != nil && len(n.packs) > 0 {
				for _, p := range d.StackPacks {
					if core.Contains(n.packs, p) {
						return stAccent.Render("◆")
					}
				}
			}
			return ""
		case 3:
			return styled(d.State)
		case 4:
			return stDim.Render(d.Row.Description)
		}
		return ""
	}
	return t
}

func (t *browseTab) title() string { return "Browse" }
func (t *browseTab) typing() bool  { return t.focus == 2 }

func (t *browseTab) node() *treeNode {
	if t.tt.cursor < len(t.nodes) {
		return &t.nodes[t.tt.cursor]
	}
	return nil
}

func (t *browseTab) refresh(m *Model) {
	t.tree = core.BuildTree(m.d.rows, m.d.tax, m.d.detected, m.d.pbs)
	if len(t.expanded) == 0 {
		for _, d := range t.tree {
			if d.Detected {
				t.expanded[d.ID] = true
			}
		}
	}
	t.flatten()
	t.prev.invalidate()
	t.fill(m)
}

func (t *browseTab) flatten() {
	keep := t.tt.current()
	t.nodes = t.nodes[:0]
	var proj []treeNode
	var projSkills []string
	seen := map[string]bool{}
	for _, d := range t.tree {
		for _, s := range d.Stacks {
			if s.Detected {
				proj = append(proj, treeNode{kind: "stack", id: s.ID, domain: d.ID, title: s.Title, skills: s.Skills, packs: s.Packs, detected: true, depth: 1})
				for _, n := range s.Skills {
					if !seen[n] {
						seen[n] = true
						projSkills = append(projSkills, n)
					}
				}
			}
		}
	}
	if len(proj) > 0 {
		t.nodes = append(t.nodes, treeNode{kind: "project", id: "project", title: "This project", skills: projSkills, detected: true})
		if t.expanded["project"] || !t.expandedSet("project") {
			t.nodes = append(t.nodes, proj...)
		}
	}
	for _, d := range t.tree {
		t.nodes = append(t.nodes, treeNode{kind: "domain", id: d.ID, domain: d.ID, title: d.Title, skills: d.Skills, detected: d.Detected})
		if !t.expanded[d.ID] {
			continue
		}
		for _, s := range d.Stacks {
			if len(s.Skills) == 0 {
				continue
			}
			t.nodes = append(t.nodes, treeNode{kind: "stack", id: s.ID, domain: d.ID, title: s.Title, skills: s.Skills, packs: s.Packs, detected: s.Detected, depth: 1})
		}
		if len(d.Other) > 0 && len(d.Stacks) > 0 {
			t.nodes = append(t.nodes, treeNode{kind: "other", id: d.ID + "/other", domain: d.ID, title: "(other)", skills: d.Other, depth: 1})
		}
	}
	t.tt.n = len(t.nodes)
	for i := range t.nodes {
		if t.tt.keyOf(i) == keep {
			t.tt.cursor = i
		}
	}
	t.tt.clamp()
}

// expandedSet: the project node starts expanded until the user collapses it.
func (t *browseTab) expandedSet(id string) bool { _, ok := t.expanded[id]; return ok }

func (t *browseTab) fill(m *Model) {
	n := t.node()
	t.items = t.items[:0]
	if n == nil {
		t.lt.n = 0
		return
	}
	idx := make([]int, 0, len(n.skills))
	for _, s := range n.skills {
		if i, ok := m.d.ix.Pos(s); ok {
			idx = append(idx, i)
		}
	}
	q := search.Parse(t.q.Value())
	q.NotInstalled = q.NotInstalled || t.onlyNew
	if q.Empty() {
		t.items = idx
	} else {
		t.items = m.d.ix.Search(q, idx)
	}
	key := t.tt.current()
	if key != t.lastNode {
		t.lt.cursor, t.lt.offset = 0, 0
		t.lastNode = key
	}
	t.lt.n = len(t.items)
	t.lt.clamp()
}

func (t *browseTab) setFocus(f int) {
	t.focus = f
	t.tt.focused = f == 0
	t.lt.focused = f == 1
	t.q.focused = f == 2
}

func (t *browseTab) toggleNode(m *Model) {
	n := t.node()
	if n == nil {
		return
	}
	switch n.kind {
	case "domain":
		t.expanded[n.id] = !t.expanded[n.id]
	case "project":
		t.expanded["project"] = !(t.expanded["project"] || !t.expandedSet("project"))
	default:
		t.setFocus(1)
		return
	}
	t.flatten()
	t.fill(m)
}

func (t *browseTab) actions(m *Model) []action {
	clear := func() { t.lt.sel = map[string]bool{} }
	return []action{
		{key: "enter", label: "open", run: func(m *Model) tea.Cmd {
			if t.focus == 0 {
				t.toggleNode(m)
				return nil
			}
			return m.openSkill(t.lt.current())
		}},
		{key: "space", label: "select", run: func(m *Model) tea.Cmd {
			if t.focus == 0 {
				t.toggleNode(m)
				return nil
			}
			t.lt.toggle()
			return nil
		}},
		{key: "i", label: "install", run: func(m *Model) tea.Cmd {
			names := t.lt.targets()
			if t.focus == 0 || len(names) == 0 {
				return m.notify("select skills in the list (tab, space) or press I to install the stack's pack", true)
			}
			var items []core.Item
			for _, n := range names {
				items = append(items, core.Item{Name: n})
			}
			return m.installItems(items, itoa(len(items))+" skill(s)", clear)
		}},
		{key: "I", label: "install stack", run: func(m *Model) tea.Cmd {
			n := t.node()
			if n == nil {
				return nil
			}
			if len(n.packs) > 0 {
				var items []core.Item
				label := ""
				for _, p := range n.packs {
					its, err := core.ExpandItems([]string{"@" + p})
					if err != nil {
						return m.notify(err.Error(), true)
					}
					items = append(items, its...)
					label += "@" + p + " "
				}
				return m.installItems(items, strings.TrimSpace(label), nil)
			}
			var items []core.Item
			for _, i := range t.items {
				items = append(items, core.Item{Name: m.d.ix.Docs[i].Row.Name})
			}
			return m.installItems(items, "all "+itoa(len(items))+" skills of "+n.title, nil)
		}},
		{key: "n", label: "new pack", run: func(m *Model) tea.Cmd {
			names := t.lt.targets()
			if len(t.lt.sel) == 0 {
				names = nil
				for _, i := range t.items {
					names = append(names, m.d.ix.Docs[i].Row.Name)
				}
			}
			return m.newPackFrom(names, clear)
		}},
		{key: "p", label: "add to pack", run: func(m *Model) tea.Cmd { return m.addToPack(t.lt.targets(), clear) }},
		{key: "u", label: "not installed", run: func(m *Model) tea.Cmd { t.onlyNew = !t.onlyNew; t.fill(m); return nil }},
		{key: "/", label: "filter", run: func(m *Model) tea.Cmd { t.setFocus(2); return nil }},
		{key: "x", label: "remove", run: func(m *Model) tea.Cmd {
			var names []string
			for _, n := range t.lt.targets() {
				if m.d.state(n) != "" {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				return m.notify("none of these skills is in the project", true)
			}
			return m.uninstallSkills(names, clear)
		}, hidden: true},
		{key: "ctrl+a", label: "select all shown", run: func(m *Model) tea.Cmd { t.lt.selectAll(); return nil }, hidden: true},
		{key: "esc", label: "clear selection", run: func(m *Model) tea.Cmd { clear(); return nil }, hidden: true},
	}
}

func (t *browseTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	switch k {
	case "tab":
		t.setFocus([]int{1, 3, 0, 0}[t.focus])
		return nil, true
	case "shift+tab":
		t.setFocus([]int{3, 0, 1, 1}[t.focus])
		return nil, true
	}
	switch t.focus {
	case 0:
		switch k {
		case "right", "l":
			if n := t.node(); n != nil && (n.kind == "domain" || n.kind == "project") && !t.isOpen(n) {
				t.toggleNode(m)
			} else {
				t.setFocus(1)
			}
			return nil, true
		case "left", "h":
			if n := t.node(); n != nil {
				if (n.kind == "domain" || n.kind == "project") && t.isOpen(n) {
					t.toggleNode(m)
				} else if n.depth > 0 { // jump to the parent domain
					for i := t.tt.cursor; i >= 0; i-- {
						if t.nodes[i].depth == 0 {
							t.tt.cursor = i
							break
						}
					}
					t.tt.clamp()
					t.fill(m)
				}
			}
			return nil, true
		}
		if t.tt.keyNav(k) {
			t.fill(m)
			return nil, true
		}
	case 1:
		if k == "left" || k == "h" {
			t.setFocus(0)
			return nil, true
		}
		if t.lt.keyNav(k) {
			return nil, true
		}
	case 2:
		switch k {
		case "enter", "down":
			t.setFocus(1)
			return nil, true
		case "esc":
			if t.q.Value() != "" {
				t.q.Set("")
				t.fill(m)
			} else {
				t.setFocus(1)
			}
			return nil, true
		}
		if ch, ok := t.q.key(msg); ok {
			if ch {
				t.fill(m)
			}
			return nil, true
		}
	case 3:
		if k == "esc" {
			t.setFocus(1)
			return nil, true
		}
		if t.prev.v.keyNav(k) {
			return nil, true
		}
	}
	return nil, false
}

func (t *browseTab) isOpen(n *treeNode) bool {
	if n.kind == "project" {
		return t.expanded["project"] || !t.expandedSet("project")
	}
	return t.expanded[n.id]
}

func (t *browseTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	mm := msg.Mouse()
	if d, ok := isWheel(msg); ok {
		switch {
		case t.areas.tree.has(mm.X, mm.Y):
			t.tt.scroll(d)
			t.fill(m)
		case t.areas.prev.has(mm.X, mm.Y):
			t.prev.v.scroll(d)
		default:
			t.lt.scroll(d)
		}
		return nil
	}
	c, ok := isClick(msg)
	if !ok {
		return nil
	}
	switch {
	case c.Y == t.q.area.y && t.areas.list.has(c.X, c.Y+1):
		t.setFocus(2)
	case t.areas.tree.has(c.X, c.Y):
		if i := t.tt.rowAt(c.X, c.Y); i >= 0 {
			t.setFocus(0)
			same := i == t.tt.cursor
			t.tt.cursor = i
			t.fill(m)
			if n := t.node(); n != nil && (n.kind == "domain" || n.kind == "project") && (same || c.X < t.areas.tree.x+5) {
				t.toggleNode(m)
			}
		}
	case t.areas.prev.has(c.X, c.Y):
		t.setFocus(3)
	case t.areas.list.has(c.X, c.Y):
		if i := t.lt.rowAt(c.X, c.Y); i >= 0 {
			t.setFocus(1)
			dbl := m.isDouble(c.X, c.Y)
			t.lt.cursor = i
			if c.X <= t.lt.area.x+1 {
				k := t.lt.current()
				if t.lt.sel[k] {
					delete(t.lt.sel, k)
				} else {
					t.lt.sel[k] = true
				}
			} else if dbl {
				return m.openSkill(t.lt.current())
			}
		}
	}
	return nil
}

func (t *browseTab) view(m *Model, r rect) []string {
	tw := min(max(26, r.w/4), 42)
	rest := r.w - tw
	lw := rest
	showPrev := r.w >= 110
	if showPrev {
		lw = rest * 55 / 100
	}
	pw := rest - lw
	t.areas.tree = rect{r.x, r.y, tw, r.h}
	treeLines := box("Domains", t.tt.render(rect{r.x + 1, r.y + 1, tw - 2, r.h - 2}), tw, r.h, t.focus == 0)

	n := t.node()
	title := ""
	if n != nil {
		title = n.title
		if n.kind == "stack" {
			title = m.d.tax.DomainTitle(n.domain) + " › " + n.title
		}
		if len(n.packs) > 0 {
			title += stDim.Render("  @" + strings.Join(n.packs, " @"))
		}
	}
	extra := ""
	if t.onlyNew {
		extra += " · not installed"
	}
	if s := len(t.lt.sel); s > 0 {
		extra += " · " + itoa(s) + " selected"
	}
	t.areas.list = rect{r.x + tw, r.y, lw, r.h}
	t.q.area = rect{r.x + tw + 1, r.y + 1, lw - 2, 1}
	inner := []string{t.q.render(lw-2, stAccentB.Render("/ "))}
	inner = append(inner, t.lt.render(rect{r.x + tw + 1, r.y + 2, lw - 2, r.h - 3})...)
	listLines := box(title+stDim.Render(" ("+itoa(len(t.items))+")"+extra), inner, lw, r.h, t.focus == 1 || t.focus == 2)
	if !showPrev {
		t.areas.prev = rect{}
		return hjoin(treeLines, listLines)
	}
	t.areas.prev = rect{r.x + tw + lw, r.y, pw, r.h}
	t.prev.show(m, t.lt.current(), pw-2)
	pl := box("Preview", t.prev.v.render(rect{r.x + tw + lw + 1, r.y + 1, pw - 2, r.h - 2}), pw, r.h, t.focus == 3)
	return hjoin(treeLines, listLines, pl)
}
