package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/hoangisreal/skillman/internal/core"
	"github.com/hoangisreal/skillman/internal/search"
)

// ───────────── preview pane (cached Markdown render) ─────────────

type preview struct {
	v     viewer
	key   string
	width int
	cache map[string][]string
	order []string
}

func (p *preview) show(m *Model, name string, w int) {
	if name == p.key && w == p.width {
		return
	}
	p.key, p.width = name, w
	if name == "" {
		p.v.set(nil)
		return
	}
	ck := name + "\x00" + itoa(w)
	if p.cache == nil {
		p.cache = map[string][]string{}
	}
	lines, ok := p.cache[ck]
	if !ok {
		head := []string{stBold.Render(name) + "  " + stDim.Render(m.d.sourceLine(name))}
		if doc := m.d.ix.Get(name); doc != nil {
			cls := m.d.tax.DomainTitle(doc.Row.Domain)
			if len(doc.Row.Stacks) > 0 {
				var ts []string
				for _, s := range doc.Row.Stacks {
					if st := m.d.tax.Stack(s); st != nil {
						ts = append(ts, st.Title)
					}
				}
				cls += " › " + strings.Join(ts, ", ")
			}
			head = append(head, stDim.Render(cls))
			if len(doc.StackPacks) > 0 {
				head = append(head, stDim.Render(m.lang.format("packs: %s", "@"+strings.Join(doc.StackPacks, " @"))))
			}
			if st := m.d.state(name); st != "" {
				head = append(head, m.lang.format("project: %s", m.lang.styled(st)))
			}
		}
		text, err := core.ReadText(filepath.Join(core.SKILLS, name, "SKILL.md"))
		if err != nil {
			text = m.lang.text("(missing SKILL.md)")
		}
		lines = append(head, renderMarkdown(m.lang, text, w, 16000)...)
		p.cache[ck] = lines
		p.order = append(p.order, ck)
		if len(p.order) > 256 { // small LRU-ish bound
			delete(p.cache, p.order[0])
			p.order = p.order[1:]
		}
	}
	p.v.set(lines)
}

func (p *preview) invalidate() { p.cache, p.order, p.key = nil, nil, "" }

// ───────────── 1 Library ─────────────

type libraryTab struct {
	q     input
	tbl   *table
	hits  []int
	focus int // 0 search, 1 list, 2 preview
	prev  preview
	split float64
	divX  int
	lastQ string
}

const defaultLibrarySplit = 0.58

func newLibraryTab(m *Model) *libraryTab {
	t := &libraryTab{split: defaultLibrarySplit, focus: 1}
	t.q.placeholder = "search: words · -word · tag:web · src:redis · pack:core · domain:backend · stack:vue · nopack · installed"
	t.tbl = newTable(column{title: " ", width: 1}, column{title: "skill", flex: 5}, column{title: "domain › stack", flex: 4},
		column{title: "source", flex: 4}, column{title: "project", width: 9}, column{title: "packs", flex: 3})
	t.tbl.focused = true
	t.tbl.empty = "no skills match"
	t.tbl.keyOf = func(i int) string { return m.d.ix.Docs[t.hits[i]].Row.Name }
	t.tbl.cell = func(i, c int) string {
		d := &m.d.ix.Docs[t.hits[i]]
		switch c {
		case 0:
			return t.tbl.mark(i)
		case 1:
			return d.Row.Name
		case 2:
			s := d.Row.Domain
			if len(d.Row.Stacks) > 0 {
				s += " › " + strings.Join(d.Row.Stacks, ",")
			}
			return stDim.Render(s)
		case 3:
			return d.Row.Source
		case 4:
			return m.lang.styled(d.State)
		case 5:
			if len(d.StackPacks) > 2 {
				return strings.Join(d.StackPacks[:2], ",") + "…"
			}
			return strings.Join(d.StackPacks, ",")
		}
		return ""
	}
	t.apply(m)
	return t
}

func (t *libraryTab) title() string { return "Library" }
func (t *libraryTab) typing() bool  { return t.focus == 0 }

func (t *libraryTab) apply(m *Model) {
	keep := t.tbl.current()
	t.hits = m.d.ix.Search(search.Parse(t.q.Value()), nil)
	t.tbl.n = len(t.hits)
	t.tbl.cursor = 0
	if keep != "" && t.q.Value() == t.lastQ {
		for i, h := range t.hits {
			if m.d.ix.Docs[h].Row.Name == keep {
				t.tbl.cursor = i
				break
			}
		}
	}
	t.lastQ = t.q.Value()
	t.tbl.clamp()
}

func (t *libraryTab) refresh(m *Model) {
	t.apply(m)
	t.prev.invalidate()
}

func (t *libraryTab) setFocus(f int) {
	t.focus = f
	t.q.focused = f == 0
	t.tbl.focused = f == 1
}

