package tui

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func center(w, h, mw, mh int) rect {
	mw, mh = max(4, min(mw, w-2)), max(3, min(mh, h-2))
	return rect{(w - mw) / 2, (h - mh) / 2, mw, mh}
}

func isWheel(msg tea.MouseMsg) (int, bool) {
	if wm, ok := msg.(tea.MouseWheelMsg); ok {
		switch wm.Button {
		case tea.MouseWheelUp:
			return -3, true
		case tea.MouseWheelDown:
			return 3, true
		}
	}
	return 0, false
}

func isClick(msg tea.MouseMsg) (tea.Mouse, bool) {
	if c, ok := msg.(tea.MouseClickMsg); ok && c.Button == tea.MouseLeft {
		return c.Mouse(), true
	}
	return tea.Mouse{}, false
}

// ───────────── confirm ─────────────

type confirmModal struct {
	msg    string
	danger bool
	yes    func(m *Model) tea.Cmd
	no     func(m *Model) tea.Cmd
	focus  int // 0 yes, 1 no
	btnYes rect
	btnNo  rect
	area   rect
}

func confirm(msg string, danger bool, yes func(m *Model) tea.Cmd) *confirmModal {
	c := &confirmModal{msg: msg, danger: danger, yes: yes}
	if danger {
		c.focus = 1
	}
	return c
}

func (c *confirmModal) answer(m *Model, ok bool) tea.Cmd {
	m.pop()
	if ok && c.yes != nil {
		return c.yes(m)
	}
	if !ok && c.no != nil {
		return c.no(m)
	}
	return nil
}

func (c *confirmModal) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "y", "Y":
		return c.answer(m, true)
	case "n", "N", "esc", "q":
		return c.answer(m, false)
	case "left", "right", "tab", "shift+tab", "h", "l":
		c.focus = 1 - c.focus
	case "enter", "space":
		return c.answer(m, c.focus == 0)
	}
	return nil
}

func (c *confirmModal) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	if mm, ok := isClick(msg); ok {
		switch {
		case c.btnYes.has(mm.X, mm.Y):
			return c.answer(m, true)
		case c.btnNo.has(mm.X, mm.Y):
			return c.answer(m, false)
		case !c.area.has(mm.X, mm.Y):
			return c.answer(m, false)
		}
	}
	return nil
}

func (c *confirmModal) paste(*Model, string) {}

func (c *confirmModal) view(m *Model, w, h int) ([]string, rect) {
	mw := min(max(50, w*2/3), 100)
	body := wrapPlain(c.msg, mw-4)
	if len(body) > h-8 {
		body = append(body[:h-9], "…")
	}
	r := center(w, h, mw, len(body)+5)
	c.area = r
	yes, no := " Yes (y) ", " No (n) "
	ys, ns := stTabOff, stTabOff
	if c.focus == 0 {
		ys = stTabOn
	} else {
		ns = stTabOn
	}
	if c.danger && c.focus == 0 {
		ys = stTabOn.Foreground(stErr.GetForeground())
	}
	inner := append([]string{}, body...)
	inner = append(inner, "", ys.Render(yes)+"  "+ns.Render(no))
	by := r.y + 1 + len(body) + 1
	c.btnYes = rect{r.x + 1, by, len(yes), 1}
	c.btnNo = rect{r.x + 1 + len(yes) + 2, by, len(no), 1}
	title := "Confirm"
	if c.danger {
		title = stErr.Render("Confirm")
	}
	return box(title, inner, r.w, r.h, true), r
}

// ───────────── ask (text input) ─────────────

type askModal struct {
	prompt string
	in     input
	submit func(m *Model, v string) tea.Cmd
}

func ask(prompt, value, placeholder string, submit func(m *Model, v string) tea.Cmd) *askModal {
	a := &askModal{prompt: prompt, submit: submit}
	a.in.Set(value)
	a.in.placeholder = placeholder
	a.in.focused = true
	return a
}

func (a *askModal) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.pop()
		return nil
	case "enter":
		m.pop()
		return a.submit(m, strings.TrimSpace(a.in.Value()))
	}
	a.in.key(msg)
	return nil
}

func (a *askModal) mouse(m *Model, msg tea.MouseMsg) tea.Cmd { return nil }
func (a *askModal) paste(m *Model, s string)                 { a.in.insert(s) }

func (a *askModal) view(m *Model, w, h int) ([]string, rect) {
	r := center(w, h, min(max(60, w*2/3), 110), 6)
	inner := []string{a.prompt, a.in.render(r.w-2, "› "), "", stDim.Render("enter = ok · esc = cancel")}
	return box("Input", inner, r.w, r.h, true), r
}

