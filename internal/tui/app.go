// Package tui is the skillman terminal UI (Bubble Tea v2). Every list is virtualized, search runs on a
// prebuilt in-memory index, and every slow operation (status hashing, upstream checks, imports, installs)
// runs off the UI goroutine, so typing and scrolling never wait on disk or network.
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/hoangisreal/skillman/internal/core"
	"github.com/hoangisreal/skillman/internal/search"
)

// ───────────── shared data ─────────────

type data struct {
	rows     []core.IndexRow
	ix       *search.Index
	tax      *core.Taxonomy
	pbs      map[string][]string
	status   []core.SkillStatus
	st       map[string]core.SkillStatus
	sugg     []core.Suggestion
	detected map[string]bool
	idxMTime int64
	loaded   bool // project status computed
}

func (d *data) state(name string) string { return d.st[name].State }

func loadData(project string) *data {
	d := &data{st: map[string]core.SkillStatus{}}
	d.rows = core.LoadIndex()
	if d.rows == nil && core.IsDir(core.SKILLS) {
		core.RebuildIndex(true)
		d.rows = core.LoadIndex()
	}
	d.idxMTime = core.IndexMTime()
	d.tax = core.LoadTaxonomy()
	d.pbs = core.PacksBySkill()
	d.ix = search.Build(d.rows, d.pbs, nil, d.tax)
	return d
}

func (d *data) setStatus(st []core.SkillStatus, sugg []core.Suggestion) {
	d.status, d.st, d.loaded = st, core.StatusMap(st), true
	m := map[string]string{}
	for _, s := range st {
		m[s.Name] = s.State
	}
	d.ix.SetState(m)
	d.sugg = sugg
	d.detected = core.DetectedStacks(sugg, d.tax)
}

// ───────────── tabs, actions, modals ─────────────

type action struct {
	key, label string
	run        func(m *Model) tea.Cmd
	hidden     bool // not shown in the footer (still in help and palette)
}

type tab interface {
	title() string
	actions(m *Model) []action
	// key handles tab-local keys (navigation, focus, text input). Return handled=false to fall through to actions.
	key(m *Model, msg tea.KeyPressMsg) (tea.Cmd, bool)
	mouse(m *Model, msg tea.MouseMsg) tea.Cmd
	view(m *Model, r rect) []string
	refresh(m *Model) // shared data changed
	typing() bool     // a text input has focus: single-letter shortcuts must not fire
}

type modal interface {
	key(m *Model, msg tea.KeyPressMsg) tea.Cmd
	mouse(m *Model, msg tea.MouseMsg) tea.Cmd
	view(m *Model, w, h int) ([]string, rect) // lines + where to place them
	paste(m *Model, s string)
}

// ───────────── messages ─────────────

type statusMsg struct {
	st   []core.SkillStatus
	sugg []core.Suggestion
}

// doneMsg finishes an async job: notify, optionally reload data, then run `then`.
type doneMsg struct {
	note    string
	isErr   bool
	reload  bool
	reindex bool
	text    string // shown in a text modal when set
	title   string
	then    func(m *Model) tea.Cmd
}

type progressMsg string

type chanMsg struct {
	inner tea.Msg
	ch    chan tea.Msg
}

type clearNoteMsg struct{ id int }

type tickMsg struct{}

// ───────────── model ─────────────

type Model struct {
	project  string
	lang     language
	w, h     int
	d        *data
	tabs     []tab
	active   int
	modals   []modal
	note     string
	noteErr  bool
	noteID   int
	busy     int
	progress string
	spin     int
	tabZones []rect
	hintZone []struct {
		r rect
		a action
	}
	last struct {
		x, y int
		at   time.Time
	}
	trace    *os.File
	dragging func(x int)
	seen     []bool // tabs opened at least once (others load lazily)
}

func New(project string) *Model {
	m := &Model{project: project, lang: loadLanguage(), d: loadData(project)}
	m.tabs = []tab{newLibraryTab(m), newBrowseTab(m), newPacksTab(m), newProjectTab(m), newUpdatesTab(m),
		newUpstreamTab(m), newValidationTab(m), newTrashTab(m), newSettingsTab(m)}
	m.seen = make([]bool, len(m.tabs))
	m.seen[0] = true
	if p := os.Getenv("SKILLMAN_TRACE"); p != "" {
		m.trace, _ = os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	return m
}

// Run starts the TUI for a project directory.
func Run(project string) error {
	_, err := tea.NewProgram(New(project)).Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadStatus(), tick()}
	if probs := core.ValidatePacks(); len(probs) > 0 {
		bad := 0
		for _, p := range probs {
			if !strings.HasPrefix(p.Problem, "skill not in library") {
				bad++
			}
		}
		if bad > 0 {
			m.notify(m.lang.format("%d pack problem(s), e.g. %s: %s", bad, probs[0].Pack, probs[0].Problem), true)
		}
	}
	if len(m.d.rows) == 0 {
		m.notify(m.lang.format("library is empty: %s (run: skillman init, then the Upstream tab)", core.LIB), true)
	}
	if errs := m.d.tax.Errors; len(errs) > 0 {
		m.notify(m.lang.format("taxonomy: %s", errs[0]), true)
	}
	return tea.Batch(cmds...)
}

