package search

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
)

func mkskill(t *testing.T, lib, name, desc string) {
	t.Helper()
	d := filepath.Join(lib, "skills", name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRecommendTaskCoverage(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	proj := filepath.Join(root, "app")
	t.Setenv("SKILL_LIBRARY", lib)
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	core.Configure()
	if err := os.MkdirAll(filepath.Join(lib, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(lib, "staging"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkskill(t, lib, "vue-testing-best-practices", "Use for Vue.js testing. Covers Vitest, Vue Test Utils, component testing, mocking, testing patterns, and Playwright for E2E testing.")
	mkskill(t, lib, "vue-best-practices", "Vue 3 composition API best practices")
	mkskill(t, lib, "vue-alpha-guide", "Vue coding guide for teams working across multiple packages and environments")
	mkskill(t, lib, "vue-zeta-guide", "Vue coding guide")
	mkskill(t, lib, "postgres-best-practices", "PostgreSQL schema and query best practices")
	mkskill(t, lib, "postgres-database-migration", "Use this skill for planning, testing, and safely executing PostgreSQL schema migrations — especially when working with production data or shared databases.")
	if err := core.SavePack(&core.Pack{Name: "web-vue", Skills: []string{"vue-best-practices", "vue-testing-best-practices"}}); err != nil {
		t.Fatal(err)
	}
	if err := core.SavePack(&core.Pack{Name: "postgres", Skills: []string{"postgres-best-practices", "postgres-database-migration"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"vue":"3","pg":"8"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := core.LoadIndex()

	tie := Recommend(proj, rows, "coding guide", 2, 0)
	if len(tie.Shortlist) != 2 || tie.Shortlist[0].Name != "vue-zeta-guide" {
		t.Fatalf("equivalent guides should prefer lower added cost: %+v", tie.Shortlist)
	}
	if _, err := core.Install(proj, []core.Item{{Name: "vue-alpha-guide", Manual: true}}, false); err != nil {
		t.Fatal(err)
	}
	tie = Recommend(proj, rows, "coding guide", 2, 0)
	if len(tie.Shortlist) != 2 || tie.Shortlist[0].Name != "vue-alpha-guide" || tie.Shortlist[0].State != "ok" {
		t.Fatalf("an equivalent matching project copy should come first: %+v", tie.Shortlist)
	}

	rec := Recommend(proj, rows, "test Vue components", 5, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "vue-testing-best-practices" {
		t.Fatalf("task shortlist: %+v", rec.Shortlist)
	}
	t.Setenv("SKILL_TOKEN_BUDGET", "1")
	rec = Recommend(proj, rows, "test Vue components", 5, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "vue-testing-best-practices" || !strings.Contains(rec.Message, "exceeds the description token budget") {
		t.Fatalf("budget warning must preserve the specialist: %+v", rec)
	}
	rec = Recommend(proj, rows, "review database migrations", 3, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "postgres-database-migration" {
		t.Fatal(rec.Shortlist)
	}
	// One generic match alone still cannot fill the shortlist.
	rec = Recommend(proj, rows, "vue zzzmissing", 5, 0)
	if len(rec.Shortlist) != 0 || rec.Message == "" {
		t.Fatalf("unmatched task should be empty: %+v", rec.Shortlist)
	}
	// Limit and determinism.
	rec = Recommend(proj, rows, "", 1, 0)
	if len(rec.Shortlist) != 1 {
		t.Fatalf("limit: %+v", rec.Shortlist)
	}
	again := Recommend(proj, rows, "", 1, 0)
	if again.Shortlist[0].Name != rec.Shortlist[0].Name {
		t.Fatal("recommendation not deterministic")
	}
}

func TestRecommendTaskSearchesWholeLibrary(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	proj := filepath.Join(root, "app")
	t.Setenv("SKILL_LIBRARY", lib)
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	core.Configure()
	for _, dir := range []string{"packs", "staging"} {
		if err := os.MkdirAll(filepath.Join(lib, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mkskill(t, lib, "vue-testing-best-practices", "Use for Vue.js testing. Covers Vitest, Vue Test Utils and component testing.")
	mkskill(t, lib, "vue-best-practices", "Vue 3 composition API best practices")
	mkskill(t, lib, "multi-stage-dockerfile", "Write multi-stage Dockerfiles and deploy containers with Docker Compose")
	mkskill(t, lib, "pytest-testing", "Python testing with pytest fixtures and component testing")
	mkskill(t, lib, "testing-library-patterns", "Testing Library patterns for any framework")
	if err := core.SavePack(&core.Pack{Name: "web-vue", Skills: []string{"vue-best-practices", "vue-testing-best-practices"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"vue":"3"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := core.LoadIndex()

	// A task outside the detected stacks still finds the local skill.
	rec := Recommend(proj, rows, "docker deploy", 3, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "multi-stage-dockerfile" || rec.Shortlist[0].Evidence != "library" {
		t.Fatalf("off-stack task: %+v %s", rec.Shortlist, rec.Message)
	}
	if len(rec.InstallArgs) == 0 || rec.InstallArgs[len(rec.InstallArgs)-1] != "multi-stage-dockerfile" {
		t.Fatalf("install args: %v", rec.InstallArgs)
	}
	// A stack match wins over an equally good match elsewhere.
	rec = Recommend(proj, rows, "component testing", 3, 0)
	if len(rec.Shortlist) < 2 || rec.Shortlist[0].Name != "vue-testing-best-practices" || rec.Shortlist[1].Name != "pytest-testing" {
		t.Fatalf("stack preference: %+v", rec.Shortlist)
	}
	// A stronger name match elsewhere still needs twice the score to win,
	// and off-stack fillers stay out of the default install.
	rec = Recommend(proj, rows, "testing", 3, 0)
	if len(rec.Shortlist) < 2 || rec.Shortlist[0].Name != "vue-testing-best-practices" || rec.Shortlist[1].Name != "testing-library-patterns" {
		t.Fatalf("stack bonus: %+v", rec.Shortlist)
	}
	if !slices.Contains(rec.InstallArgs, "vue-testing-best-practices") || slices.Contains(rec.InstallArgs, "testing-library-patterns") || slices.Contains(rec.InstallArgs, "pytest-testing") {
		t.Fatalf("install args: %v", rec.InstallArgs)
	}
	// Filler words do not count as matches.
	rec = Recommend(proj, rows, "write tests for vue components", 3, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "vue-testing-best-practices" || slices.Contains(rec.Shortlist[0].Matched, "for") {
		t.Fatalf("stop words: %+v", rec.Shortlist)
	}
	// Without a task the shortlist stays within the project's stacks.
	for _, c := range Recommend(proj, rows, "", 5, 0).Shortlist {
		if c.Evidence == "library" {
			t.Fatalf("unrelated skill without a task: %+v", c)
		}
	}
}

func TestRecommendBudgetCountsWholeInstall(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	proj := filepath.Join(root, "app")
	t.Setenv("SKILL_LIBRARY", lib)
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	core.Configure()
	for _, dir := range []string{"packs", "staging"} {
		if err := os.MkdirAll(filepath.Join(lib, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mkskill(t, lib, "vue-testing-best-practices", "Vue component testing with Vitest")
	mkskill(t, lib, "vue-router-guide", "Vue Router testing and navigation guards")
	if err := core.SavePack(&core.Pack{Name: "web-vue", Skills: []string{"vue-testing-best-practices", "vue-router-guide"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"vue":"3"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := core.LoadIndex()
	rec := Recommend(proj, rows, "vue testing", 2, 0)
	if len(rec.Shortlist) != 2 || len(rec.InstallArgs) != 5 {
		t.Fatalf("setup: %+v %v", rec.Shortlist, rec.InstallArgs)
	}
	if want := rec.Shortlist[0].AddedTokens + rec.Shortlist[1].AddedTokens; rec.InstallTokens != want {
		t.Fatalf("install tokens %d, want %d", rec.InstallTokens, want)
	}
	// Each skill fits on its own; the two together do not.
	t.Setenv("SKILL_TOKEN_BUDGET", strconv.Itoa(rec.Shortlist[0].AddedTokens+1))
	rec = Recommend(proj, rows, "vue testing", 2, 0)
	if !strings.Contains(rec.Message, "suggested install exceeds") || strings.Contains(rec.Message, "Top choice") {
		t.Fatalf("message: %q", rec.Message)
	}
}
