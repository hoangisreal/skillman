package tui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ───────────── styles (ANSI 16 colors: follow the terminal theme) ─────────────

var (
	stBold      = lipgloss.NewStyle().Bold(true)
	stDim       = lipgloss.NewStyle().Faint(true)
	stAccent    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	stAccentB   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stTabOn     = lipgloss.NewStyle().Reverse(true).Bold(true)
	stTabOff    = lipgloss.NewStyle()
	stCursor    = lipgloss.NewStyle().Reverse(true)
	stCursorDim = lipgloss.NewStyle().Background(lipgloss.Color("8"))
	stHeader    = lipgloss.NewStyle().Bold(true).Underline(true)
	stSel       = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	stKey       = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	stWarn      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stOK        = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stBorder    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stCode      = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	stH1        = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Underline(true)
	stH2        = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stStar      = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
)

var stateStyle = map[string]lipgloss.Style{
	"ok": stOK, "outdated": stWarn, "modified": lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
	"conflict": stErr, "missing": stErr, "invalid": stErr, "orphan": stDim, "untracked": stAccent,
	"changed": stWarn, "up-to-date": stOK, "removed-upstream": stErr, "license-gone": stErr,
	"pinned": lipgloss.NewStyle().Foreground(lipgloss.Color("4")), "error": stErr,
	"pending-risky": lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true),
	"ERROR":         stErr, "WARN": stWarn, "review": stWarn, "info": stDim,
}

func styled(s string) string {
	if st, ok := stateStyle[s]; ok && s != "" {
		return st.Render(s)
	}
	return s
}

// fit pads or truncates s (may contain ANSI) to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", w-sw)
}

// box draws lines (already fitted to w) inside a rounded border with a title.
func box(title string, lines []string, w, h int, focused bool) []string {
	bs := stBorder
	if focused {
		bs = stAccent
	}
	if w < 4 || h < 2 {
		return nil
	}
	t := ""
	if title != "" {
		t = " " + title + " "
	}
	top := "╭" + t
	tw := ansi.StringWidth(top)
	if tw > w-1 {
		top = ansi.Truncate(top, w-1, "…")
		tw = ansi.StringWidth(top)
	}
	out := []string{bs.Render(top + strings.Repeat("─", w-1-tw) + "╮")}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, bs.Render("│")+fit(l, w-2)+bs.Render("│"))
	}
	out = append(out, bs.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return out
}

// hjoin places blocks side by side (every block has h lines of its width).
func hjoin(blocks ...[]string) []string {
	h := 0
	for _, b := range blocks {
		if len(b) > h {
			h = len(b)
		}
	}
	out := make([]string, h)
	for i := 0; i < h; i++ {
		var sb strings.Builder
		for _, b := range blocks {
			if i < len(b) {
				sb.WriteString(b[i])
			}
		}
		out[i] = sb.String()
	}
	return out
}

// ───────────── rect / hit testing ─────────────

type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

// ───────────── virtualized table ─────────────

type column struct {
	title string
	width int // fixed width; 0 = flexible
	flex  int // share of the remaining width
}

type table struct {
	cols    []column
	n       int
	cell    func(i, c int) string // lazily called for visible rows only
	keyOf   func(i int) string
	sel     map[string]bool
	cursor  int
	offset  int
	focused bool
	area    rect // last render: whole table including header
	widths  []int
	empty   string
}

func newTable(cols ...column) *table { return &table{cols: cols, sel: map[string]bool{}} }

func (t *table) bodyH() int { return max(1, t.area.h-1) }