// noTimers disables spinner/notification timers (model tests drive Update synchronously).
var noTimers bool

func tick() tea.Cmd {
	if noTimers {
		return nil
	}
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) loadStatus() tea.Cmd {
	p := m.project
	return func() tea.Msg { return statusMsg{core.ProjectStatus(p), core.ProjectSignals(p)} }
}

func (m *Model) notify(s string, isErr bool) tea.Cmd {
	m.note, m.noteErr = strings.ReplaceAll(s, "\n", " · "), isErr
	m.noteID++
	id := m.noteID
	if noTimers {
		return nil
	}
	d := 5 * time.Second
	if isErr {
		d = 9 * time.Second
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return clearNoteMsg{id} })
}

// async runs fn off the UI goroutine. fn may report progress; its return value is delivered as a message.
func (m *Model) async(label string, fn func(progress func(string)) tea.Msg) tea.Cmd {
	m.busy++
	m.progress = label
	ch := make(chan tea.Msg, 64)
	go func() {
		defer close(ch)
		res := fn(func(s string) {
			select {
			case ch <- progressMsg(s):
			default: // drop progress when the UI is behind; the final result always arrives
			}
		})
		ch <- res
	}()
	return listen(ch)
}

func listen(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return nil
		}
		return chanMsg{v, ch}
	}
}

// reload refreshes shared data after a change (index only when it was rebuilt) and recomputes status async.
func (m *Model) reload(reindex bool) tea.Cmd {
	if reindex || core.IndexMTime() != m.d.idxMTime {
		st, sugg := m.d.status, m.d.sugg
		m.d = loadData(m.project)
		m.d.setStatus(st, sugg)
	} else {
		m.d.pbs = core.PacksBySkill()
		m.d.ix = search.Build(m.d.rows, m.d.pbs, nil, m.d.tax)
		if m.d.loaded {
			m.d.setStatus(m.d.status, m.d.sugg)
		}
	}
	m.refreshTabs()
	return m.loadStatus()
}

// refreshTabs refreshes the tabs that were opened; the others refresh when first shown.
func (m *Model) refreshTabs() {
	for i, t := range m.tabs {
		if m.seen[i] {
			t.refresh(m)
		}
	}
}

func (m *Model) push(md modal) { m.modals = append(m.modals, md) }
func (m *Model) pop() {
	if len(m.modals) > 0 {
		m.modals = m.modals[:len(m.modals)-1]
	}
}

func (m *Model) isDouble(x, y int) bool {
	now := time.Now()
	d := m.last.y == y && abs(m.last.x-x) <= 3 && now.Sub(m.last.at) < 450*time.Millisecond
	m.last.x, m.last.y, m.last.at = x, y, now
	if d {
		m.last.at = time.Time{}
	}
	return d
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (m *Model) setTab(i int) {
	if i >= 0 && i < len(m.tabs) {
		m.active = i
		m.seen[i] = true
		m.tabs[i].refresh(m)
	}
}

func (m *Model) tab() tab { return m.tabs[m.active] }

func (m *Model) allActions() []action {
	acts := m.tab().actions(m)
	return append(acts,
		action{key: "?", label: "Help", run: func(m *Model) tea.Cmd { m.push(newHelpModal(m)); return nil }, hidden: true},
		action{key: "ctrl+p", label: "Commands", run: func(m *Model) tea.Cmd { m.push(newPalette(m)); return nil }, hidden: true},
		action{key: "ctrl+r", label: "Reload", run: func(m *Model) tea.Cmd { m.notify(m.lang.text("reloaded"), false); return m.reload(true) }, hidden: true},
		action{key: "q", label: "Quit", run: func(m *Model) tea.Cmd { return tea.Quit }, hidden: true},
	)
}

// ───────────── update ─────────────

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var t0 time.Time
	if m.trace != nil {
		t0 = time.Now()
	}
	cmd := m.update(msg)
	if m.trace != nil {
		fmt.Fprintf(m.trace, "update %T %d\n", msg, time.Since(t0).Microseconds())
	}
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return nil
	case tickMsg:
		if m.busy > 0 {
			m.spin++
		}
		return tick()
	case clearNoteMsg:
		if msg.id == m.noteID {
			m.note = ""
		}
		return nil
	case chanMsg:
		if p, ok := msg.inner.(progressMsg); ok {
			m.progress = string(p)
			return listen(msg.ch)
		}
		m.busy = max(0, m.busy-1)
		if m.busy == 0 {
			m.progress = ""
		}
		return tea.Batch(m.update(msg.inner), listen(msg.ch))
	case statusMsg:
		m.d.setStatus(msg.st, msg.sugg)
		m.refreshTabs()
		return nil
	case doneMsg:
		var cmds []tea.Cmd
		if msg.note != "" {
			cmds = append(cmds, m.notify(msg.note, msg.isErr))
		}
		if msg.reload || msg.reindex {
			cmds = append(cmds, m.reload(msg.reindex))
		}
		if msg.text != "" {
			m.push(newTextModal(m.lang.text(msg.title), msg.text))
		}
		if msg.then != nil {
			cmds = append(cmds, msg.then(m))
		}
		return tea.Batch(cmds...)
	case tea.PasteMsg:
		if len(m.modals) > 0 {
			m.modals[len(m.modals)-1].paste(m, msg.Content)
			return nil
		}
		if p, ok := m.tab().(paster); ok {
			return p.paste(m, msg.Content)
		}
		return nil
	case tea.KeyPressMsg:
		return m.onKey(msg)
	case tea.MouseMsg:
		return m.onMouse(msg)
	}
	var cmds []tea.Cmd
	for _, t := range m.tabs {
		if h, ok := t.(handler); ok {
			cmds = append(cmds, h.handle(m, msg))
		}
	}
	return tea.Batch(cmds...)
}

