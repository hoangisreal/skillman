package tui

// TestScreenshots renders SVG snapshots of the TUI model. See CONTRIBUTING.md
// for sample commands and instructions for capturing the running binary.

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/hoangisreal/skillman/internal/core"
)

func TestScreenshots(t *testing.T) {
	out, lib := os.Getenv("SKILLMAN_SCREENSHOTS"), os.Getenv("SKILLMAN_BENCH_LIBRARY")
	if out == "" || lib == "" {
		t.Skip("set SKILLMAN_SCREENSHOTS (output dir) and SKILLMAN_BENCH_LIBRARY (a real library)")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join("..", "..", out)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	os.MkdirAll(filepath.Join(home, "code"), 0o755)
	os.Symlink(lib, filepath.Join(home, "skill-library")) // shown as ~/skill-library
	t.Setenv("SKILL_LIBRARY", filepath.Join(home, "skill-library"))
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	t.Setenv("HOME", home)
	core.Configure()
	proj := filepath.Join(home, "code", "shop-web")
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"vue":"^3.5","pinia":"^2","postgres":"^3"},"devDependencies":{"vitest":"^2"}}`), 0o644)
	os.WriteFile(filepath.Join(proj, "Dockerfile"), []byte("FROM node:22\n"), 0o644)
	items, _ := core.ExpandItems([]string{"@core", "vue-best-practices", "vue-pinia-best-practices", "postgres-best-practices", "docker-compose-patterns"})
	core.Install(proj, items, false)

	d := newDriver(t, proj)
	d.send(tea.WindowSizeMsg{Width: 170, Height: 44})
	save := func(name, title string) {
		svg := toSVG(d.screen(), 170, 44, title)
		if err := os.WriteFile(filepath.Join(out, name+".svg"), []byte(svg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	d.press("/")
	d.typeText("postgres")
	d.press("enter", "down", "space", "down", "space", "down")
	save("1-library", "Library: ranked search, multi-select, Markdown preview")
	d.press("esc", "2")
	br := d.m.tabs[1].(*browseTab)
	for i, n := range br.nodes {
		if n.kind == "domain" && n.id == "database" {
			br.tt.cursor = i
			br.toggleNode(d.m)
		}
	}
	for i, n := range br.nodes {
		if n.kind == "stack" && n.id == "postgres" {
			br.tt.cursor = i
			br.fill(d.m)
		}
	}
	d.press("tab", "down")
	save("2-browse", "Browse: domains › stacks, ★ = used by this project")
	d.press("3")
	pt := d.m.tabs[2].(*packsTab)
	for i, s := range pt.sums {
		if s.Name == "web-vue" {
			pt.tbl.cursor = i
			pt.loadMembers(d.m)
		}
	}
	save("3-packs", "Packs: composites via extends, token cost per pack")
	d.press("4")
	save("4-project", "Project: drift states and token budget")
	d.press("6")
	ut := d.m.tabs[5].(*upstreamTab)
	ut.q.Set("new")
	ut.apply()
	save("5-upstream", "Upstream: tick verified candidates, dry-run import")
	d.press("7")
	save("6-validation", "Validation: every warning, jump to the skill")
	d.press("1", "ctrl+p")
	d.typeText("inst")
	save("7-palette", "Command palette (ctrl+p): every action, searchable")
	d.press("esc")
	t.Logf("wrote screenshots to %s", out)
}

// ───────────── ANSI → SVG ─────────────

var palette = []string{"#1d2021", "#ea6962", "#a9b665", "#d8a657", "#7daea3", "#d3869b", "#89b482", "#d4be98",
	"#665c54", "#ef938e", "#bbc58a", "#e3bb7a", "#9dc1b9", "#e0a3b6", "#a5c6a5", "#ebdbb2"}

const (
	svgBG, svgFG = "#282828", "#d4be98"
	cellW, cellH = 8.4, 18.0
)

type cellStyle struct {
	fg, bg                        string
	bold, faint, under, rev, ital bool
}

func color256(n int) string {
	if n < 16 {
		return palette[n]
	}
	if n >= 232 {
		v := 8 + (n-232)*10
		return fmt.Sprintf("#%02x%02x%02x", v, v, v)
	}
	n -= 16
	lv := []int{0, 95, 135, 175, 215, 255}
	return fmt.Sprintf("#%02x%02x%02x", lv[n/36], lv[(n/6)%6], lv[n%6])
}

func applySGR(st *cellStyle, params string) {
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		n, _ := strconv.Atoi(ps[i])
		switch {
		case n == 0:
			*st = cellStyle{}
		case n == 1:
			st.bold = true
		case n == 2:
			st.faint = true
		case n == 3:
			st.ital = true
		case n == 4:
			st.under = true
		case n == 7:
			st.rev = true
		case n == 22:
			st.bold, st.faint = false, false
		case n == 23:
			st.ital = false
		case n == 24:
			st.under = false
		case n == 27:
			st.rev = false
		case n >= 30 && n <= 37:
			st.fg = palette[n-30]
		case n >= 90 && n <= 97:
			st.fg = palette[n-90+8]
		case n >= 40 && n <= 47:
			st.bg = palette[n-40]
		case n >= 100 && n <= 107:
			st.bg = palette[n-100+8]
		case n == 39:
			st.fg = ""
		case n == 49:
			st.bg = ""
		case (n == 38 || n == 48) && i+2 < len(ps) && ps[i+1] == "5":
			c, _ := strconv.Atoi(ps[i+2])
			if n == 38 {
				st.fg = color256(c)
			} else {
				st.bg = color256(c)
			}
			i += 2
		case (n == 38 || n == 48) && i+4 < len(ps) && ps[i+1] == "2":
			r, _ := strconv.Atoi(ps[i+2])
			g, _ := strconv.Atoi(ps[i+3])
			b, _ := strconv.Atoi(ps[i+4])
			c := fmt.Sprintf("#%02x%02x%02x", r, g, b)
			if n == 38 {
				st.fg = c
			} else {
				st.bg = c
			}
			i += 4
		}
	}
}

func toSVG(screen string, cols, rows int, title string) string {
	var body strings.Builder
	top := 34.0
	for y, line := range strings.Split(screen, "\n") {
		if y >= rows {
			break
		}
		var st cellStyle
		col := 0
		type run struct {
			col  int
			text strings.Builder
			st   cellStyle
			w    int
		}
		var runs []*run
		cur := &run{}
		flush := func() {
			if cur.w > 0 {
				runs = append(runs, cur)
			}
			cur = &run{col: col, st: st}
		}
		for i := 0; i < len(line); {
			if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
				j := i + 2
				for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
					j++
				}
				if j < len(line) && line[j] == 'm' {
					flush()
					applySGR(&st, line[i+2:j])
					cur.st = st
				}
				i = j + 1
				continue
			}
			if line[i] == 0x1b { // other escapes (OSC etc.): skip to ST/BEL
				j := strings.IndexAny(line[i+1:], "\a\\")
				if j < 0 {
					break
				}
				i += j + 2
				continue
			}
			r, size := []rune(line[i:])[0], len(string([]rune(line[i:])[0]))
			w := ansi.StringWidth(string(r))
			cur.text.WriteRune(r)
			cur.w += w
			col += w
			i += size
		}
		flush()
		yy := top + float64(y)*cellH
		for _, r := range runs {
			fg, bg := r.st.fg, r.st.bg
			if fg == "" {
				fg = svgFG
			}
			if r.st.rev {
				if bg == "" {
					bg = svgBG
				}
				fg, bg = bg, fg
			}
			x := 10 + float64(r.col)*cellW
			if bg != "" {
				fmt.Fprintf(&body, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.0f" fill="%s"/>`+"\n", x, yy-13.5, float64(r.w)*cellW+0.5, cellH, bg)
			}
			txt := strings.TrimRight(r.text.String(), " ")
			if txt == "" {
				continue
			}
			attrs := ""
			if r.st.bold {
				attrs += ` font-weight="bold"`
			}
			if r.st.faint {
				attrs += ` opacity="0.6"`
			}
			if r.st.ital {
				attrs += ` font-style="italic"`
			}
			if r.st.under {
				attrs += ` text-decoration="underline"`
			}
			fmt.Fprintf(&body, `<text x="%.1f" y="%.1f" fill="%s"%s textLength="%.1f" lengthAdjust="spacingAndGlyphs">%s</text>`+"\n",
				x, yy, fg, attrs, float64(ansi.StringWidth(txt))*cellW, html.EscapeString(txt))
		}
	}
	w, h := 20+float64(cols)*cellW, top+float64(rows)*cellH+8
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f" font-family="'JetBrains Mono','Fira Code','DejaVu Sans Mono',Menlo,Consolas,monospace" font-size="14" xml:space="preserve">
<!-- rendered from a real skillman TUI frame by internal/tui/screenshots_test.go -->
<rect width="100%%" height="100%%" rx="8" fill="%s"/>
<circle cx="18" cy="14" r="5" fill="#ea6962"/><circle cx="36" cy="14" r="5" fill="#d8a657"/><circle cx="54" cy="14" r="5" fill="#a9b665"/>
<text x="%.0f" y="18" fill="#928374" text-anchor="middle">%s</text>
%s</svg>
`, w, h, svgBG, w/2, html.EscapeString(title), body.String())
}