func (t *table) clamp() {
	if t.cursor >= t.n {
		t.cursor = t.n - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	h := t.bodyH()
	if t.offset > t.cursor {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+h {
		t.offset = t.cursor - h + 1
	}
	if t.offset > t.n-h {
		t.offset = max(0, t.n-h)
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

func (t *table) move(d int) bool {
	old := t.cursor
	t.cursor += d
	t.clamp()
	return t.cursor != old
}

func (t *table) scroll(d int) {
	t.offset += d
	h := t.bodyH()
	if t.offset > t.n-h {
		t.offset = max(0, t.n-h)
	}
	if t.offset < 0 {
		t.offset = 0
	}
	if t.cursor < t.offset {
		t.cursor = t.offset
	}
	if t.cursor >= t.offset+h {
		t.cursor = t.offset + h - 1
	}
	if t.cursor >= t.n {
		t.cursor = t.n - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

func (t *table) current() string {
	if t.n == 0 || t.keyOf == nil || t.cursor >= t.n {
		return ""
	}
	return t.keyOf(t.cursor)
}

// targets: selected keys (in row order), else the key under the cursor.
func (t *table) targets() []string {
	if len(t.sel) > 0 {
		var out []string
		seen := map[string]bool{}
		for i := 0; i < t.n; i++ {
			if k := t.keyOf(i); t.sel[k] && !seen[k] {
				out = append(out, k)
				seen[k] = true
			}
		}
		for k := range t.sel { // selected rows hidden by the current filter
			if !seen[k] {
				out = append(out, k)
			}
		}
		return out
	}
	if k := t.current(); k != "" {
		return []string{k}
	}
	return nil
}

func (t *table) toggle() {
	k := t.current()
	if k == "" {
		return
	}
	if t.sel[k] {
		delete(t.sel, k)
	} else {
		t.sel[k] = true
	}
	t.move(1)
}

func (t *table) selectAll() {
	all := true
	for i := 0; i < t.n; i++ {
		all = all && t.sel[t.keyOf(i)]
	}
	for i := 0; i < t.n; i++ {
		if all {
			delete(t.sel, t.keyOf(i))
		} else {
			t.sel[t.keyOf(i)] = true
		}
	}
}

// keyNav handles navigation keys; returns true when the key was used.
func (t *table) keyNav(k string) bool {
	h := t.bodyH()
	switch k {
	case "up", "k":
		t.move(-1)
	case "down", "j":
		t.move(1)
	case "pgup", "ctrl+b":
		t.move(-h)
	case "pgdown", "ctrl+f":
		t.move(h)
	case "ctrl+u":
		t.move(-h / 2)
	case "ctrl+d":
		t.move(h / 2)
	case "home", "g":
		t.cursor = 0
		t.clamp()
	case "end", "G":
		t.cursor = t.n - 1
		t.clamp()
	default:
		return false
	}
	return true
}

// rowAt maps a screen position to a row index (-1 = header/outside).
func (t *table) rowAt(x, y int) int {
	if !t.area.has(x, y) || y == t.area.y {
		return -1
	}
	i := t.offset + (y - t.area.y - 1)
	if i >= t.n {
		return -1
	}
	return i
}

func (t *table) layout(w int) {
	t.widths = make([]int, len(t.cols))
	fixed, flex := 0, 0
	for i, c := range t.cols {
		if c.width > 0 {
			t.widths[i] = c.width
			fixed += c.width + 1
		} else {
			f := c.flex
			if f == 0 {
				f = 1
			}
			flex += f
		}
	}
	rest := w - fixed
	for i, c := range t.cols {
		if c.width == 0 {
			f := c.flex
			if f == 0 {
				f = 1
			}
			t.widths[i] = max(3, rest*f/max(1, flex)-1)
		}
	}
}

// render draws header + visible rows into exactly r.h lines of width r.w.
func (t *table) render(r rect) []string {
	if r.h <= 0 || r.w <= 0 {
		t.area = rect{}
		return nil
	}
	t.area = r
	t.layout(r.w)
	t.clamp()
	lines := make([]string, 0, r.h)
	var hb strings.Builder
	for i, c := range t.cols {
		if i > 0 {
			hb.WriteByte(' ')
		}
		hb.WriteString(fit(c.title, t.widths[i]))
	}
	lines = append(lines, stHeader.Render(fit(hb.String(), r.w)))
	h := r.h - 1
	if t.n == 0 && t.empty != "" {
		lines = append(lines, stDim.Render(fit("  "+t.empty, r.w)))
	}
	for row := t.offset; row < t.offset+h && row < t.n; row++ {
		var sb strings.Builder
		for c := range t.cols {
			if c > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(fit(t.cell(row, c), t.widths[c]))
		}
		line := fit(sb.String(), r.w)
		if row == t.cursor {
			plain := fit(ansi.Strip(sb.String()), r.w)
			if t.focused {
				line = stCursor.Render(plain)
			} else {
				line = stCursorDim.Render(plain)
			}
		}
		lines = append(lines, line)
	}
	for len(lines) < r.h {
		lines = append(lines, strings.Repeat(" ", r.w))
	}
	// scrollbar hint in the header when the list is longer than the view
	if t.n > h && r.w > 12 {
		pos := " " + itoa(t.cursor+1) + "/" + itoa(t.n) + " "
		lines[0] = ansi.Truncate(lines[0], r.w-len(pos), "") + stDim.Render(pos)
	}
	return lines
}

func (t *table) mark(i int) string {
	if t.sel[t.keyOf(i)] {
		return stSel.Render("●")
	}
	return " "
}

// ───────────── text input ─────────────

type input struct {
	val         []rune
	pos         int
	placeholder string
	focused     bool
	area        rect
}

func (in *input) Value() string { return string(in.val) }

func (in *input) Set(s string) { in.val = []rune(s); in.pos = len(in.val) }

func (in *input) insert(s string) {
	rs := []rune(strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", ""))
	v := append([]rune{}, in.val[:in.pos]...)
	v = append(v, rs...)
	in.val = append(v, in.val[in.pos:]...)
	in.pos += len(rs)
}

// key edits the value; returns (changed, handled).
func (in *input) key(msg tea.KeyPressMsg) (bool, bool) {
	k := msg.String()
	switch k {
	case "backspace", "ctrl+h":
		if in.pos > 0 {
			in.val = append(in.val[:in.pos-1], in.val[in.pos:]...)
			in.pos--
			return true, true
		}
		return false, true
	case "delete", "ctrl+d":
		if in.pos < len(in.val) {
			in.val = append(in.val[:in.pos], in.val[in.pos+1:]...)
			return true, true
		}
		return false, true
	case "left", "ctrl+b":
		in.pos = max(0, in.pos-1)
		return false, true
	case "right", "ctrl+f":
		in.pos = min(len(in.val), in.pos+1)
		return false, true
	case "home", "ctrl+a":
		in.pos = 0
		return false, true
	case "end", "ctrl+e":
		in.pos = len(in.val)
		return false, true
	case "ctrl+u":
		ch := in.pos > 0
		in.val, in.pos = in.val[in.pos:], 0
		return ch, true
	case "ctrl+k":
		ch := in.pos < len(in.val)
		in.val = in.val[:in.pos]
		return ch, true
	case "ctrl+w", "alt+backspace", "ctrl+backspace":
		i := in.pos
		for i > 0 && unicode.IsSpace(in.val[i-1]) {
			i--
		}
		for i > 0 && !unicode.IsSpace(in.val[i-1]) {
			i--
		}
		ch := i != in.pos
		in.val = append(in.val[:i], in.val[in.pos:]...)
		in.pos = i
		return ch, true
	case "space":
		in.insert(" ")
		return true, true
	}
	if msg.Text != "" && msg.Mod&^tea.ModShift == 0 {
		in.insert(msg.Text)
		return true, true
	}
	return false, false
}

func (in *input) render(w int, prompt string) string {
	in.area.w = w
	pw := ansi.StringWidth(prompt)
	avail := w - pw
	if avail < 1 {
		return fit(prompt, w)
	}
	if len(in.val) == 0 && !in.focused {
		return fit(prompt+stDim.Render(in.placeholder), w)
	}
	// keep the cursor visible
	start := 0
	if in.pos >= avail {
		start = in.pos - avail + 1
	}
	vis := in.val[start:]
	var sb strings.Builder
	for i, r := range vis {
		if in.focused && start+i == in.pos {
			sb.WriteString(stCursor.Render(string(r)))
		} else {
			sb.WriteRune(r)
		}
	}
	if in.focused && in.pos == len(in.val) {
		sb.WriteString(stCursor.Render(" "))
	}
	if len(in.val) == 0 && in.focused {
		sb.WriteString(stDim.Render(in.placeholder))
	}
	return fit(prompt+sb.String(), w)
}

// ───────────── scrollable text view ─────────────

type viewer struct {
	lines  []string
	offset int
	area   rect
}

func (v *viewer) set(lines []string) { v.lines, v.offset = lines, 0 }

func (v *viewer) scroll(d int) {
	v.offset += d
	if v.offset > len(v.lines)-v.area.h {
		v.offset = len(v.lines) - v.area.h
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

func (v *viewer) keyNav(k string) bool {
	h := max(1, v.area.h)
	switch k {
	case "up", "k":
		v.scroll(-1)
	case "down", "j":
		v.scroll(1)
	case "pgup", "ctrl+b", "ctrl+u":
		v.scroll(-h + 1)
	case "pgdown", "ctrl+f", "ctrl+d", "space":
		v.scroll(h - 1)
	case "home", "g":
		v.offset = 0
	case "end", "G":
		v.scroll(len(v.lines))
	default:
		return false
	}
	return true
}

func (v *viewer) render(r rect) []string {
	if r.h <= 0 || r.w <= 0 {
		v.area = rect{}
		return nil
	}
	v.area = r
	v.scroll(0)
	out := make([]string, r.h)
	for i := 0; i < r.h; i++ {
		l := ""
		if v.offset+i < len(v.lines) {
			l = v.lines[v.offset+i]
		}
		out[i] = fit(l, r.w)
	}
	if len(v.lines) > r.h && r.w > 10 && r.h > 0 {
		pct := itoa((v.offset+r.h)*100/len(v.lines)) + "%"
		out[r.h-1] = ansi.Truncate(out[r.h-1], r.w-len(pct)-1, "") + " " + stDim.Render(pct)
	}
	return out
}

// wrapPlain wraps plain text lines to width w.
func wrapPlain(text string, w int) []string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(text, "\t", "    "), "\n") {
		if w > 0 && ansi.StringWidth(l) > w {
			out = append(out, strings.Split(ansi.Wrap(l, w, ""), "\n")...)
		} else {
			out = append(out, l)
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