// handler receives tab-specific async results (e.g. update checks).
type handler interface {
	handle(m *Model, msg tea.Msg) tea.Cmd
}

type paster interface {
	paste(m *Model, s string) tea.Cmd
}

func (m *Model) onKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return tea.Quit
	}
	if len(m.modals) > 0 {
		return m.modals[len(m.modals)-1].key(m, msg)
	}
	t := m.tab()
	if t.typing() {
		if cmd, ok := t.key(m, msg); ok {
			return cmd
		}
	}
	if !t.typing() {
		if len(k) == 1 && k[0] >= '1' && k[0] <= '0'+byte(len(m.tabs)) {
			m.setTab(int(k[0] - '1'))
			return nil
		}
	}
	switch {
	case k == "alt+right" || k == "ctrl+right" || (k == "]" && !t.typing()):
		m.setTab((m.active + 1) % len(m.tabs))
		return nil
	case k == "alt+left" || k == "ctrl+left" || (k == "[" && !t.typing()):
		m.setTab((m.active + len(m.tabs) - 1) % len(m.tabs))
		return nil
	}
	if !t.typing() {
		if cmd, ok := t.key(m, msg); ok {
			return cmd
		}
	}
	for _, a := range m.allActions() {
		if a.key == k && (!t.typing() || strings.HasPrefix(k, "ctrl+")) {
			return a.run(m)
		}
	}
	return nil
}

func (m *Model) onMouse(msg tea.MouseMsg) tea.Cmd {
	mm := msg.Mouse()
	if _, ok := msg.(tea.MouseReleaseMsg); ok {
		m.dragging = nil
	}
	if mo, ok := msg.(tea.MouseMotionMsg); ok {
		if m.dragging != nil && mo.Button == tea.MouseLeft {
			m.dragging(mm.X)
		}
		return nil
	}
	if len(m.modals) > 0 {
		return m.modals[len(m.modals)-1].mouse(m, msg)
	}
	if c, ok := msg.(tea.MouseClickMsg); ok && c.Button == tea.MouseLeft {
		for i, z := range m.tabZones {
			if z.has(mm.X, mm.Y) {
				m.setTab(i)
				return nil
			}
		}
		for _, hz := range m.hintZone {
			if hz.r.has(mm.X, mm.Y) {
				return hz.a.run(m)
			}
		}
	}
	return m.tab().mouse(m, msg)
}

// ───────────── view ─────────────

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *Model) View() tea.View {
	var t0 time.Time
	if m.trace != nil {
		t0 = time.Now()
	}
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "skillman · " + m.project
	if m.trace != nil {
		fmt.Fprintf(m.trace, "view %d\n", time.Since(t0).Microseconds())
	}
	return v
}

