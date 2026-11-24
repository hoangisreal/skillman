package search

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hoangisreal/skillman/internal/core"
)

// evalSkills is a small labelled catalog: one specialist per task family.
var evalSkills = map[string]string{
	"vue-testing-best-practices":  "Vue component testing with Vitest and Vue Test Utils",
	"vue-best-practices":          "Vue 3 composition API best practices",
	"vue-router-guide":            "Vue Router navigation guards and routes",
	"react-testing-library":       "Test React components with React Testing Library",
	"nextjs-app-router":           "Next.js App Router data fetching and server components",
	"postgres-database-migration": "Plan and review PostgreSQL schema migrations safely",
	"postgres-query-tuning":       "Tune slow PostgreSQL queries with EXPLAIN and indexes",
	"multi-stage-dockerfile":      "Write multi-stage Dockerfiles and deploy containers",
	"docker-compose-patterns":     "Docker Compose services, volumes and networks",
	"kubernetes-helm-charts":      "Write Helm charts and Kubernetes manifests",
	"terraform-modules":           "Terraform modules, state and plan review",
	"github-actions-ci":           "GitHub Actions CI workflows, caching and matrices",
	"pytest-fixtures":             "Python testing with pytest fixtures",
	"fastapi-endpoints":           "FastAPI endpoints, Pydantic models and dependency injection",
	"rust-error-handling":         "Rust error handling with thiserror and anyhow",
	"golang-testing":              "Go table-driven tests and benchmarks",
	"laravel-eloquent":            "Laravel Eloquent models and PHP migrations",
	"rails-active-record":         "Ruby on Rails Active Record queries",
	"swiftui-views":               "SwiftUI views and state management for iOS",
	"security-review":             "Security review for injection, auth and secrets",
	"tdd":                         "Test-driven development with red green refactor cycles",
	"git-commit-messages":         "Write clear git commit messages",
}

var evalPacks = map[string][]string{
	"core":      {"tdd", "git-commit-messages", "security-review"},
	"web-vue":   {"vue-testing-best-practices", "vue-best-practices", "vue-router-guide"},
	"web-react": {"react-testing-library", "nextjs-app-router"},
	"postgres":  {"postgres-database-migration", "postgres-query-tuning"},
	"docker":    {"multi-stage-dockerfile", "docker-compose-patterns"},
	"python":    {"pytest-fixtures", "fastapi-endpoints"},
}

var evalProjects = map[string]map[string]string{
	"vue":    {"package.json": `{"dependencies":{"vue":"3","pg":"8"}}`},
	"react":  {"package.json": `{"dependencies":{"react":"19","next":"15"}}`},
	"python": {"pyproject.toml": "[project]\nname = \"api\"\ndependencies = [\"fastapi\"]\n"},
	"rust":   {"Cargo.toml": "[package]\nname = \"demo\"\n"},
	"go":     {"go.mod": "module example.test/demo\n"},
	"php":    {"composer.json": `{"require":{"laravel/framework":"11"}}`},
	"ruby":   {"Gemfile": "gem \"rails\"\n"},
	"swift":  {"Package.swift": "// swift-tools-version:5.9\n"},
	"empty":  {},
}

// evalCases: an empty want means no skill fits and the shortlist should be empty.
var evalCases = []struct {
	project, task string
	want          []string
}{
	{"vue", "test vue components", []string{"vue-testing-best-practices"}},
	{"vue", "router navigation guards", []string{"vue-router-guide"}},
	{"vue", "postgres migration", []string{"postgres-database-migration"}},
	{"vue", "slow query", []string{"postgres-query-tuning"}},
	{"vue", "docker deploy", []string{"multi-stage-dockerfile"}},
	{"vue", "helm chart", []string{"kubernetes-helm-charts"}},
	{"vue", "security review", []string{"security-review"}},
	{"react", "test components", []string{"react-testing-library"}},
	{"react", "server components data fetching", []string{"nextjs-app-router"}},
	{"python", "pytest fixtures", []string{"pytest-fixtures"}},
	{"python", "api endpoints", []string{"fastapi-endpoints"}},
	{"rust", "error handling", []string{"rust-error-handling"}},
	{"go", "table tests", []string{"golang-testing"}},
	{"php", "eloquent models", []string{"laravel-eloquent"}},
	{"ruby", "active record queries", []string{"rails-active-record"}},
	{"swift", "swiftui state", []string{"swiftui-views"}},
	{"empty", "terraform plan review", []string{"terraform-modules"}},
	{"empty", "ci workflow caching", []string{"github-actions-ci"}},
	{"vue", "quantum entanglement", nil},
	{"rust", "zzz unknown capability", nil},
}

func TestRecommendationQuality(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "lib")
	t.Setenv("SKILL_LIBRARY", lib)
	t.Setenv("SKILLMAN_STATE", filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	core.Configure()
	for _, dir := range []string{"packs", "staging"} {
		if err := os.MkdirAll(filepath.Join(lib, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, desc := range evalSkills {
		mkskill(t, lib, name, desc)
	}
	for name, skills := range evalPacks {
		if err := core.SavePack(&core.Pack{Name: name, Skills: skills}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	rows := core.LoadIndex()
	projects := map[string]string{}
	for name, files := range evalProjects {
		p := filepath.Join(root, "projects", name)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		for f, body := range files {
			if err := os.WriteFile(filepath.Join(p, f), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		projects[name] = p
	}
	var positives, top1, top3, negatives, emptyOK, wrongInstall int
	for _, c := range evalCases {
		rec := Recommend(projects[c.project], rows, c.task, 3, 0)
		var got []string
		for _, s := range rec.Shortlist {
			got = append(got, s.Name)
		}
		if len(c.want) == 0 {
			negatives++
			if len(got) == 0 {
				emptyOK++
			} else {
				t.Logf("MISS  %-6s %-32q want none, got %v", c.project, c.task, got)
			}
			continue
		}
		positives++
		hit1 := len(got) > 0 && slices.Contains(c.want, got[0])
		hit3 := slices.ContainsFunc(got, func(n string) bool { return slices.Contains(c.want, n) })
		if hit1 {
			top1++
		}
		if hit3 {
			top3++
		}
		if hit3 && len(rec.InstallArgs) > 3 && !slices.ContainsFunc(rec.InstallArgs[3:], func(n string) bool { return slices.Contains(c.want, n) }) {
			wrongInstall++
		}
		if !hit1 {
			t.Logf("MISS  %-6s %-32q want %v, got %v", c.project, c.task, c.want, got)
		}
	}
	t.Logf("top-1 %d/%d, top-3 %d/%d, empty when nothing fits %d/%d, install_args without the answer %d",
		top1, positives, top3, positives, emptyOK, negatives, wrongInstall)
	// Regression gate at the measured level; raise it when ranking improves.
	if top1 < positives || top3 < positives || emptyOK < negatives || wrongInstall > 0 {
		t.Errorf("recommendation quality regressed")
	}
}

func TestTaskWordsOnlyStopWords(t *testing.T) {
	if w := taskWords("for the"); len(w) != 0 {
		t.Fatalf("stop words alone are not a task: %v", w)
	}
	if w := taskWords("tests for Vue"); strings.Join(w, " ") != "tests vue" {
		t.Fatal(w)
	}
}
