package search

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
)

// setupBench builds a synthetic library of n skills and a Vue project.
func setupBench(b *testing.B, n int) (string, []core.IndexRow) {
	b.Helper()
	root := b.TempDir()
	lib := filepath.Join(root, "lib")
	b.Setenv("SKILL_LIBRARY", lib)
	b.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	b.Setenv("HOME", filepath.Join(root, "home"))
	core.Configure()
	for _, dir := range []string{"packs", "staging"} {
		if err := os.MkdirAll(filepath.Join(lib, dir), 0o755); err != nil {
			b.Fatal(err)
		}
	}
	kinds := []string{"Vue component testing with Vitest", "PostgreSQL schema migrations", "Docker Compose services", "Rust error handling", "Generic engineering guide"}
	var vue []string
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("skill-%04d", i)
		d := filepath.Join(lib, "skills", name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			b.Fatal(err)
		}
		body := "---\nname: " + name + "\ndescription: " + kinds[i%len(kinds)] + "\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(body), 0o644); err != nil {
			b.Fatal(err)
		}
		if i%len(kinds) == 0 && len(vue) < 20 {
			vue = append(vue, name)
		}
	}
	if err := core.SavePack(&core.Pack{Name: "web-vue", Skills: vue}); err != nil {
		b.Fatal(err)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		b.Fatal(err)
	}
	proj := filepath.Join(root, "app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"vue":"3"}}`), 0o644); err != nil {
		b.Fatal(err)
	}
	return proj, core.LoadIndex()
}

// Run: go test ./internal/search -run '^$' -bench Rank3000 -benchmem
func BenchmarkRank3000Task(b *testing.B) {
	proj, rows := setupBench(b, 3000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Rank(core.NewRecommendationCatalog(), proj, rows, "docker compose services", 5, 0, true)
	}
}

func BenchmarkRank3000NoTask(b *testing.B) {
	proj, rows := setupBench(b, 3000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Rank(core.NewRecommendationCatalog(), proj, rows, "", 5, 0, true)
	}
}
