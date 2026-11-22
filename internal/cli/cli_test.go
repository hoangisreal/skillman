package cli

// The agent workflow from skills/skillman/SKILL.md, run against a fixture library through Run().

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	if !core.IsSymlink(filepath.Join(proj, ".claude/skills/vue-best-practices")) {
		t.Error("claude link missing")
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

func TestTaskSelection(t *testing.T) {
	lib, original := setup(t)
	project := filepath.Join(filepath.Dir(original), "repo 'with spaces'")
	if err := os.Rename(original, project); err != nil {
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
		if rc := runJSON(t, &planned, append(rec.InstallArgs, "--dry-run")...); rc != 0 || planned.Project != project || planned.Skills[0].Name != tc.want {
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
	// idempotent, and never touches a real directory
	os.Remove(filepath.Join(home, ".agents/skills/skillman"))
	os.MkdirAll(filepath.Join(home, ".agents/skills/skillman"), 0o755)
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
