package core

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// ── diff ──

func splitKeepEnds(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.SplitAfter(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// DiffText is a unified diff of two skill dirs (Python diff_text).
func DiffText(a, b, labelA, labelB string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 4000
	}
	files := map[string]bool{}
	for _, base := range []string{a, b} {
		if !IsDir(base) {
			continue
		}
		for _, e := range Rglob(base) {
			if e.IsFile() && !hasIgnoredPart(e.Rel, HashIgnore) {
				files[e.Rel] = true
			}
		}
	}
	rels := make([]string, 0, len(files))
	for f := range files {
		rels = append(rels, f)
	}
	sort.Strings(rels)
	var out strings.Builder
	n := 0
	for _, rel := range rels {
		fa, fb := filepath.Join(a, rel), filepath.Join(b, rel)
		var la, lb []string
		if Exists(fa) {
			t, err := ReadText(fa)
			if err != nil {
				continue
			}
			la = splitKeepEnds(t)
		}
		if Exists(fb) {
			t, err := ReadText(fb)
			if err != nil {
				continue
			}
			lb = splitKeepEnds(t)
		}
		if strings.Join(la, "") == strings.Join(lb, "") {
			continue
		}
		s, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{A: la, B: lb, FromFile: labelA + "/" + rel,
			ToFile: labelB + "/" + rel, Context: 3, Eol: "\n"})
		out.WriteString(s)
		n += strings.Count(s, "\n")
		if n > maxLines {
			out.WriteString("\n… (diff truncated)\n")
			break
		}
	}
	if out.Len() == 0 {
		return "(no differences)\n"
	}
	return out.String()
}

// ── stack detection ──

type SuggestRule struct {
	Pack  string
	Globs []string
	Rx    *regexp.Regexp // nil = file presence is enough
}

func rx(s string) *regexp.Regexp { return regexp.MustCompile(s) }

var SuggestRules = []SuggestRule{
	{"web-vue", []string{"package.json"}, rx(`"(vue|nuxt|@nuxt/[^"]+|pinia|vitepress)"\s*:`)},
	{"web-react", []string{"package.json"}, rx(`"(react|next|react-dom)"\s*:`)},
	{"web-svelte", []string{"package.json"}, rx(`"(svelte|@sveltejs/kit)"\s*:`)},
	{"mobile-rn", []string{"package.json", "app.json"}, rx(`"(expo|react-native)"`)},
	{"node-backend", []string{"package.json"}, rx(`"(@nestjs/core|hono|express|fastify|prisma|@prisma/client|@apollo/server|graphql)"\s*:`)},
	{"cloudflare", []string{"wrangler.toml", "wrangler.json", "wrangler.jsonc"}, nil},
	{"python", []string{"pyproject.toml", "requirements*.txt", "setup.py", "uv.lock"}, nil},
	{"ml-train", []string{"pyproject.toml", "requirements*.txt"}, rx(`\b(torch|transformers|trl|peft|unsloth|ultralytics|wandb|mlflow|datasets|accelerate)\b`)},
	{"ml-serve", []string{"pyproject.toml", "requirements*.txt", "Dockerfile"}, rx(`\b(vllm|bentoml|tritonserver|onnxruntime)\b`)},
	{"llm-apps", []string{"pyproject.toml", "requirements*.txt", "package.json"}, rx(`\b(langchain|langgraph|langsmith|llama[-_]index|llama_parse|anthropic|openai|qdrant|pymilvus|@anthropic-ai/sdk)\b`)},
	{"data-eng", []string{"pyproject.toml", "requirements*.txt", "dbt_project.yml", "dagster.yaml"}, rx(`(?m)\b(dbt|dagster|apache-airflow|airflow|polars|duckdb|pyspark|kafka|confluent|clickhouse)\b|^name:`)},
	{"postgres", []string{"pyproject.toml", "requirements*.txt", "package.json", "docker-compose*.yml", "compose*.yml", "schema.prisma"}, rx(`\b(psycopg|asyncpg|postgres|postgresql|pg|timescale|supabase|neon)\b`)},
	{"nosql", []string{"pyproject.toml", "requirements*.txt", "package.json", "docker-compose*.yml", "compose*.yml"}, rx(`\b(redis|ioredis|mongodb|mongoose|pymongo|neo4j)\b`)},
	{"jvm", []string{"pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle.kts"}, nil},
	{"dotnet", []string{"*.csproj", "*.sln", "*.fsproj", "global.json"}, nil},
	{"flutter", []string{"pubspec.yaml"}, nil},
	{"docker", []string{"Dockerfile", "*.dockerfile", "docker-compose*.yml", "compose*.yml", "compose*.yaml"}, nil},
	{"iac", []string{"*.tf", "Pulumi.yaml", "*.pkr.hcl"}, nil},
	{"k8s-gitops", []string{"Chart.yaml", "kustomization.yaml", "kustomization.yml"}, nil},
	{"ci-github", []string{".github/workflows/*.yml", ".github/workflows/*.yaml"}, nil},
	{"web3", []string{"foundry.toml", "hardhat.config.*", "Scarb.toml", "Anchor.toml", "Move.toml"}, nil},
	{"qt", []string{"CMakeLists.txt", "*.pro", "*.qml"}, rx(`(?i)\bqt[56]?\b|find_package\(Qt|import Qt`)},
	{"research-writing", []string{"*.tex", "*.bib"}, nil},
}

