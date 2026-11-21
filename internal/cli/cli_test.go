package cli

// The agent workflow from skills/skillman/SKILL.md, run against a fixture library through Run().

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
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
