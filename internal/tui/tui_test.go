package tui

// Model tests: drive the TUI with key and mouse messages (no terminal), port of tests/test_tui.py.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/hoangisreal/skillman/internal/core"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func mkskill(t *testing.T, base, name, desc, body string) {
	t.Helper()
	d := filepath.Join(base, name)
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+desc+"\n---\n"+body), 0o644)
}

type env struct {
	root, lib, up, bare, proj string
}

func setup(t *testing.T) *env {
	root := t.TempDir()
	e := &env{root: root, lib: filepath.Join(root, "lib"), up: filepath.Join(root, "up")}
	rem := filepath.Join(root, "remotes")
	e.bare = filepath.Join(rem, "acme", "skills.git")
	t.Setenv("SKILL_LIBRARY", e.lib)
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	t.Setenv("SKILLCORE_GIT_BASE", "file://"+rem+"/")
	t.Setenv("HOME", filepath.Join(root, "home"))
	for _, kv := range [][2]string{{"GIT_AUTHOR_NAME", "t"}, {"GIT_AUTHOR_EMAIL", "t@t"}, {"GIT_COMMITTER_NAME", "t"}, {"GIT_COMMITTER_EMAIL", "t@t"}} {
		t.Setenv(kv[0], kv[1])
	}
	core.Configure()
	os.MkdirAll(e.up, 0o755)
	sh(t, e.up, "git", "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(e.up, "LICENSE"), []byte("MIT\n"), 0o644)
	for _, n := range []string{"alpha", "beta", "gamma"} {
		mkskill(t, filepath.Join(e.up, "skills"), n, n+" skill for testing", "body\n")
	}
	sh(t, e.up, "git", "add", "-A")
	sh(t, e.up, "git", "commit", "-qm", "v1")
	c1 := sh(t, e.up, "git", "rev-parse", "HEAD")
	os.MkdirAll(filepath.Dir(e.bare), 0o755)
	sh(t, root, "git", "clone", "-q", "--bare", e.up, e.bare)
	sh(t, e.bare, "git", "config", "uploadpack.allowFilter", "true")
	sh(t, e.bare, "git", "config", "uploadpack.allowAnySHA1InWant", "true")
	os.MkdirAll(filepath.Join(e.lib, "skills"), 0o755)
	sh(t, e.lib, "git", "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(e.lib, ".gitignore"), []byte(".cache/\n"), 0o644)
	for _, n := range []string{"alpha", "beta", "gamma"} {
		st, _, err := core.Materialize("acme/skills", c1, "skills/"+n, n, "official", nil)
		if err != nil {
			t.Fatal(err)
		}
		core.InstallIntoLibrary(st, n)
	}
	mkskill(t, filepath.Join(e.lib, "skills"), "vue", "Vue 3 components and composition API", "# Vue\n\n- use `ref`\n")
	mkskill(t, filepath.Join(e.lib, "skills"), "vitest", "Unit testing with Vitest", "body\n")
	mkskill(t, filepath.Join(e.lib, "skills"), "tdd", "Test-driven development loop", "body\n")
	mkskill(t, filepath.Join(e.lib, "skills"), "review-pr", "Review a pull request", "body\n")
	core.SavePack(&core.Pack{Name: "core", Description: "baseline", Skills: []string{"tdd", "review-pr"}})
	core.SavePack(&core.Pack{Name: "web-vue", Description: "vue stack", Skills: []string{"vue", "vitest"}})
	core.SavePack(&core.Pack{Name: "fullstack", Description: "web + acme", Extends: []string{"web-vue"}, Skills: []string{"alpha"}})
	os.MkdirAll(filepath.Join(e.lib, "staging"), 0o755)
	os.WriteFile(filepath.Join(e.lib, "staging/CANDIDATES.md"), []byte("# c\n\n### acme/skills (official) · `abc` · LICENSE\n"+
		"- [ ] `alpha` — d <sub>skills/alpha · 5L · collides</sub>\n- [ ] `newone` — d <sub>skills/newone · 5L</sub>\n"), 0o644)
	core.RebuildIndex(true)
	sh(t, e.lib, "git", "add", "-A")
	sh(t, e.lib, "git", "commit", "-qm", "init")
	e.proj = filepath.Join(root, "demo-app")
	os.MkdirAll(e.proj, 0o755)
	os.WriteFile(filepath.Join(e.proj, "package.json"), []byte(`{"dependencies":{"vue":"^3"}}`), 0o644)
	return e
}

type driver struct {
	t *testing.T
	m *Model
}

func newDriver(t *testing.T, proj string) *driver {
	noTimers = true
	d := &driver{t: t, m: New(proj)}
	d.run(d.m.Init(), 0)
	d.send(tea.WindowSizeMsg{Width: 160, Height: 45})
	d.m.render()
	return d
}

func (d *driver) send(msg tea.Msg) {
	d.run(d.m.update(msg), 0)
	d.m.render() // refresh hit-test geometry like a real frame
}

func (d *driver) run(cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 64 {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(20 * time.Second):
		d.t.Fatal("command timed out")
	}
	switch x := msg.(type) {
	case nil, tea.QuitMsg:
	case tea.BatchMsg:
		for _, c := range x {
			d.run(c, depth+1)
		}
	default:
		d.run(d.m.update(x), depth+1)
	}
}

var named = map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "up": tea.KeyUp,
	"down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight, "backspace": tea.KeyBackspace, "home": tea.KeyHome, "end": tea.KeyEnd}

func (d *driver) press(keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch {
		case k == "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case strings.HasPrefix(k, "ctrl+"):
			msg = tea.KeyPressMsg{Code: rune(k[5]), Mod: tea.ModCtrl}
		case named[k] != 0:
			msg = tea.KeyPressMsg{Code: named[k]}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		d.send(msg)
	}
}

func (d *driver) typeText(s string) {
	for _, r := range s {
		d.press(string(r))
	}
}

func (d *driver) click(x, y int) {
	d.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	d.send(tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func (d *driver) screen() string { return d.m.render() }

func installed(p string) map[string]bool {
	out := map[string]bool{}
	for n := range core.ReadManifest(p).Skills {
		out[n] = true
	}
	return out
}

func check(t *testing.T, cond bool, msg string, a ...any) {
	t.Helper()
	if !cond {
		t.Errorf(msg, a...)
	}
}

func TestTUIFlow(t *testing.T) {
	e := setup(t)
	d := newDriver(t, e.proj)
	m := d.m
	lib := m.tabs[0].(*libraryTab)
	check(t, len(lib.hits) == 7, "library rows %d", len(lib.hits))

	// search: "/" focuses, typing filters live, enter returns to the list
	d.press("/")
	d.typeText("pack:core")
	check(t, lib.tbl.n == 2, "search pack:core -> %d", lib.tbl.n)
	d.press("ctrl+u")
	d.typeText("src:acme")
	check(t, lib.tbl.n == 3, "search src:acme -> %d", lib.tbl.n)
	d.press("enter")
	check(t, !lib.typing(), "enter leaves the search box")
	d.press("space", "space", "i")
	check(t, len(m.modals) == 1, "install asks for confirmation")
	d.press("n")
	check(t, len(lib.tbl.sel) == 2 && len(installed(e.proj)) == 0, "cancel keeps the selection")
	d.press("i", "y")
	got := installed(e.proj)
	check(t, len(got) == 2 && got["alpha"] && got["beta"], "install library selection: %v", got)
	check(t, len(lib.tbl.sel) == 0, "selection cleared after install")
	check(t, strings.Contains(d.screen(), "alpha"), "screen renders")

	// ranking: exact name first
	d.press("/", "ctrl+u")
	d.typeText("vue")
	check(t, lib.tbl.current() == "vue", "exact name ranks first: %s", lib.tbl.current())
	d.press("esc", "esc")

	// mouse: click a row, click ● toggles, double click opens
	row := lib.tbl.area.y + 1
	d.click(lib.tbl.area.x+5, row+1)
	check(t, lib.tbl.cursor == 1, "click moves cursor: %d", lib.tbl.cursor)
	d.click(lib.tbl.area.x, row+1)
	check(t, len(lib.tbl.sel) == 1, "click on ● toggles selection")
	d.press("esc")
	check(t, len(lib.tbl.sel) == 0, "esc clears selection")
	d.click(lib.tbl.area.x+5, row)
	d.click(lib.tbl.area.x+5, row)
	_, isMD := m.modals[len(m.modals)-1].(*mdModal)
	check(t, len(m.modals) == 1 && isMD, "double click opens the skill")
	d.press("esc")
	check(t, len(m.modals) == 0, "esc closes")

	// tab bar click
	d.click(m.tabZones[2].x+1, 0)
	check(t, m.active == 2, "click on tab switches: %d", m.active)

	// Packs: install a composite pack, create a pack, add a skill via picker
	packs := m.tabs[2].(*packsTab)
	for i, s := range packs.sums {
		if s.Name == "fullstack" {
			packs.tbl.cursor = i
		}
	}
	packs.loadMembers(m)
	d.press("i", "y")
	man := core.ReadManifest(e.proj)
	check(t, man.Skills["vue"] != nil && man.Skills["vitest"] != nil && man.Packs.Has("fullstack"), "install pack with extends")
	d.press("n")
	d.typeText("my-pack")
	d.press("enter")
	p, _ := core.PackPath("my-pack")
	check(t, core.Exists(p), "create pack")
	for i, s := range packs.sums {
		if s.Name == "my-pack" {
			packs.tbl.cursor = i
		}
	}
	d.press("a")
	d.typeText("gamma")
	d.press("enter")
	pk, _ := core.LoadPack("my-pack")
	check(t, len(pk.Skills) == 1 && pk.Skills[0] == "gamma", "add skill to pack via picker: %v", pk.Skills)

	// Browse: the project's stacks are detected (★) and the vue stack lists vue
	d.press("2")
	br := m.tabs[1].(*browseTab)
	check(t, len(br.nodes) > 0 && br.nodes[0].kind == "project", "project node first: %+v", br.nodes[0])
	found := false
	for i, n := range br.nodes {
		if n.kind == "stack" && n.id == "vue" {
			br.tt.cursor = i
			br.fill(m)
			found = true
		}
	}
	check(t, found, "vue stack visible")
	names := map[string]bool{}
	for _, i := range br.items {
		names[m.d.ix.Docs[i].Row.Name] = true
	}
	check(t, names["vue"], "vue stack lists vue: %v", names)
	check(t, strings.Contains(d.screen(), "This project"), "browse renders project node")

	// Project tab: remove, doctor, verify, audit, suggest
	d.press("4")
	pt := m.tabs[3].(*projectTab)
	keys := map[string]bool{}
	for _, r := range pt.rows {
		keys[r.Name] = true
	}
	check(t, len(keys) == 4 && keys["vue"] && keys["alpha"], "project rows %v", keys)
	for i, r := range pt.rows {
		if r.Name == "vue" {
			pt.tbl.cursor = i
		}
	}
	d.press("x", "y")
	check(t, !installed(e.proj)["vue"], "remove skill (to trash)")
	d.press("o")
	check(t, len(m.modals) == 1, "doctor view")
	d.press("esc", "v")
	tm, ok := m.modals[0].(*textModal)
	check(t, ok && strings.Contains(tm.text, "match the manifest") && strings.Contains(tm.text, "alpha: ok"), "verify view")
	d.press("esc", "A")
	tm, ok = m.modals[0].(*textModal)
	check(t, ok && strings.HasPrefix(tm.title, "audit") && strings.Contains(tm.text, "alpha"), "audit view")
	d.press("esc", "s")
	picker, ok := m.modals[0].(*pickModal)
	check(t, ok && picker.pickMany != nil, "shortlist picker")
	d.press("space", "enter")
	_, ok = m.modals[0].(*confirmModal)
	check(t, ok, "shortlist selection previews install")
	d.press("esc")

	// Trash: restore
	d.press("8")
	tr := m.tabs[7].(*trashTab)
	for i, en := range tr.entries {
		if en.S("name") == "vue" {
			tr.tbl.cursor = i
		}
	}
	d.press("R")
	check(t, installed(e.proj)["vue"], "restore from trash")

	// Updates: alpha safe, beta risky (adds hooks)
	os.WriteFile(filepath.Join(e.up, "skills/alpha/SKILL.md"), []byte("---\nname: alpha\ndescription: alpha v2\n---\nv2\n"), 0o644)
	os.WriteFile(filepath.Join(e.up, "skills/beta/SKILL.md"), []byte("---\nname: beta\ndescription: b\nhooks:\n  Stop: []\n---\nb2\n"), 0o644)
	sh(t, e.up, "git", "add", "-A")
	sh(t, e.up, "git", "commit", "-qm", "v2")
	sh(t, e.up, "git", "push", "-q", e.bare, "main")
	d.press("5")
	up := m.tabs[4].(*updatesTab)
	d.press("c")
	check(t, up.items["alpha"].Status == "changed" && up.items["beta"].Status == "changed", "check all: %v %v", up.items["alpha"].Status, up.items["beta"].Status)
	up.tbl.cursor = 0
	d.press("enter")
	if len(m.modals) != 1 || !strings.Contains(d.screen(), "v2") {
		t.Fatal("missing first staged diff")
	}
	d.press("esc")
	mkskill(t, filepath.Join(e.up, "skills"), "alpha", "alpha v3", "Latest upstream revision.\n")
	sh(t, e.up, "git", "add", "-A")
	sh(t, e.up, "git", "commit", "-qm", "v3")
	sh(t, e.up, "git", "push", "-q", e.bare, "main")
	d.press("c", "enter")
	if len(m.modals) != 1 || !strings.Contains(m.modals[0].(*textModal).text, "Latest upstream revision.") {
		t.Fatal("re-check still showed a stale staged diff")
	}
	d.press("esc")
	d.press("space", "space", "a")
	_, ok = m.modals[len(m.modals)-1].(*pickModal)
	check(t, ok, "risky choice dialog")
	d.press("esc")
	b, _ := os.ReadFile(filepath.Join(e.lib, "skills/alpha/SKILL.md"))
	check(t, !strings.Contains(string(b), "v2"), "esc on the risky dialog cancels everything")
	d.press("a", "enter") // first option: safe only
	b, _ = os.ReadFile(filepath.Join(e.lib, "skills/alpha/SKILL.md"))
	check(t, strings.Contains(string(b), "Latest upstream revision."), "latest safe update applied")
	b, _ = os.ReadFile(filepath.Join(e.lib, "skills/beta/SKILL.md"))
	check(t, !strings.Contains(string(b), "hooks"), "risky update not applied")
	check(t, strings.Contains(sh(t, e.lib, "git", "log", "-1", "--format=%s"), "Update 1 skill"), "library commit")
	d.press("esc", "4")
	check(t, m.d.state("alpha") == "outdated", "project shows outdated: %s", m.d.state("alpha"))
	d.press("U")
	check(t, m.d.state("alpha") == "ok", "update all outdated: %s", m.d.state("alpha"))

	// Upstream: tick a candidate
	d.press("6")
	ut := m.tabs[5].(*upstreamTab)
	for i, si := range ut.shown {
		if ut.cands[si].Name == "newone" {
			ut.tbl.cursor = i
		}
	}
	d.press("space")
	ticked := false
	for _, c := range core.ReadCandidates() {
		ticked = ticked || (c.Ticked && c.Name == "newone")
	}
	check(t, ticked, "tick candidate")

	// Validation: re-validate, jump to Library
	d.press("7")
	vt := m.tabs[6].(*validationTab)
	mkskill(t, filepath.Join(e.lib, "skills"), "broken", "x", "see [x](references/missing.md)\n")
	d.press("v")
	has := false
	for i, r := range vt.all {
		if r.name == "broken" {
			has = true
			for j, s := range vt.shown {
				if s == i {
					vt.tbl.cursor = j
				}
			}
		}
	}
	check(t, has, "re-validate finds the new warning")
	d.press("enter")
	check(t, m.active == 0 && lib.tbl.current() == "broken", "jump to library: tab %d cur %s", m.active, lib.tbl.current())

	// palette + help
	d.press("ctrl+p")
	_, ok = m.modals[0].(*pickModal)
	check(t, ok, "command palette")
	d.press("esc", "?")
	check(t, len(m.modals) == 1, "help")
	d.press("esc")
}

func TestAdoptUsesProjectSelection(t *testing.T) {
	e := setup(t)
	if rs, err := core.Install(e.proj, []core.Item{{Name: "alpha"}}, false); err != nil || len(rs) != 1 || !rs[0].OK {
		t.Fatal(rs, err)
	}
	mkskill(t, filepath.Dir(core.Dest(e.proj, "beta")), "beta", "local copy", "body")
	d := newDriver(t, e.proj)
	d.press("4")
	pt := d.m.tabs[3].(*projectTab)
	if pt.tbl.current() != "alpha" {
		t.Fatal("unexpected initial selection", pt.tbl.current())
	}
	d.press("a")
	if installed(e.proj)["beta"] || core.Lexists(core.LinkP(e.proj, "beta")) {
		t.Fatal("adopt changed an unselected skill")
	}
	d.press("down", "a")
	if !installed(e.proj)["beta"] {
		t.Fatal("selected untracked skill was not adopted")
	}
}

func TestInstallPreviewShowsBlockedAndUnavailableSkills(t *testing.T) {
	e := setup(t)
	if err := core.CopyTree(filepath.Join(core.SKILLS, "alpha"), core.Dest(e.proj, "alpha"), nil); err != nil {
		t.Fatal(err)
	}
	if err := core.SavePack(&core.Pack{Name: "core", Skills: []string{"not-downloaded"}}); err != nil {
		t.Fatal(err)
	}
	d := newDriver(t, e.proj)
	d.press("/", "a", "l", "p", "h", "a", "enter", "i")
	m := d.m
	if len(m.modals) != 1 {
		t.Fatalf("missing preview: %d modals", len(m.modals))
	}
	info, ok := m.modals[0].(*textModal)
	if !ok || !strings.Contains(info.text, "skillman adopt") {
		t.Fatalf("preview did not explain the untracked copy: %v", m.modals[0])
	}
	d.press("y")
	if core.Lexists(core.LinkP(e.proj, "alpha")) || len(installed(e.proj)) != 0 {
		t.Fatal("preview accepted a blocked install")
	}
	d.press("esc", "3")
	packs := m.tabs[2].(*packsTab)
	if !strings.Contains(packs.info, "0/1 available") || !strings.Contains(packs.info, "skillman bootstrap --pack core --dry-run") {
		t.Fatalf("empty starter pack guidance: %s", packs.info)
	}
	d.press("i")
	if len(m.modals) != 1 {
		t.Fatalf("missing preview for unavailable pack: %d modals", len(m.modals))
	}
	info, ok = m.modals[0].(*textModal)
	if !ok || !strings.Contains(info.text, "skillman bootstrap --pack core") {
		t.Fatalf("preview did not explain how to populate @core: %v", m.modals[0])
	}
}

func TestUpstreamRequiresReadingPreviewBeforeConfirmation(t *testing.T) {
	e := setup(t)
	mkskill(t, filepath.Join(e.up, "skills"), "fresh-docs", "Documentation review", "Review Markdown.\n")
	mkskill(t, filepath.Join(e.up, "skills"), "other-docs", "Another documentation skill", "Read the examples.\n")
	sh(t, e.up, "git", "add", "-A")
	sh(t, e.up, "git", "commit", "-qm", "add documentation skill")
	sh(t, e.up, "git", "push", "-q", e.bare, "main")
	repos := filepath.Join(core.STAGING, "repos.txt")
	if err := os.WriteFile(repos, []byte("official core acme/skills\n"), 0644); err != nil {
		t.Fatal(err)
	}
	d := newDriver(t, e.proj)
	d.press("6", "V", "y", "/")
	d.typeText("fresh-docs")
	d.press("enter", "space", "I")
	if len(d.m.modals) != 1 {
		t.Fatalf("expected a single preview modal: %d", len(d.m.modals))
	}
	pre, ok := d.m.modals[0].(*textModal)
	if !ok || !strings.Contains(pre.text, "fresh-docs") || pre.accept == nil {
		t.Fatalf("missing preview or continue action: %v", d.m.modals[0])
	}
	d.press("enter")
	if len(d.m.modals) != 1 {
		t.Fatal("confirmation did not replace the preview")
	}
	if _, ok := d.m.modals[0].(*confirmModal); !ok {
		t.Fatal("expected confirmation after preview")
	}
	other := -1
	for _, c := range core.ReadCandidates() {
		if c.Name == "other-docs" {
			other = c.Line
			break
		}
	}
	if err := core.SetTicks(map[int]bool{other: true}); err != nil {
		t.Fatal(err)
	}
	d.press("y")
	if !strings.Contains(d.m.note, "selection changed") || core.IsDir(filepath.Join(core.SKILLS, "fresh-docs")) || core.IsDir(filepath.Join(core.SKILLS, "other-docs")) {
		t.Fatal("confirmed an import after the reviewed selection changed")
	}
	if err := core.SetTicks(map[int]bool{other: false}); err != nil {
		t.Fatal(err)
	}
	d.press("I", "enter")
	d.press("y")
	if !core.IsFile(filepath.Join(core.SKILLS, "fresh-docs", "SKILL.md")) {
		t.Fatal("confirmed candidate did not import")
	}
}

// BenchmarkSearchKeystroke measures one keystroke in the Library search box (filter + render).
func BenchmarkSearchKeystroke(b *testing.B) {
	lib := os.Getenv("SKILLMAN_BENCH_LIBRARY")
	if lib == "" {
		b.Skip("set SKILLMAN_BENCH_LIBRARY to a real library")
	}
	b.Setenv("SKILL_LIBRARY", lib)
	core.Configure()
	noTimers = true
	m := New(b.TempDir())
	m.update(tea.WindowSizeMsg{Width: 180, Height: 50})
	m.update(m.loadStatus()())
	m.render()
	m.update(tea.KeyPressMsg{Code: '/', Text: "/"})
	words := []string{"p", "o", "s", "t"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := words[i%len(words)]
		if i%len(words) == 0 {
			m.update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
		}
		m.update(tea.KeyPressMsg{Code: rune(w[0]), Text: w})
		m.render()
	}
}

// TestLatencyReport replays the baseline script (type words, backspace, cursor down) on a real library and
// reports per-key Update+render latency. SKILLMAN_BENCH_LIBRARY enables it.
func TestLatencyReport(t *testing.T) {
	lib := os.Getenv("SKILLMAN_BENCH_LIBRARY")
	if lib == "" {
		t.Skip("set SKILLMAN_BENCH_LIBRARY to a real library")
	}
	t.Setenv("SKILL_LIBRARY", lib)
	core.Configure()
	noTimers = true
	t0 := time.Now()
	m := New(t.TempDir())
	m.update(tea.WindowSizeMsg{Width: 180, Height: 50})
	m.update(m.loadStatus()())
	m.render()
	startup := time.Since(t0)
	measure := func(msg tea.Msg) float64 {
		s := time.Now()
		m.update(msg)
		m.render()
		return float64(time.Since(s).Microseconds()) / 1000
	}
	measure(tea.KeyPressMsg{Code: '/', Text: "/"})
	var typing, moves []float64
	for _, w := range []string{"post", "vue", "docker", "test", "react"} {
		for _, r := range w {
			typing = append(typing, measure(tea.KeyPressMsg{Code: r, Text: string(r)}))
		}
		for range w {
			typing = append(typing, measure(tea.KeyPressMsg{Code: tea.KeyBackspace}))
		}
	}
	measure(tea.KeyPressMsg{Code: tea.KeyEnter})
	for i := 0; i < 40; i++ {
		moves = append(moves, measure(tea.KeyPressMsg{Code: tea.KeyDown}))
	}
	stats := func(xs []float64) string {
		s := append([]float64{}, xs...)
		sortFloats(s)
		return fmt.Sprintf("n=%d median %.2f ms  p90 %.2f  max %.2f", len(s), s[len(s)/2], s[len(s)*9/10], s[len(s)-1])
	}
	t.Logf("go TUI: startup %d ms (index %d skills)", startup.Milliseconds(), len(m.d.rows))
	t.Logf("  keystroke in search: %s", stats(typing))
	t.Logf("  cursor down:         %s", stats(moves))
}

func sortFloats(xs []float64) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

// TestRenderAnySize renders every tab and the modals at many terminal sizes (no panics, exact line widths).
func TestRenderAnySize(t *testing.T) {
	e := setup(t)
	d := newDriver(t, e.proj)
	for _, sz := range [][2]int{{1, 1}, {8, 3}, {20, 6}, {50, 8}, {40, 12}, {80, 24}, {120, 40}, {250, 70}} {
		d.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		for i := range d.m.tabs {
			d.m.setTab(i)
			for _, l := range strings.Split(d.screen(), "\n") {
				if w := ansi.StringWidth(l); sz[0] >= 10 && w != sz[0] {
					t.Fatalf("tab %d at %v: line width %d: %q", i, sz, w, ansi.Strip(l))
				}
			}
			d.press("?")
			d.screen()
			d.press("esc", "ctrl+p")
			d.screen()
			d.press("esc")
		}
		d.m.setTab(0)
		d.press("enter")
		d.screen()
		d.press("esc", "i")
		screen := ansi.Strip(d.screen())
		if sz[0] >= 20 && sz[1] >= 6 && !strings.Contains(screen, "Yes") {
			t.Fatalf("missing confirm buttons at %v:\n%s", sz, screen)
		}
		d.press("esc")
		if sz[0] == 80 {
			d.press("4")
			screen = ansi.Strip(d.screen())
			if !strings.Contains(screen, "8 Trash") || !strings.Contains(screen, "? help") {
				t.Fatalf("narrow view hid navigation or help:\n%s", screen)
			}
		}
	}
}
