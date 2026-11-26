package cli

// The agent workflow from skills/skillman/SKILL.md, run against a fixture library through Run().

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
	"github.com/hoangisreal/skillman/internal/search"
)

func mkskill(t *testing.T, lib, name, desc string) {
	t.Helper()
	d := filepath.Join(lib, "skills", name)
	os.MkdirAll(d, 0o755)
	os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: "+desc+"\n---\nbody\n"), 0o644)
}

func setup(t *testing.T) (lib, proj string) {
	root := t.TempDir()
	lib, proj = filepath.Join(root, "lib"), filepath.Join(root, "app")
	t.Setenv("SKILL_LIBRARY", lib)
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	core.Configure()
	if rc, _, errOut := run(t, "init"); rc != 0 {
		t.Fatalf("init: %s", errOut)
	}
	for n, d := range map[string]string{
		"vue-best-practices": "Vue 3 composition API best practices", "vitest": "Unit testing with Vitest",
		"postgres-best-practices": "PostgreSQL schema and query best practices", "docker-compose-patterns": "Docker Compose patterns",
		"tdd": "Test-driven development loop", "review-pr": "Review a pull request", "vue-testing-best-practices": "Testing Vue components",
	} {
		mkskill(t, lib, n, d)
	}
	core.RebuildIndex(true)
	os.MkdirAll(proj, 0o755)
	os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"vue":"^3","postgres":"^3"}}`), 0o644)
	os.WriteFile(filepath.Join(proj, "Dockerfile"), []byte("FROM node\n"), 0o644)
	return lib, proj
}

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var o, e bytes.Buffer
	stdout, stderr = &o, &e
	defer func() { stdout, stderr = os.Stdout, os.Stderr }()
	rc := Run("skillman", args)
	return rc, o.String(), e.String()
}

func runJSON(t *testing.T, v any, args ...string) int {
	t.Helper()
	rc, out, errOut := run(t, append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("%v: invalid JSON: %v\n%s\n%s", args, err, out, errOut)
	}
	return rc
}

func TestAgentWorkflow(t *testing.T) {
	lib, proj := setup(t)
	for _, f := range []string{"packs/core.yaml", "packs/web-vue.yaml", "staging/repos.txt", "skills/skillman/SKILL.md"} {
		if !core.Exists(filepath.Join(lib, f)) {
			t.Errorf("init did not create %s", f)
		}
	}
	var rec core.Recommendation
	runJSON(t, &rec, "recommend", proj)
	var packs []string
	for _, g := range rec.Groups {
		packs = append(packs, g.Pack)
	}
	for _, want := range []string{"core", "web-vue", "postgres", "docker"} {
		if !core.Contains(packs, want) {
			t.Errorf("recommend misses @%s: %v", want, packs)
		}
	}
	var sr struct {
		Count  int `json:"count"`
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	runJSON(t, &sr, "search", "vue", "testing")
	if sr.Count == 0 || sr.Skills[0].Name != "vue-testing-best-practices" {
		t.Errorf("search ranking: %+v", sr)
	}
	var info map[string]any
	runJSON(t, &info, "info", "vue-best-practices")
	if info["domain"] != "web-frontend" {
		t.Errorf("info domain: %v", info["domain"])
	}
	var dry map[string]any
	runJSON(t, &dry, "add", "@web-vue", "postgres-best-practices", "--dry-run", "-C", proj)
	if core.Exists(filepath.Join(proj, ".agents")) {
		t.Error("dry run wrote files")
	}
	var res struct {
		OK      bool          `json:"ok"`
		Results []core.Result `json:"results"`
	}
	if rc := runJSON(t, &res, "add", "@web-vue", "postgres-best-practices", "-C", proj); rc != 0 || !res.OK {
		t.Errorf("add: rc=%d %+v", rc, res)
	}
	if !core.LinkOK(proj, "vue-best-practices") || core.IsSymlink(core.LinkP(proj, "vue-best-practices")) == (runtime.GOOS == "windows") {
		t.Error("expected the platform's default Claude path")
	}
	// a skill named explicitly that is missing still fails
	if rc, _, _ := run(t, "add", "no-such-skill", "-C", proj); rc != 1 {
		t.Errorf("missing explicit skill should fail, rc=%d", rc)
	}
	var st []core.SkillStatus
	runJSON(t, &st, "status", proj)
	for _, s := range st {
		if s.State != "ok" {
			t.Errorf("status %s=%s", s.Name, s.State)
		}
	}
	var ver map[string]any
	if rc := runJSON(t, &ver, "verify", proj); rc != 0 || ver["ok"] != true {
		t.Errorf("verify: %v", ver)
	}
	var ok map[string]any
	runJSON(t, &ok, "pack", "create", "my-kit", "tdd", "vitest", "-d", "Mine: kit")
	var show struct {
		Pack core.Pack `json:"pack"`
	}
	runJSON(t, &show, "pack", "show", "my-kit")
	if show.Pack.Description != "Mine: kit" || len(show.Pack.Skills) != 2 {
		t.Errorf("pack show: %+v", show.Pack)
	}
	y, _ := os.ReadFile(filepath.Join(lib, "packs/my-kit.yaml"))
	if string(y) != "name: my-kit\ndescription: 'Mine: kit'\nskills:\n- tdd\n- vitest\n" {
		t.Errorf("pack yaml: %q", y)
	}
	runJSON(t, &ok, "remove", "@web-vue", "-C", proj)
	if core.Exists(filepath.Join(proj, ".agents/skills/vitest")) {
		t.Error("remove @web-vue left vitest")
	}
	var browse struct {
		Domains []core.DomainNode `json:"domains"`
	}
	runJSON(t, &browse, "browse", "--stack", "vue", "-C", proj)
	if len(browse.Domains) != 1 || browse.Domains[0].Stacks[0].ID != "vue" || !browse.Domains[0].Stacks[0].Detected {
		t.Errorf("browse --stack vue: %+v", browse.Domains)
	}
	// unknown command / usage errors use exit code 2
	if rc, _, _ := run(t, "nope"); rc != 2 {
		t.Errorf("unknown command rc=%d", rc)
	}
	// legacy entry points
	var o bytes.Buffer
	stdout = &o
	Run("/usr/local/bin/skill-search", []string{"postgres"})
	stdout = os.Stdout
	if !strings.Contains(o.String(), "postgres-best-practices") || !strings.Contains(o.String(), "[database") {
		t.Errorf("skill-search: %q", o.String())
	}
	o.Reset()
	stdout = &o
	Run("skill-add", []string{"tdd", proj})
	stdout = os.Stdout
	if !core.Exists(filepath.Join(proj, ".agents/skills/tdd")) {
		t.Errorf("skill-add name dir: %s", o.String())
	}
}

func TestUpdateDryRun(t *testing.T) {
	lib, proj := setup(t)
	if rc, out, errOut := run(t, "update", "--dry-run", "-C", proj); rc != 0 || !strings.Contains(out, "nothing to do") || core.Exists(filepath.Join(proj, ".agents")) {
		t.Fatalf("preview of a fresh project: rc=%d, %s%s", rc, out, errOut)
	}
	if rc, _, errOut := run(t, "repair-links", "--mode", "copy", "-C", proj); rc != 0 {
		t.Fatalf("set copy mode: %s", errOut)
	}
	if rc, _, errOut := run(t, "add", "vitest", "tdd", "-C", proj); rc != 0 {
		t.Fatalf("add: %s", errOut)
	}
	before := core.TreeHash(proj)
	mkskill(t, lib, "vitest", "Updated Vitest instructions")
	var preview struct {
		OK      bool          `json:"ok"`
		Results []core.Result `json:"results"`
	}
	rc := runJSON(t, &preview, "update", "--dry-run", "-C", proj)
	if got := core.TreeHash(proj); got != before || len(core.ListTrash("")) != 0 {
		t.Fatal("update --dry-run changed the project or trash")
	}
	if want := []core.Result{{Name: "vitest", OK: true, Message: "would update"}}; rc != 0 || !preview.OK || !slices.Equal(preview.Results, want) {
		t.Fatalf("preview: rc=%d, %+v", rc, preview)
	}
	if rc, _, errOut := run(t, "pin", "vitest", "-C", proj); rc != 0 {
		t.Fatalf("pin: %s", errOut)
	}
	before = core.TreeHash(proj)
	if rc := runJSON(t, &preview, "update", "vitest", "--dry-run", "-C", proj); rc != 1 || preview.OK || len(preview.Results) != 1 || !strings.Contains(preview.Results[0].Message, "pinned") {
		t.Fatalf("pinned preview: rc=%d, %+v", rc, preview)
	}
	if rc := runJSON(t, &preview, "update", "vitest", "--dry-run", "--force", "-C", proj); rc != 0 || !preview.OK || len(preview.Results) != 1 || preview.Results[0].Message != "would update" {
		t.Fatalf("forced preview: rc=%d, %+v", rc, preview)
	}
	if got := core.TreeHash(proj); got != before || len(core.ListTrash("")) != 0 {
		t.Fatal("forced preview changed the project or trash")
	}
	mirror := filepath.Join(core.LinkP(proj, "vitest"), "SKILL.md")
	original, err := os.ReadFile(mirror)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mirror, []byte("local Claude edit"), 0o644); err != nil {
		t.Fatal(err)
	}
	before = core.TreeHash(proj)
	if rc := runJSON(t, &preview, "update", "vitest", "--dry-run", "--force", "-C", proj); rc != 1 || preview.OK || len(preview.Results) != 1 || !strings.Contains(preview.Results[0].Message, "modified or unmanaged Claude copy") {
		t.Fatalf("modified Claude copy: rc=%d, %+v", rc, preview)
	}
	if got := core.TreeHash(proj); got != before || len(core.ListTrash("")) != 0 {
		t.Fatal("blocked preview changed the project or trash")
	}
	if err := os.WriteFile(mirror, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if rc, out, errOut := run(t, "update", "vitest", "--force", "-C", proj); rc != 0 || !strings.Contains(out, "vitest: updated") {
		t.Fatalf("update: rc=%d, %s%s", rc, out, errOut)
	}
	if rc, _, errOut := run(t, "verify", "-C", proj); rc != 0 {
		t.Fatalf("verify: %s", errOut)
	}
}

func TestTaskSelection(t *testing.T) {
	lib, original := setup(t)
	project := filepath.Join(filepath.Dir(original), "repo 'with spaces'")
	if err := os.Rename(original, project); err != nil {
		t.Fatal(err)
	}
	resolvedProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	mkskill(t, lib, "postgres-database-migration", "Review database migrations for PostgreSQL")
	mkskill(t, lib, "golang-testing", "Testing Go modules")
	for path, data := range map[string]string{"go.mod": "module example.test/sample", ".claude/worktrees/demo/pyproject.toml": "[project]"} {
		file := filepath.Join(project, path)
		if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ task, want, evidence string }{
		{"test Vue components", "vue-testing-best-practices", "package.json"},
		{"review database migrations", "postgres-database-migration", "package.json"},
		{"golang", "golang-testing", "go.mod"},
	} {
		var rec search.Recommendation
		if rc := runJSON(t, &rec, "recommend", project, "--task", tc.task, "--limit", "3", "--extra", "0"); rc != 0 {
			t.Fatal(rc)
		}
		if len(rec.Shortlist) == 0 || len(rec.Shortlist) > 3 || rec.Shortlist[0].Name != tc.want || rec.Shortlist[0].Evidence != tc.evidence {
			t.Fatalf("%s: %+v", tc.task, rec.Shortlist)
		}
		for _, g := range rec.Groups {
			if g.Pack == "python" {
				t.Fatal("detected another worktree")
			}
		}
		var planned struct {
			Project string
			Skills  []struct{ Name string }
		}
		if rc := runJSON(t, &planned, append(rec.InstallArgs, "--dry-run")...); rc != 0 || planned.Project != resolvedProject || planned.Skills[0].Name != tc.want {
			t.Fatal(planned)
		}
		var result struct {
			OK      bool
			Summary core.InstallSummary
		}
		if rc := runJSON(t, &result, "add", "-C", project, tc.want); rc != 0 || result.Summary.Installed != 1 {
			t.Fatal(result)
		}
		rec = search.Recommendation{}
		runJSON(t, &rec, "recommend", project, "--task", tc.task)
		if rec.Shortlist[0].AddedTokens != 0 || slices.Contains(rec.InstallArgs, tc.want) {
			t.Fatal(rec)
		}
		if err := os.RemoveAll(core.Dest(project, tc.want)); err != nil {
			t.Fatal(err)
		}
		rec = search.Recommendation{}
		runJSON(t, &rec, "recommend", project, "--task", tc.task, "--limit", "1")
		if len(rec.Shortlist) != 1 || rec.Shortlist[0].State != "missing" || !slices.Contains(rec.InstallArgs, tc.want) {
			t.Fatal(rec)
		}
		var dry struct {
			Added int `json:"added_tokens"`
		}
		if rc := runJSON(t, &dry, append(slices.Clone(rec.InstallArgs), "--dry-run")...); rc != 0 || dry.Added == 0 || dry.Added != rec.Shortlist[0].AddedTokens {
			t.Fatal(rec, dry)
		}
		if rc := runJSON(t, &result, rec.InstallArgs...); rc != 0 || result.Summary.Installed != 1 {
			t.Fatal(result)
		}
	}
	var rec search.Recommendation
	runJSON(t, &rec, "recommend", project, "--task", "zzzznothing")
	if len(rec.Shortlist) != 0 || rec.Message == "" || len(rec.InstallArgs) != 0 {
		t.Fatal(rec)
	}
}

func TestBriefRecommendation(t *testing.T) {
	lib, p := setup(t)
	firstVue := func() string {
		t.Helper()
		var rec search.Recommendation
		if rc := runJSON(t, &rec, "recommend", "-C", p, "--task", "vue", "--limit", "1", "--brief"); rc != 0 || len(rec.Shortlist) != 1 {
			t.Fatal(rc, rec)
		}
		return rec.Shortlist[0].Name
	}
	if name := firstVue(); name != "vue-testing-best-practices" {
		t.Fatalf("first candidate: %s", name)
	}
	// Remove a top-ranked name after indexing. Both modes should fill the vacant slot.
	if err := os.RemoveAll(filepath.Join(lib, "skills", "vue-testing-best-practices")); err != nil {
		t.Fatal(err)
	}
	if name := firstVue(); name != "vue-best-practices" {
		t.Fatalf("replacement: %s", name)
	}
	for _, task := range []string{"", "vue test", "no-matching-skill"} {
		args := []string{"recommend", "-C", p, "--limit", "3", "--task", task}
		var full, brief map[string]any
		runJSON(t, &full, args...)
		runJSON(t, &brief, append(args, "--brief")...)
		for _, key := range []string{"project", "budget", "shortlist", "install_args", "message"} {
			if !reflect.DeepEqual(full[key], brief[key]) {
				t.Fatalf("different %s: %v vs %v", key, full, brief)
			}
		}
		if _, ok := brief["groups"]; ok {
			t.Fatal("brief includes groups")
		}
		if task != "" {
			continue
		}
		shortlist := brief["shortlist"].([]any)
		if len(shortlist) != 3 {
			t.Fatal(shortlist)
		}
		packs := map[string]bool{}
		for _, v := range shortlist {
			c := v.(map[string]any)
			packs[c["reason"].(string)] = true
			if c["name"] == "vue-testing-best-practices" {
				t.Fatal("stale skill selected")
			}
		}
		if !packs["@web-vue"] || !packs["@postgres"] || !packs["@docker"] {
			t.Fatal(packs)
		}
	}
}

func TestAddOutcomesAndPackOwners(t *testing.T) {
	lib, p := setup(t)
	for _, pack := range []core.Pack{
		{Name: "empty-lib", Skills: []string{"absent"}},
		{Name: "first", Skills: []string{"tdd", "vitest", "review-pr"}},
		{Name: "second", Skills: []string{"vitest", "review-pr"}},
	} {
		if err := core.SavePack(&pack); err != nil {
			t.Fatal(err)
		}
	}
	var result struct {
		OK      bool
		Summary core.InstallSummary
	}
	add := func(args ...string) int { return runJSON(t, &result, append([]string{"add", "-C", p}, args...)...) }
	if rc := add("@empty-lib"); rc != 1 || result.OK || result.Summary.Skipped != 1 {
		t.Fatal(result)
	}
	if rc := add("review-pr", "@first", "@second", "@empty-lib"); rc != 0 || result.Summary.Installed != 3 || result.Summary.Skipped != 1 {
		t.Fatal(result)
	}
	if rc := add("review-pr"); rc != 0 || result.Summary.Present != 1 {
		t.Fatal(result)
	}
	if rc, _, e := run(t, "pack", "add", "second", "tdd"); rc != 0 {
		t.Fatal(e)
	}
	if rc, _, _ := run(t, "remove", "-C", p, "@first"); rc != 0 {
		t.Fatal(rc)
	}
	man := core.ReadManifest(p)
	if man.Skills["tdd"] != nil || !man.Skills["vitest"].HasPack("second") || !man.Skills["review-pr"].Manual {
		t.Fatal(man.Skills)
	}
	if rc, _, _ := run(t, "remove", "-C", p, "@second"); rc != 0 {
		t.Fatal(rc)
	}
	if !core.IsDir(core.Dest(p, "review-pr")) || core.IsDir(core.Dest(p, "vitest")) {
		t.Fatal("incorrect retained skills")
	}
	if err := core.CopyTree(filepath.Join(lib, "skills", "vitest"), core.Dest(p, "vitest"), nil); err != nil {
		t.Fatal(err)
	}
	if rc := add("vitest"); rc != 1 || result.Summary.Failed != 1 || core.ReadManifest(p).Skills["vitest"] != nil {
		t.Fatal(result)
	}
	if rc, _, _ := run(t, "adopt", "vitest", "-C", p); rc != 0 {
		t.Fatal(rc)
	}
}

func TestAgentSetup(t *testing.T) {
	setup(t)
	home := os.Getenv("HOME")
	if rc, out, e := run(t, "agent-setup"); rc != 0 {
		t.Fatalf("agent-setup: %s %s", out, e)
	}
	for _, d := range []string{".claude/skills/skillman", ".agents/skills/skillman"} {
		if !core.IsFile(filepath.Join(home, d, "SKILL.md")) {
			t.Errorf("%s not linked", d)
		}
	}
	runOK(t, "agent-setup")
	unmanaged := filepath.Join(home, ".agents/skills/skillman")
	if err := os.RemoveAll(unmanaged); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(unmanaged, 0o755); err != nil {
		t.Fatal(err)
	}
	rc, _, _ := run(t, "agent-setup")
	if rc != 1 || core.IsSymlink(filepath.Join(home, ".agents/skills/skillman")) {
		t.Errorf("agent-setup must refuse a real dir (rc=%d)", rc)
	}
	run(t, "agent-setup", "--remove", "--no-codex")
	if core.Lexists(filepath.Join(home, ".claude/skills/skillman")) {
		t.Error("--remove left the link")
	}
}

func TestInstallScript(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	out, err := exec.Command("bash", "-n", "../../install.sh").CombinedOutput()
	if err != nil {
		t.Errorf("install.sh syntax: %v %s", err, out)
	}
}

func TestStatusSeparatesManifestFromLibrary(t *testing.T) {
	lib, p := setup(t)
	runOK(t, "add", "-C", p, "vue-best-practices")
	status := func() core.SkillStatus {
		t.Helper()
		var st []core.SkillStatus
		if rc := runJSON(t, &st, "status", "-C", p); rc != 0 {
			t.Fatal(rc)
		}
		for _, s := range st {
			if s.Name == "vue-best-practices" {
				return s
			}
		}
		t.Fatal("not listed")
		return core.SkillStatus{}
	}
	if s := status(); s.MatchesManifest == nil || !*s.MatchesManifest || s.Library != "same" {
		t.Fatalf("fresh install: %+v", s)
	}
	// A teammate's newer library: the committed copy still matches the manifest.
	skill := filepath.Join(lib, "skills", "vue-best-practices", "SKILL.md")
	b, err := os.ReadFile(skill)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, append(b, []byte("newer\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	s := status()
	if s.State != "outdated" || !*s.MatchesManifest || s.Library != "differs" {
		t.Fatalf("newer library: %+v", s)
	}
	if _, out, _ := run(t, "status", "-C", p); !strings.Contains(out, "matches manifest; library on this machine differs") {
		t.Fatalf("text status: %s", out)
	}
}

func TestInfoKeepsFullDescriptionAndResultsGoToStdout(t *testing.T) {
	lib, p := setup(t)
	long := strings.Repeat("performance patterns for React components ", 8) + "end-of-description"
	mkskill(t, lib, "long-desc", long)
	runOK(t, "library", "rebuild-index")
	var info map[string]any
	if rc := runJSON(t, &info, "info", "long-desc"); rc != 0 || !strings.HasSuffix(info["description"].(string), "end-of-description") {
		t.Fatalf("info description cut: %v", info["description"])
	}
	var rec struct {
		Shortlist []struct{ Name, Description string }
	}
	runJSON(t, &rec, "recommend", "-C", p, "--task", "react performance patterns", "--brief")
	for _, c := range rec.Shortlist {
		if c.Name == "long-desc" && !strings.HasSuffix(c.Description, "end-of-description") {
			t.Fatalf("recommend description cut: %q", c.Description)
		}
	}
	for _, args := range [][]string{{"add", "-C", p, "long-desc"}, {"verify", "-C", p}} {
		rc, out, errOut := run(t, args...)
		if rc != 0 || !strings.Contains(out, "✓ long-desc") || strings.Contains(errOut, "✓") {
			t.Fatalf("%v: rc=%d stdout=%q stderr=%q", args, rc, out, errOut)
		}
	}
}

func TestDoctorSelfFindsBrokenInstallAndLegacyLibrary(t *testing.T) {
	lib, _ := setup(t)
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Links left by the Python installer point into a removed bin/ folder.
	for _, name := range []string{"skillman", "skill-add"} {
		if err := os.Symlink(filepath.Join(t.TempDir(), "gone", name), filepath.Join(bin, name)); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
	}
	t.Setenv("PATH", bin)
	if err := os.Symlink(filepath.Join(t.TempDir(), "scripts"), filepath.Join(lib, "scripts")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "README.md"), []byte("Run scripts/skillcore.py doctor .\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	selfSkill := filepath.Join(lib, "skills", "skillman", "SKILL.md")
	if err := os.WriteFile(selfSkill, []byte("---\nname: skillman\ndescription: old skill-add and skillcore.py instructions\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var report struct {
		OK     bool
		Checks []SelfCheck
	}
	if rc := runJSON(t, &report, "doctor", "--self"); rc != 1 || report.OK {
		t.Fatalf("broken links must fail: rc=%d", rc)
	}
	found := func(level, text string) bool {
		for _, c := range report.Checks {
			if c.Level == level && strings.Contains(c.Message, text) {
				return true
			}
		}
		return false
	}
	for _, want := range []struct{ level, text string }{
		{"error", filepath.Join(bin, "skillman") + " is a broken link"},
		{"error", filepath.Join(bin, "skill-add") + " is a broken link"},
		{"warn", "skillman is not on PATH"},
		{"warn", "broken link to the removed Python scripts"},
		{"warn", "describes the Python skillcore.py commands"},
		{"warn", "not the version shipped with this binary"},
	} {
		if !found(want.level, want.text) {
			t.Errorf("missing %s %q in %+v", want.level, want.text, report.Checks)
		}
	}
	// init repairs the library side.
	runOK(t, "init")
	report.Checks = nil
	runJSON(t, &report, "doctor", "--self")
	for _, text := range []string{"Python scripts", "skillcore.py", "not the version shipped"} {
		if found("warn", text) {
			t.Errorf("init left %q", text)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(lib, "README.md")); strings.Contains(string(b), "skillcore.py") || !strings.Contains(string(b), "skillman doctor --self") {
		t.Fatalf("README not rewritten: %s", b)
	}
}
