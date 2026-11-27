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

// A Next.js monorepo that tests with node --test, as in a reported project.
func TestRecommendUsesDependenciesAndNames(t *testing.T) {
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
	for name, desc := range map[string]string{
		"react-best-practices":    "React and Next.js performance guidelines for components, rendering and data fetching",
		"vite":                    "Vite config and plugins",
		"vitest":                  "Vitest unit tests and mocks",
		"nextjs-on-cloudflare":    "Deploy Next.js apps to Cloudflare Workers",
		"postgres":                "PostgreSQL " + strings.Repeat("schema design, indexing and operations guidance ", 12),
		"postgres-best-practices": "PostgreSQL schema and query best practices",
		"sql-optimization":        "SQL tips",
		"github-actions":          "GitHub Actions workflows, caching and matrices",
		"commit":                  "Use for every request to commit changes; conventional commits",
		"sandbox-next":            "Build Cloudflare Sandbox apps on the preview SDK",
	} {
		mkskill(t, lib, name, desc)
	}
	for name, skills := range map[string][]string{
		"web-react": {"react-best-practices", "vite", "vitest"},
		"postgres":  {"postgres", "postgres-best-practices", "sql-optimization"},
		"ci-github": {"github-actions", "commit"},
	} {
		if err := core.SavePack(&core.Pack{Name: name, Skills: skills}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"package.json":             `{"packageManager":"pnpm@9.0.0","scripts":{"test":"node --test"}}`,
		"apps/web/package.json":    `{"dependencies":{"next":"15","react":"19"}}`,
		"packages/db/package.json": `{"dependencies":{"pg":"8"}}`,
		".github/workflows/ci.yml": "on: push\n",
	} {
		p := filepath.Join(proj, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rec := Recommend(proj, core.LoadIndex(), "", 5, 0)
	got := map[string]Choice{}
	for _, c := range rec.Shortlist {
		got[c.Name] = c
		if len(c.Signals) == 0 {
			t.Errorf("%s has no signals", c.Name)
		}
	}
	for _, want := range []string{"react-best-practices", "postgres-best-practices", "github-actions"} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing %s in %v", want, rec.Shortlist)
		}
	}
	for _, unwanted := range []string{"vite", "vitest", "nextjs-on-cloudflare", "sql-optimization", "commit"} {
		if _, ok := got[unwanted]; ok {
			t.Errorf("suggested %s: %v", unwanted, got[unwanted].Signals)
		}
	}
	if s := strings.Join(got["react-best-practices"].Signals, "; "); !strings.Contains(s, "name matches react") {
		t.Errorf("react signals: %s", s)
	}
	// A task that names the tool still finds it.
	rec = Recommend(proj, core.LoadIndex(), "vitest unit tests", 3, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "vitest" {
		t.Fatalf("named tool: %+v", rec.Shortlist)
	}
	rec = Recommend(proj, core.LoadIndex(), "deploy Next.js to cloudflare workers", 3, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "nextjs-on-cloudflare" {
		t.Fatalf("named platform: %+v", rec.Shortlist)
	}
	// "next" as a later name segment is an ordinary word, not Next.js.
	rec = Recommend(proj, core.LoadIndex(), "sandbox preview sdk", 3, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "sandbox-next" {
		t.Fatalf("ordinary next: %+v", rec.Shortlist)
	}
	// A package.json deeper than the stack scan still counts as a dependency.
	deep := filepath.Join(proj, "services", "web", "apps", "site", "package.json")
	if err := os.MkdirAll(filepath.Dir(deep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep, []byte(`{"devDependencies":{"vitest":"3"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if deps := core.ProjectDependencies(proj); deps["vitest"] != "services/web/apps/site/package.json" {
		t.Fatalf("deep dependency: %v", deps)
	}
}

func TestRecommendSkipsNearDuplicates(t *testing.T) {
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
	mkskill(t, lib, "postgres", "PostgreSQL schema design, indexing and migrations for any database work")
	mkskill(t, lib, "postgres-best-practices", "PostgreSQL schema and query best practices")
	mkskill(t, lib, "postgres-database-migration", "Plan and review PostgreSQL schema migrations safely")
	if err := core.SavePack(&core.Pack{Name: "postgres", Skills: []string{"postgres", "postgres-best-practices", "postgres-database-migration"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"pg":"8"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	names := func(rec Recommendation) []string {
		var out []string
		for _, c := range rec.Shortlist {
			out = append(out, c.Name)
		}
		return out
	}
	got := names(Recommend(proj, core.LoadIndex(), "postgres schema", 5, 0))
	if slices.Contains(got, "postgres") && slices.Contains(got, "postgres-best-practices") {
		t.Fatalf("both near-duplicates shortlisted: %v", got)
	}
	// An installed copy wins over its near-duplicate.
	if _, err := core.Install(proj, []core.Item{{Name: "postgres-best-practices", Manual: true}}, false); err != nil {
		t.Fatal(err)
	}
	got = names(Recommend(proj, core.LoadIndex(), "postgres schema", 5, 0))
	if slices.Contains(got, "postgres") || !slices.Contains(got, "postgres-best-practices") {
		t.Fatalf("installed duplicate not preferred: %v", got)
	}
}

func TestNamesSkillTech(t *testing.T) {
	detected := map[string]bool{"docker": true, "react": true}
	for _, c := range []struct {
		name, task string
		want       bool
	}{
		{"vllm-deploy-docker", "deploy with vllm", true},
		{"vllm-deploy-docker", "docker compose deploy", false},
		{"kafka-schema-registry", "drizzle schema migration", false},
		{"docker-compose-patterns", "docker compose", false}, // the project's own stack
	} {
		if got := namesSkillTech(c.name, taskWords(c.task), detected); got != c.want {
			t.Errorf("%s / %q: %v", c.name, c.task, got)
		}
	}
}

// A skill named for the task beats one that only mentions every word in its description.
func TestRecommendPrefersNameMatches(t *testing.T) {
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
	mkskill(t, lib, "docker-compose-patterns", "Compose services, volumes and networks")
	mkskill(t, lib, "otel-collector", "Wire an OpenTelemetry collector with docker compose and deploy it")
	if err := core.SavePack(&core.Pack{Name: "docker", Skills: []string{"docker-compose-patterns"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := Recommend(proj, core.LoadIndex(), "docker compose deploy", 3, 0)
	if len(rec.Shortlist) == 0 || rec.Shortlist[0].Name != "docker-compose-patterns" {
		t.Fatalf("shortlist: %+v", rec.Shortlist)
	}
}

// When nothing local fits, recommend points to matching upstream candidates.
func TestRecommendSuggestsUpstreamCandidates(t *testing.T) {
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
	mkskill(t, lib, "react-best-practices", "React rendering guidelines")
	if err := core.SavePack(&core.Pack{Name: "web-react", Skills: []string{"react-best-practices"}}); err != nil {
		t.Fatal(err)
	}
	candidates := "# Candidates\n\n## web\n\n### acme/web-skills (official) · `abc` · LICENSE\n" +
		"- [ ] `seo` — Improve search engine visibility, meta tags and structured data <sub>skills/seo · 80L</sub>\n" +
		"- [ ] `dicom-metadata-extract` — Extract DICOM metadata from medical images <sub>skills/dicom · 40L</sub>\n" +
		"- [ ] `nuxt-seo` — SEO for Nuxt and Vue apps with metadata <sub>skills/nuxt-seo · 60L</sub>\n"
	if err := os.WriteFile(filepath.Join(lib, "staging", "CANDIDATES.md"), []byte(candidates), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "package.json"), []byte(`{"dependencies":{"react":"19"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := Recommend(proj, core.LoadIndex(), "seo metadata", 3, 0)
	if len(rec.Shortlist) != 0 || len(rec.Upstream) != 1 || rec.Upstream[0].Name != "seo" || rec.Upstream[0].Repo != "acme/web-skills" {
		t.Fatalf("upstream hints: %+v", rec.Upstream)
	}
	if !slices.Equal(rec.Upstream[0].PreviewArgs, []string{"import", "acme/web-skills", "--skill", "seo", "--dry-run"}) || !strings.Contains(rec.Message, "seo (acme/web-skills)") {
		t.Fatalf("preview: %+v %q", rec.Upstream[0], rec.Message)
	}
}

// A pack member whose description is about another technology is not
// suggested unless the task names that technology.
func TestRecommendDropsPackMembersForOtherTech(t *testing.T) {
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
	mkskill(t, lib, "react-best-practices", "React rendering guidelines")
	mkskill(t, lib, "github-actions", "GitHub Actions workflow patterns for React Native iOS simulator and Android emulator cloud builds")
	mkskill(t, lib, "gh-fix-ci", "Debug failing GitHub Actions checks on a pull request")
	for name, skills := range map[string][]string{"web-react": {"react-best-practices"}, "ci-github": {"github-actions", "gh-fix-ci"}} {
		if err := core.SavePack(&core.Pack{Name: name, Skills: skills}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := core.RebuildIndex(true); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"package.json": `{"dependencies":{"react":"19"}}`, ".github/workflows/ci.yml": "on: push\n"} {
		p := filepath.Join(proj, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	names := func(task string) []string {
		var out []string
		for _, c := range Recommend(proj, core.LoadIndex(), task, 5, 0).Shortlist {
			out = append(out, c.Name)
		}
		return out
	}
	if got := names("github actions ci"); slices.Contains(got, "github-actions") || !slices.Contains(got, "gh-fix-ci") {
		t.Fatalf("React project: %v", got)
	}
	if got := names("react native ci builds"); !slices.Contains(got, "github-actions") {
		t.Fatalf("task names React Native: %v", got)
	}
}