// ───────────── pick (filterable list) ─────────────

type option struct{ value, label string }

type pickModal struct {
	title    string
	opts     []option
	shown    []int
	filter   input
	list     *table
	pick     func(m *Model, v string) tea.Cmd
	pickMany func(m *Model, names []string) tea.Cmd
	area     rect
}

func pick(title string, opts []option, fn func(m *Model, v string) tea.Cmd) *pickModal {
	p := &pickModal{title: title, opts: opts, pick: fn}
	p.filter.focused = true
	p.filter.placeholder = "type to filter · ↑↓ · enter = choose · esc = cancel"
	p.list = newTable(column{title: "", flex: 1})
	p.list.focused = true
	p.list.cell = func(i, c int) string {
		o := p.opts[p.shown[i]]
		if p.pickMany != nil {
			return p.list.mark(i) + " " + stBold.Render(o.value) + "  " + stDim.Render(o.label)
		}
		if o.label == "" {
			return o.value
		}
		return stBold.Render(o.value) + "  " + stDim.Render(o.label)
	}
	p.list.keyOf = func(i int) string { return p.opts[p.shown[i]].value }
	p.apply()
	return p
}

func (p *pickModal) apply() {
	words := strings.Fields(strings.ToLower(p.filter.Value()))
	p.shown = p.shown[:0]
	for i, o := range p.opts {
		hay := strings.ToLower(o.value + " " + o.label)
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(hay, w)
		}
		if ok {
			p.shown = append(p.shown, i)
		}
	}
	if len(words) > 0 { // names starting with the first word first
		sort.SliceStable(p.shown, func(a, b int) bool {
			pa := strings.HasPrefix(strings.ToLower(p.opts[p.shown[a]].value), words[0])
			pb := strings.HasPrefix(strings.ToLower(p.opts[p.shown[b]].value), words[0])
			return pa && !pb
		})
	}
	p.list.n = len(p.shown)
	p.list.cursor = 0
	p.list.offset = 0
}

func (p *pickModal) choose(m *Model) tea.Cmd {
	if p.pickMany != nil {
		names := p.list.targets()
		if len(names) == 0 {
			return nil
		}
		m.pop()
		return p.pickMany(m, names)
	}
	v := p.list.current()
	if v == "" {
		return nil
	}
	m.pop()
	return p.pick(m, v)
}

func (p *pickModal) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if p.pickMany != nil && k == "space" {
		p.list.toggle()
		return nil
	}
	switch k {
	case "esc":
		m.pop()
		return nil
	case "enter":
		return p.choose(m)
	case "up", "ctrl+p":
		p.list.move(-1)
		return nil
	case "down", "ctrl+n", "tab":
		p.list.move(1)
		return nil
	case "pgup", "pgdown":
		p.list.keyNav(k)
		return nil
	}
	if ch, _ := p.filter.key(msg); ch {
		p.apply()
	}
	return nil
}

func (p *pickModal) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	if d, ok := isWheel(msg); ok {
		p.list.scroll(d)
		return nil
	}
	if mm, ok := isClick(msg); ok {
		if !p.area.has(mm.X, mm.Y) {
			m.pop()
			return nil
		}
		if i := p.list.rowAt(mm.X, mm.Y); i >= 0 { // one click chooses
			p.list.cursor = i
			if p.pickMany != nil {
				p.list.toggle()
				return nil
			}
			return p.choose(m)
		}
	}
	return nil
}

func (p *pickModal) paste(m *Model, s string) { p.filter.insert(s); p.apply() }

func (p *pickModal) view(m *Model, w, h int) ([]string, rect) {
	r := center(w, h, min(max(70, w*3/4), 140), h*4/5)
	p.area = r
	inner := []string{p.filter.render(r.w-2, "/ ")}
	lr := rect{r.x + 1, r.y + 2, r.w - 2, r.h - 3}
	inner = append(inner, p.list.render(lr)...)
	return box(p.title+stDim.Render(" ("+itoa(len(p.shown))+")"), inner, r.w, r.h, true), r
}

// ───────────── text viewer ─────────────

type textModal struct {
	title string
	text  string
	v     viewer
	width int
	area  rect
}

func newTextModal(title, text string) *textModal { return &textModal{title: title, text: text} }

func (t *textModal) key(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "enter":
		m.pop()
		return nil
	}
	t.v.keyNav(msg.String())
	return nil
}

func (t *textModal) mouse(m *Model, msg tea.MouseMsg) tea.Cmd {
	if d, ok := isWheel(msg); ok {
		t.v.scroll(d)
	}
	if mm, ok := isClick(msg); ok && !t.area.has(mm.X, mm.Y) {
		m.pop()
	}
	return nil
}

