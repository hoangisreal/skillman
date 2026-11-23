package search

import (
	"os"
	"path/filepath"
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

	rec := Recommend(proj, rows, "test Vue components", 5, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "vue-testing-best-practices" {
		t.Fatalf("task shortlist: %+v", rec.Shortlist)
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