// StackRules detect languages that have a taxonomy stack but no curated pack.
// Their recommendations come from library skills classified under the stack.
var StackRules = []struct {
	Stack string
	Globs []string
}{
	{"go", []string{"go.mod"}},
	{"rust", []string{"Cargo.toml"}},
	{"php", []string{"composer.json"}},
	{"ruby", []string{"Gemfile", "*.gemspec"}},
	{"ios", []string{"Package.swift"}},
}

var skipScan = set("node_modules", ".git", ".claude", ".agents", ".codex", ".opencode", ".venv", "venv", "dist", "build", "target", ".next", ".nuxt", "__pycache__")

// scanFiles walks d to depth 3 (plus .github/workflows), skipping vendored dirs. Returns relative paths.
func scanFiles(d string) []string {
	var out []string
	filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(d, p)
		if e.IsDir() {
			if p == d {
				return nil
			}
			parent := filepath.Dir(rel)
			depth := 0
			if parent != "." {
				depth = len(relParts(parent))
			}
			if skipScan[e.Name()] || (depth >= 3 && e.Name() != "workflows") {
				return filepath.SkipDir
			}
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out
}

// pathMatch = pathlib PurePath.match for relative patterns (matched from the right).
func pathMatch(rel, pattern string) bool {
	pp, pat := relParts(rel), relParts(pattern)
	if len(pat) > len(pp) {
		return false
	}
	off := len(pp) - len(pat)
	for i, g := range pat {
		if ok, _ := filepath.Match(g, pp[off+i]); !ok {
			return false
		}
	}
	return true
}

func anyGlob(rel string, globs []string) bool {
	for _, g := range globs {
		if pathMatch(rel, g) {
			return true
		}
	}
	return false
}

// Suggestion is a pack proposed for a project and the file that triggered it.
type Suggestion struct {
	Pack   string `json:"pack"`
	Reason string `json:"reason"`
	Stack  string `json:"stack,omitempty"` // a detected stack without a curated pack
}

// SuggestPacks proposes library packs from the project's manifests.
func SuggestPacks(d string) []Suggestion {
	signals := ProjectSignals(d)
	packs := signals[:0]
	for _, s := range signals {
		if s.Pack != "" {
			packs = append(packs, s)
		}
	}
	return packs
}

func ProjectSignals(d string) []Suggestion {
	available := map[string]bool{}
	for _, p := range ListPacks() {
		available[p] = true
	}
	return projectSignals(d, available)
}

func projectSignals(d string, available map[string]bool) []Suggestion {
	files := scanFiles(d)
	var out []Suggestion
	if available["core"] {
		out = append(out, Suggestion{Pack: "core", Reason: "baseline"})
	}
	for _, r := range SuggestRules {
		if !available[r.Pack] {
			continue
		}
		for _, f := range files {
			if !anyGlob(f, r.Globs) {
				continue
			}
			if r.Rx == nil {
				out = append(out, Suggestion{Pack: r.Pack, Reason: f})
				break
			}
			st, err := os.Stat(filepath.Join(d, f))
			if err != nil || st.Size() >= 2_000_000 {
				continue
			}
			b, err := os.ReadFile(filepath.Join(d, f))
			if err == nil && r.Rx.Match(b) {
				out = append(out, Suggestion{Pack: r.Pack, Reason: f})
				break
			}
		}
	}
	for _, s := range out {
		if (s.Pack == "web-vue" || s.Pack == "web-react" || s.Pack == "web-svelte") && available["web-quality"] {
			out = append(out, Suggestion{Pack: "web-quality", Reason: s.Reason})
			break
		}
	}
	seen := map[string]int{}
	var dd []Suggestion
	for _, s := range out {
		if i, ok := seen[s.Pack]; ok {
			dd[i] = s
			continue
		}
		seen[s.Pack] = len(dd)
		dd = append(dd, s)
	}
	for _, r := range StackRules {
		for _, file := range files {
			if anyGlob(file, r.Globs) {
				dd = append(dd, Suggestion{Stack: r.Stack, Reason: file})
				break
			}
		}
	}
	return dd
}

// ── audit ──

var AuditLevels = []string{"ok", "info", "review", "error"}

func levelIndex(l string) int {
	for i, x := range AuditLevels {
		if x == l {
			return i
		}
	}
	return -1
}

type AuditReport struct {
	Name        string   `json:"name"`
	Path        string   `json:"path"`
	Level       string   `json:"level"`
	Errors      []string `json:"errors"`
	Review      []string `json:"review"`
	Executables []string `json:"executables"`
}

func auditTargets(target string, library bool) ([]string, error) {
	if library {
		if !IsDir(SKILLS) {
			return nil, errf("no library skills dir at %s", SKILLS)
		}
		var out []string
		for _, e := range SortedDir(SKILLS) {
			p := filepath.Join(SKILLS, e.Name())
			if IsDir(p) && !strings.HasPrefix(e.Name(), ".") {
				out = append(out, p)
			}
		}
		return out, nil
	}
	t := expand(target)
	if md := filepath.Join(t, "SKILL.md"); IsFile(md) && !IsSymlink(md) {
		return []string{t}, nil
	}
	sk := filepath.Join(t, ".agents", "skills")
	if IsDir(sk) {
		var out []string
		for _, e := range SortedDir(sk) {
			p := filepath.Join(sk, e.Name())
			if (IsDir(p) || IsSymlink(p)) && !strings.HasPrefix(e.Name(), ".") {
				out = append(out, p)
			}
		}
		return out, nil
	}
	return nil, errf("%s: not a skill dir and no .agents/skills inside", t)
}

func isExecutable(p string, mode fs.FileMode) bool {
	return ScriptExt[filepath.Ext(p)] || mode&0o111 != 0
}

// Audit is the offline risk scan of a project's skills, one skill dir, or the library.
func Audit(target string, library bool) ([]AuditReport, error) {
	dirs, err := auditTargets(target, library)
	if err != nil {
		return nil, err
	}
	out := []AuditReport{}
	for _, d := range dirs {
		out = append(out, auditSkill(d, filepath.Base(d)))
	}
	return out, nil
}

func auditSkill(d, name string) AuditReport {
	if IsSymlink(d) {
		t, _ := os.Readlink(d)
		return AuditReport{Name: name, Path: d, Level: "review", Errors: []string{},
			Review: []string{fmt.Sprintf("skill dir is a symlink -> %s (not scanned)", t)}, Executables: []string{}}
	}
	errs, warns, _ := CheckSkill(d, name)
	review := []string{}
	for _, w := range warns {
		if strings.HasPrefix(w, "REVIEW:") {
			review = append(review, strings.TrimPrefix(w, "REVIEW: "))
		}
	}
	files := Rglob(d)
	for _, f := range files {
		parts := relParts(f.Rel)
		covered := f.Rel == "SKILL.md" || (len(parts) == 2 && parts[0] == "scripts")
		if covered || f.IsSymlink() || !f.Mode.IsRegular() {
			continue
		}
		st, err := os.Stat(f.Path)
		if err != nil || st.Size() > 2_000_000 {
			continue
		}
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			continue
		}
		head := raw
		if len(head) > 8192 {
			head = head[:8192]
		}
		if strings.IndexByte(string(head), 0) >= 0 {
			continue // binary
		}
		for _, l := range SuspiciousLabels(strings.ToValidUTF8(string(raw), "")) {
			review = append(review, fmt.Sprintf("%s in %s", l, f.Rel))
		}
	}
	exe := []string{}
	for _, f := range files {
		if f.IsSymlink() {
			review = append(review, "symlink inside skill: "+f.Rel)
		}
	}
	for _, f := range files {
		if f.Mode.IsRegular() && isExecutable(f.Path, f.Mode) {
			exe = append(exe, f.Rel)
		}
	}
	if errs == nil {
		errs = []string{}
	}
	level := "ok"
	switch {
	case len(errs) > 0:
		level = "error"
	case len(review) > 0:
		level = "review"
	case len(exe) > 0:
		level = "info"
	}
	return AuditReport{name, d, level, errs, review, exe}
}

func AuditFailed(rep []AuditReport, failOn string) bool {
	if failOn == "never" {
		return false
	}
	bar := levelIndex(failOn)
	for _, r := range rep {
		if levelIndex(r.Level) >= bar {
			return true
		}
	}
	return false
}

// ── new local skill ──