func (t *textModal) paste(*Model, string) {}

func (t *textModal) view(m *Model, w, h int) ([]string, rect) {
	r := center(w, h, w-4, h-2)
	t.area = r
	if t.width != r.w-2 {
		t.width = r.w - 2
		off := t.v.offset
		t.v.set(colorDiff(wrapPlain(t.text, t.width)))
		t.v.offset = off
	}
	inner := t.v.render(rect{r.x + 1, r.y + 1, r.w - 2, r.h - 3})
	inner = append(inner, stDim.Render("↑↓ PgUp PgDn wheel · esc / q / enter = close"))
	return box(t.title, inner, r.w, r.h, true), r
}

// colorDiff colors unified-diff lines; other text passes through.
func colorDiff(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---"):
			out[i] = stBold.Render(l)
		case strings.HasPrefix(l, "@@"):
			out[i] = stAccent.Render(l)
		case strings.HasPrefix(l, "+"):
			out[i] = stOK.Render(l)
		case strings.HasPrefix(l, "-"):
			out[i] = stErr.Render(l)
		case strings.HasPrefix(l, "✗") || strings.HasPrefix(l, "ERROR"):
			out[i] = stErr.Render(l)
		case strings.HasPrefix(l, "!") || strings.HasPrefix(l, "WARN"):
			out[i] = stWarn.Render(l)
		default:
			out[i] = l
		}
	}
	return out
}

// markdown viewer (full SKILL.md)
type mdModal struct {
	textModal
	raw string
}

func newMDModal(title, raw string) *mdModal {
	return &mdModal{textModal: textModal{title: title}, raw: raw}
}

func (t *mdModal) view(m *Model, w, h int) ([]string, rect) {
	r := center(w, h, min(w-4, 120), h-2)
	t.area = r
	if t.width != r.w-2 {
		t.width = r.w - 2
		t.v.set(renderMarkdown(t.raw, t.width, 0))
	}
	inner := t.v.render(rect{r.x + 1, r.y + 1, r.w - 2, r.h - 3})
	inner = append(inner, stDim.Render("↑↓ PgUp PgDn wheel · esc / q = close"))
	return box(t.title, inner, r.w, r.h, true), r
}

func (t *mdModal) key(m *Model, msg tea.KeyPressMsg) tea.Cmd { return t.textModal.key(m, msg) }
func (t *mdModal) mouse(m *Model, msg tea.MouseMsg) tea.Cmd  { return t.textModal.mouse(m, msg) }

// ───────────── help + command palette ─────────────

func newHelpModal(m *Model) *textModal {
	var b strings.Builder
	b.WriteString("Global\n")
	for _, l := range [][2]string{
		{"1-8 · [ ]  · alt+←/→", "switch tab (or click a tab)"},
		{"tab / shift+tab", "move focus between panes"},
		{"/", "search / filter"},
		{"↑↓ j k · PgUp PgDn · g G", "move (mouse wheel scrolls the pane under the pointer)"},
		{"space · click ●", "select row (multi-select) · ctrl+a select all shown · esc clear"},
		{"enter · double-click", "primary action / open"},
		{"ctrl+p", "command palette: every action of this tab, searchable"},
		{"ctrl+r", "reload library, packs and project status"},
		{"q · ctrl+c", "quit"},
	} {
		b.WriteString("  " + fit(l[0], 28) + " " + l[1] + "\n")
	}
	b.WriteString("\n" + m.tab().title() + " tab\n")
	for _, a := range m.tab().actions(m) {
		b.WriteString("  " + fit(a.key, 28) + " " + a.label + "\n")
	}
	b.WriteString("\nThe footer keys are clickable. Drag the │ divider to resize panes.\n" +
		"Everything here is also a CLI command: skillman help")
	return newTextModal("Help", b.String())
}

func newPalette(m *Model) *pickModal {
	var opts []option
	acts := map[string]action{}
	for _, a := range m.allActions() {
		id := a.key + " " + a.label
		acts[id] = a
		opts = append(opts, option{value: a.label, label: "[" + a.key + "]"})
		acts[a.label] = a
	}
	for i, t := range m.tabs {
		i := i
		lbl := "Go to " + t.title()
		acts[lbl] = action{run: func(m *Model) tea.Cmd { m.setTab(i); return nil }}
		opts = append(opts, option{value: lbl, label: "[" + itoa(i+1) + "]"})
	}
	return pick("Commands", opts, func(m *Model, v string) tea.Cmd {
		if a, ok := acts[v]; ok && a.run != nil {
			return a.run(m)
		}
		return nil
	})
}