func (m *Model) render() string {
	if m.w == 0 || m.h == 0 {
		return m.lang.text("loading…")
	}
	if m.w < 20 || m.h < 6 { // too small to draw panes: say so instead of a garbled frame
		msg := []string{fit(m.lang.text("skillman: terminal too small"), m.w)}
		for len(msg) < m.h {
			msg = append(msg, strings.Repeat(" ", m.w))
		}
		return strings.Join(msg[:m.h], "\n")
	}
	w, h := m.w, m.h
	lines := make([]string, 0, h)
	// tab bar
	m.tabZones = m.tabZones[:0]
	var tb strings.Builder
	titles := make([]string, len(m.tabs))
	total := 0
	for i, t := range m.tabs {
		titles[i] = m.lang.text(t.title())
		total += 4 + ansi.StringWidth(titles[i])
	}
	// Compact tabs drop one padding space. Give remaining cells to Settings, the final tab.
	available := w - 3*len(titles)
	titleWidth := max(0, available/len(titles))
	spare := available
	for _, title := range titles {
		spare -= min(titleWidth, ansi.StringWidth(title))
	}
	x := 0
	for i, title := range titles {
		number := itoa(i+1) + " "
		label := " " + number + title + " "
		if total > w {
			label = number
			if titleWidth > 0 {
				limit := titleWidth
				if i == len(titles)-1 {
					limit += spare
				}
				label += ansi.Truncate(title, limit, "…") + " "
			}
		}
		lw := ansi.StringWidth(label)
		if i == m.active {
			tb.WriteString(stTabOn.Render(label))
		} else {
			tb.WriteString(stTabOff.Render(label))
		}
		m.tabZones = append(m.tabZones, rect{x, 0, lw, 1})
		x += lw
	}
	right := stDim.Render(" " + tilde(core.LIB) + " ")
	bar := tb.String()
	if x+ansi.StringWidth(right) < w {
		bar += strings.Repeat(" ", w-x-ansi.StringWidth(right)) + right
	}
	lines = append(lines, fit(bar, w))
	body := rect{0, 1, w, h - 3}
	if body.h < 1 {
		body.h = 1
	}
	bl := m.tab().view(m, body)
	for i := 0; i < body.h; i++ {
		if i < len(bl) {
			lines = append(lines, fit(bl[i], w))
		} else {
			lines = append(lines, strings.Repeat(" ", w))
		}
	}
	// status line
	st := ""
	switch {
	case m.busy > 0:
		st = stAccent.Render(spinner[m.spin%len(spinner)]+" ") + m.progress
	case m.note != "":
		if m.noteErr {
			st = stErr.Render("! ") + m.note
		} else {
			st = stOK.Render("✓ ") + m.note
		}
	case len(m.d.rows) == 1 && m.d.rows[0].Name == core.SelfSkill:
		st = stDim.Render(m.lang.text("Load starter skills.") + " " + m.lang.text(core.PackImportHint("core")))
	}
	proj := stDim.Render(" " + m.lang.format("project: %s", tilde(m.project)))
	if ansi.StringWidth(st)+ansi.StringWidth(proj) < w {
		st = fit(st, w-ansi.StringWidth(proj)) + proj
	}
	lines = append(lines, fit(st, w))
	lines = append(lines, m.footer(w))
	out := strings.Join(lines, "\n")
	if len(m.modals) > 0 {
		out = m.overlay(lines)
	}
	return out
}

func (m *Model) footer(w int) string {
	m.hintZone = m.hintZone[:0]
	var sb strings.Builder
	x := 0
	help := action{key: "?", label: "help", run: func(m *Model) tea.Cmd { m.push(newHelpModal(m)); return nil }}
	palette := action{key: "ctrl+p", label: "commands", run: func(m *Model) tea.Cmd { m.push(newPalette(m)); return nil }}
	add := func(a action, limit int) bool {
		seg := stKey.Render(a.key) + " " + m.lang.text(a.label) + "  "
		sw := ansi.StringWidth(seg)
		if x+sw > limit {
			return false
		}
		sb.WriteString(seg)
		m.hintZone = append(m.hintZone, struct {
			r rect
			a action
		}{rect{x, m.h - 1, sw - 2, 1}, a})
		x += sw
		return true
	}
	limit := w - ansi.StringWidth("? "+m.lang.text(help.label)+"  ")
	for _, a := range m.tab().actions(m) {
		if !a.hidden && !add(a, limit) {
			break
		}
	}
	add(help, w)
	add(palette, w)
	return fit(sb.String(), w)
}

// overlay draws the top modal centered over the screen lines.
func (m *Model) overlay(base []string) string {
	md := m.modals[len(m.modals)-1]
	ml, r := md.view(m, m.w, m.h)
	for i, l := range ml {
		y := r.y + i
		if y < 0 || y >= len(base) {
			continue
		}
		row := base[y]
		left := ansi.Truncate(row, r.x, "")
		left += strings.Repeat(" ", max(0, r.x-ansi.StringWidth(left)))
		rightPart := ansi.Cut(row, r.x+r.w, m.w)
		base[y] = left + fit(l, r.w) + rightPart
	}
	return strings.Join(base, "\n")
}

// tilde shortens paths under $HOME for display.
func tilde(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && h != "/" && strings.HasPrefix(p, h+"/") {
		return "~" + p[len(h):]
	}
	return p
}