func (t *libraryTab) actions(m *Model) []action {
	clear := func() { t.tbl.sel = map[string]bool{} }
	return []action{
		{key: "/", label: "search", run: func(m *Model) tea.Cmd { t.setFocus(0); return nil }},
		{key: "space", label: "select", run: func(m *Model) tea.Cmd { t.tbl.toggle(); return nil }},
		{key: "i", label: "install", run: func(m *Model) tea.Cmd {
			var items []core.Item
			for _, n := range t.tbl.targets() {
				items = append(items, core.Item{Name: n})
			}
			return m.installItems(items, m.lang.format("%d skill(s)", len(items)), clear)
		}},
		{key: "x", label: "remove", run: func(m *Model) tea.Cmd {
			var names []string
			for _, n := range t.tbl.targets() {
				if m.d.state(n) != "" {
					names = append(names, n)
				}
			}
			if len(names) == 0 {
				return m.notify(m.lang.text("none of these skills is in the project"), true)
			}
			return m.uninstallSkills(names, clear)
		}},
		{key: "p", label: "add to pack", run: func(m *Model) tea.Cmd { return m.addToPack(t.tbl.targets(), clear) }},
		{key: "enter", label: "open", run: func(m *Model) tea.Cmd { return m.openSkill(t.tbl.current()) }},
		{key: "w", label: "warnings", run: func(m *Model) tea.Cmd { return m.showWarnings(t.tbl.current()) }},
		{key: "ctrl+a", label: "select all shown", run: func(m *Model) tea.Cmd { t.tbl.selectAll(); return nil }, hidden: true},
		{key: "esc", label: "clear selection", run: func(m *Model) tea.Cmd { clear(); return nil }, hidden: true},
	}
}

func (t *libraryTab) key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	switch t.focus {
	case 0:
		switch k {
		case "enter", "down", "tab":
			t.setFocus(1)
			return nil, true
		case "esc":
			if t.q.Value() != "" {
				t.q.Set("")
				t.apply(m)
			} else {
				t.setFocus(1)
			}
			return nil, true
		case "shift+tab":
			t.setFocus(2)
			return nil, true
		case "ctrl+a":
			t.tbl.selectAll()
			return nil, true
		}
		if ch, ok := t.q.key(msg); ok {
			if ch {
				t.apply(m)
			}
			return nil, true
		}
		return nil, false
	case 1:
		switch k {
		case "tab":
			t.setFocus(2)
			return nil, true
		case "shift+tab":
			t.setFocus(0)
			return nil, true
		}
		if t.tbl.keyNav(k) {
			return nil, true
		}
	case 2:
		switch k {
		case "tab", "esc":
			t.setFocus(0)
			return nil, true
		case "shift+tab":
			t.setFocus(1)
			return nil, true
		}
		if t.prev.v.keyNav(k) {
			return nil, true
		}
	}
	return nil, false
}

func (t *libraryTab) paste(m *Model, s string) tea.Cmd {
	t.setFocus(0)
	t.q.insert(s)
	t.apply(m)
	return nil
}

func (t *libraryTab) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	mm := msg.Mouse()
	if d, ok := isWheel(msg); ok {
		if t.prev.v.area.has(mm.X, mm.Y) {
			t.prev.v.scroll(d)
		} else {
			t.tbl.scroll(d)
		}
		return nil
	}
	c, ok := isClick(msg)
	if !ok {
		return nil
	}
	switch {
	case c.Y == t.q.area.y:
		t.setFocus(0)
	case abs(c.X-t.divX) <= 1 && c.Y > t.q.area.y:
		x0, w := t.tbl.area.x, m.w
		initial := t.split
		m.dragging = func(x int) { t.split = clampF(float64(x-x0)/float64(w), 0.25, 0.85) }
		m.endDrag = func() tea.Cmd {
			if t.split == initial {
				return nil
			}
			if err := core.SavePreference("library_split", t.split); err != nil {
				return m.notify(m.lang.format("Could not save setting: %v", err), true)
			}
			return nil
		}
	case t.prev.v.area.has(c.X, c.Y):
		t.setFocus(2)
	default:
		if i := t.tbl.rowAt(c.X, c.Y); i >= 0 {
			t.setFocus(1)
			dbl := m.isDouble(c.X, c.Y)
			t.tbl.cursor = i
			if c.X <= t.tbl.area.x+1 {
				k := t.tbl.current()
				if t.tbl.sel[k] {
					delete(t.tbl.sel, k)
				} else {
					t.tbl.sel[k] = true
				}
			} else if dbl {
				return m.openSkill(t.tbl.current())
			}
		}
	}
	return nil
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (t *libraryTab) view(m *Model, r rect) []string {
	info := " " + itoa(len(t.hits)) + "/" + itoa(len(m.d.ix.Docs))
	if n := len(t.tbl.sel); n > 0 {
		info += " · " + m.lang.format("%d selected", n)
	}
	t.q.area = rect{r.x, r.y, r.w, 1}
	qline := t.q.render(m.lang, r.w-len([]rune(info))-1, stAccentB.Render("/ ")) + " " + stDim.Render(info)
	if len(t.tbl.sel) > 0 {
		qline = t.q.render(m.lang, r.w-len([]rune(info))-1, stAccentB.Render("/ ")) + " " + stSel.Render(info)
	}
	lw := int(float64(r.w) * t.split)
	if r.w < 90 { // narrow terminal: list only, preview on enter
		lw = r.w
	}
	bodyH := r.h - 1
	listLines := t.tbl.render(m.lang, rect{r.x, r.y + 1, lw, bodyH})
	out := []string{fit(qline, r.w)}
	if lw >= r.w {
		t.divX = -10
		t.prev.v.area = rect{}
		return append(out, listLines...)
	}
	t.divX = r.x + lw
	pw := r.w - lw - 1
	t.prev.show(m, t.tbl.current(), pw-1)
	pl := t.prev.v.render(rect{r.x + lw + 2, r.y + 1, pw - 1, bodyH})
	div := make([]string, bodyH)
	for i := range div {
		if t.focus == 2 {
			div[i] = stAccent.Render("│")
		} else {
			div[i] = stBorder.Render("│")
		}
	}
	for i := range pl {
		pl[i] = " " + pl[i]
	}
	return append(out, hjoin(listLines, div, pl)...)
}
