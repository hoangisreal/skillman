package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"
)

// TreeHash is a content hash of a directory: relative paths + file bytes (+ symlink targets).
// Identical to Python tree_hash (component-wise path order, __pycache__/.DS_Store ignored).
func TreeHash(d string) string {
	h := sha256.New()
	for _, e := range Rglob(d) {
		if hasIgnoredPart(e.Rel, HashIgnore) {
			continue
		}
		if e.IsSymlink() {
			t, _ := os.Readlink(e.Path)
			h.Write([]byte("L" + e.Rel + "\x00" + t + "\x00"))
		} else if e.IsFile() {
			h.Write([]byte("F" + e.Rel + "\x00"))
			b, _ := os.ReadFile(e.Path)
			s := sha256.Sum256(b)
			h.Write(s[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ── frontmatter ──

// ParseFrontmatter mirrors Python parse_frontmatter: text.split("---", 2) + yaml.safe_load.
// Returns (fm, "") or (nil, error message).
func ParseFrontmatter(text string) (map[string]any, string) {
	if !strings.HasPrefix(text, "---") {
		return nil, "frontmatter must start on line 1 with ---"
	}
	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return nil, "unterminated frontmatter"
	}
	v, err := yamlLoad(parts[1])
	if err != nil {
		msg := strings.SplitN(err.Error(), "\n", 2)[0]
		return nil, "invalid YAML: " + strings.TrimPrefix(msg, "yaml: ")
	}
	if v == nil {
		return map[string]any{}, ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "frontmatter is not a mapping"
	}
	return m, ""
}

// yamlLoad decodes YAML leniently like PyYAML (duplicate keys: last wins).
func yamlLoad(s string) (any, error) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil {
		return nil, err
	}
	if n.Kind == 0 || len(n.Content) == 0 {
		return nil, nil
	}
	return nodeValue(n.Content[0])
}

func nodeValue(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			var k any
			if err := n.Content[i].Decode(&k); err != nil {
				return nil, err
			}
			v, err := nodeValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			m[fmt.Sprint(k)] = v
		}
		return m, nil
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			v, err := nodeValue(c)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.AliasNode:
		return nodeValue(n.Alias)
	default:
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil
	}
}

// SkillMeta returns the frontmatter of d/SKILL.md (empty on any error).
func SkillMeta(d string) map[string]any {
	t, err := ReadText(filepath.Join(d, "SKILL.md"))
	if err != nil {
		return map[string]any{}
	}
	fm, _ := ParseFrontmatter(t)
	if fm == nil {
		return map[string]any{}
	}
	return fm
}

// pyStr renders a value like Python str().
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = pyRepr(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprint(x)
	}
}

// pyRepr renders like Python repr() for the simple values found in frontmatter.
func pyRepr(v any) string {
	if s, ok := v.(string); ok {
		if strings.Contains(s, "'") && !strings.Contains(s, "\"") {
			return "\"" + s + "\""
		}
		return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
	}
	return pyStr(v)
}

func collapseWS(s string) string { return strings.Join(strings.Fields(s), " ") }

func runeCut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// ── provenance ──

var provOnce struct {
	sync.Mutex
	loaded      bool
	lock, added map[string]any
}

func resetCaches() {
	provOnce.Lock()
	provOnce.loaded = false
	provOnce.Unlock()
}

func getMap(m map[string]any, k string) map[string]any {
	if v, ok := m[k].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func getStr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// SourceOf names where a library skill came from: repo, "local" or "unknown".
func SourceOf(d string) string {
	provOnce.Lock()
	if !provOnce.loaded {
		provOnce.lock = getMap(LoadMap(filepath.Join(LIB, "provenance", ".skill-lock.json")), "skills")
		provOnce.added = getMap(LoadMap(filepath.Join(LIB, "provenance", "skill-provenance.json")), "added")
		provOnce.loaded = true
	}
	lock, added := provOnce.lock, provOnce.added
	provOnce.Unlock()
	s := LoadMap(filepath.Join(d, "SOURCE.json"))
	if r := getStr(s, "repo"); r != "" {
		return r
	}
	if r := getStr(s, "repository"); r != "" {
		return r
	}
	name := filepath.Base(d)
	if e, ok := lock[name].(map[string]any); ok {
		if v := getStr(e, "source"); v != "" {
			return v
		}
		return "unknown"
	}
	if e, ok := added[name].(map[string]any); ok {
		if v := getStr(e, "repository"); v != "" {
			return v
		}
		return "unknown"
	}
	if o := getStr(s, "origin"); o == "local" || o == "local-migrated" {
		return "local"
	}
	return "unknown"
}

// ── validation ──

type suspicious struct {
	rx    *regexp.Regexp
	label string
}

var Suspicious = []suspicious{
	{regexp.MustCompile(`(?i)(curl|wget)[^\n|]*\|\s*(sudo\s+)?(ba|z)?sh\b`), "pipe download to shell"},
	{regexp.MustCompile(`(?i)base64\s+(-d|--decode)[^\n]*\|\s*(ba|z)?sh`), "base64 decode piped to shell"},
	{regexp.MustCompile(`(?i)eval\s*\(\s*(atob|base64|Buffer\.from)`), "eval of encoded payload"},
	{regexp.MustCompile(`(?i)(cat|read|open)[^\n]{0,40}(~|\$HOME)/\.(ssh|aws|kube|netrc)`), "reads credential dir"},
	{nil, "reads .env"}, // needs a negative lookahead: see readsDotEnv
	{regexp.MustCompile(`(?i)ignore (all )?(previous|prior) instructions`), "prompt-injection phrase"},
	{regexp.MustCompile(`(?i)(nc|ncat|socat)\s+-[a-z]*e\b`), "reverse shell"},
}

var (
	dotEnvAt     = regexp.MustCompile(`(?i)\.env\b`)
	dotEnvPrefix = regexp.MustCompile(`(?i)(cat|source|read|open)\s*\(?['"]?[^\s'"]*$`)
	dotEnvSample = regexp.MustCompile(`(?i)^\.(example|template|sample)`)
)

// readsDotEnv = re.search(r"(cat|source|read|open)\s*\(?['\"]?[^\s'\"]*\.env\b(?!\.(example|template|sample))", body, re.I)
func readsDotEnv(body string) bool {
	for _, loc := range dotEnvAt.FindAllStringIndex(body, -1) {
		if dotEnvSample.MatchString(body[loc[1]:]) {
			continue
		}
		lo := loc[0] - 4096
		if lo < 0 {
			lo = 0
		}
		if dotEnvPrefix.MatchString(body[lo:loc[0]]) {
			return true
		}
	}
	return false
}

// SuspiciousLabels returns the labels of every risk pattern found in body.
func SuspiciousLabels(body string) []string {
	var out []string
	for _, s := range Suspicious {
		if s.rx == nil {
			if readsDotEnv(body) {
				out = append(out, s.label)
			}
		} else if s.rx.MatchString(body) {
			out = append(out, s.label)
		}
	}
	return out
}

type category struct {
	name string
	rx   *regexp.Regexp
}

// Categories are the legacy coarse tags (kept in INDEX.json "tags").
var Categories = []category{
	{"web", regexp.MustCompile(`\b(react|next\.?js|vue|nuxt|svelte|vite|tailwind|css|html|frontend|web|unocss|pinia|shadcn|astro|remix|tanstack)\b`)},
	{"mobile", regexp.MustCompile(`\b(expo|react native|flutter|dart|swift|swiftui|ios|android|kotlin|compose)\b`)},
	{"backend", regexp.MustCompile(`\b(api|rest|graphql|grpc|protobuf|spring|quarkus|django|fastapi|flask|nestjs|express|hono|laravel|rails|dotnet|\.net|asp\.net|server|workers|durable objects|nitro)\b`)},
	{"database", regexp.MustCompile(`\b(sql|postgres|postgresql|mysql|sqlite|supabase|redis|mongo|mongodb|duckdb|neo4j|clickhouse|prisma|ef core|database|orm)\b`)},
	{"data", regexp.MustCompile(`\b(pandas|polars|spark|dbt|airflow|dagster|kafka|etl|pipeline|dataset|spreadsheet|excel|csv|parquet)\b`)},
	{"ml-ai", regexp.MustCompile(`\b(ml|machine learning|pytorch|torch|tensorflow|hugging ?face|transformers|llm|model|training|fine-?tun|unsloth|scikit|gradio|mlops|eval)\b`)},
	{"cloud-devops", regexp.MustCompile(`\b(docker|dockerfile|kubernetes|k8s|terraform|helm|ci|github actions|aws|gcp|azure|cloudflare|deploy|observability|opentelemetry)\b`)},
	{"security", regexp.MustCompile(`\b(security|owasp|vulnerab|crypto|auth|oauth|secret|hardening|audit|variant analysis|threat)\b`)},
	{"systems", regexp.MustCompile(`\b(c\+\+|rust|go|golang|llvm|linux|kernel|embedded|reverse.engineer|binary|assembly|ebpf)\b`)},
	{"testing", regexp.MustCompile(`\b(test|tests|testing|tdd|vitest|jest|pytest|playwright|property-based|e2e|qa|verification)\b`)},
	{"design", regexp.MustCompile(`\b(design|ui|ux|typography|layout|visual|animation|figma|slides?|presentation)\b`)},
	{"docs-research", regexp.MustCompile(`\b(paper|citation|literature|research|writing|documentation|docs|prose|pdf|docx|readme)\b`)},
	{"workflow", regexp.MustCompile(`\b(commit|pr|review|plan|planning|refactor|debug|skill|workflow|ci fix)\b`)},
}

var (
	linkRx1     = regexp.MustCompile(`\]\(([^)\s#]+)(?:#[^)]*)?\)`)
	linkRx2     = regexp.MustCompile("`((?:references|scripts|assets)/[^`\\s*]+)`")
	skipLinkRx  = regexp.MustCompile(`^[a-z]+:|^/|^\$|^~`)
	broadBashRx = regexp.MustCompile(`(^|[\s,])Bash(\(\*\))?($|[\s,])`)
)

func links(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, rx := range []*regexp.Regexp{linkRx1, linkRx2} {
		for _, m := range rx.FindAllStringSubmatch(text, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				out = append(out, m[1])
			}
		}
	}
	sort.Strings(out)
	return out
}

// IndexRow is one entry of INDEX.json.
type IndexRow struct {
	Name        string   `json:"name"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	Source      string   `json:"source"`
	Lines       int      `json:"lines"`
	HasScripts  bool     `json:"has_scripts"`
	Domain      string   `json:"domain,omitempty"`
	Stacks      []string `json:"stacks,omitempty"`
}

// CheckSkill validates one skill dir -> (errors, warnings, row|nil). Same rules and messages as Python.
func CheckSkill(d, name string) ([]string, []string, *IndexRow) {
	if name == "" {
		name = filepath.Base(d)
	}
	var errs, warns []string
	md := filepath.Join(d, "SKILL.md")
	if !Exists(md) {
		return []string{"missing SKILL.md"}, nil, nil
	}
	text, err := ReadText(md)
	if err != nil {
		return []string{"missing SKILL.md"}, nil, nil
	}
	fm, ferr := ParseFrontmatter(text)
	if ferr != "" {
		errs = append(errs, ferr)
		fm = map[string]any{}
	}
	fname, hasName := fm["name"]
	descV := fm["description"]
	if !KEBAB.MatchString(name) {
		errs = append(errs, "directory name is not kebab-case")
	}
	if s, ok := fname.(string); !ok || s != name {
		if !hasName {
			fname = nil
		}
		errs = append(errs, fmt.Sprintf("frontmatter name %s != directory", pyRepr(fname)))
	}
	desc, ok := descV.(string)
	if !ok || strings.TrimSpace(desc) == "" {
		errs = append(errs, "missing description")
		desc = ""
	} else if runeLen(desc) > 1024 {
		warns = append(warns, fmt.Sprintf("description %d chars > 1024", runeLen(desc)))
	}
	nLines := strings.Count(text, "\n") + 1
	if nLines > MaxLines {
		warns = append(warns, fmt.Sprintf("SKILL.md has %d lines > %d", nLines, MaxLines))
	}
	for _, t := range links(text) {
		if skipLinkRx.MatchString(t) || strings.Contains(t, "{") || strings.Contains(t, "<") {
			continue
		}
		if !Exists(filepath.Join(d, t)) {
			warns = append(warns, "broken link: "+t)
		}
	}
	if _, ok := fm["hooks"]; ok {
		warns = append(warns, "REVIEW: frontmatter registers hooks")
	}
	var atS string
	switch at := fm["allowed-tools"].(type) {
	case []any:
		ps := make([]string, len(at))
		for i, x := range at {
			ps[i] = pyStr(x)
		}
		atS = strings.Join(ps, " ")
	case nil:
	default:
		atS = pyStr(at)
		if atS == "False" || atS == "0" || atS == "" {
			atS = "" // Python: str(at or "")
		}
	}
	if broadBashRx.MatchString(atS) {
		warns = append(warns, "REVIEW: broad allowed-tools: "+atS)
	}
	files := []string{md}
	for _, e := range Rglob(d) {
		// Python: d.rglob("scripts/*") = any direct child of a directory named "scripts"
		parts := relParts(e.Rel)
		if len(parts) >= 2 && parts[len(parts)-2] == "scripts" {
			files = append(files, e.Path)
		}
	}
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || !st.Mode().IsRegular() || st.Size() > 2_000_000 {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		body := strings.ToValidUTF8(string(b), "")
		rel, _ := filepath.Rel(d, f)
		for _, label := range SuspiciousLabels(body) {
			warns = append(warns, fmt.Sprintf("REVIEW: %s in %s", label, filepath.ToSlash(rel)))
		}
	}
	hay := strings.ToLower(strings.ReplaceAll(name, "-", " ") + " " + desc)
	var tags []string
	for _, c := range Categories {
		if c.rx.MatchString(hay) {
			tags = append(tags, c.name)
		}
	}
	if len(tags) == 0 {
		tags = []string{"general"}
	}
	row := &IndexRow{Name: name, Tags: tags, Description: runeCut(collapseWS(desc), 200), Source: SourceOf(d),
		Lines: nLines, HasScripts: IsDir(filepath.Join(d, "scripts"))}
	return errs, warns, row
}

// SkillTokens estimates the description tokens a skill injects per session.
func SkillTokens(d string) int {
	m := SkillMeta(d)
	n, ok := m["name"]
	ns := ""
	if ok {
		ns = pyStr(n)
	}
	ds := ""
	if v, ok := m["description"]; ok {
		ds = pyStr(v)
	}
	return DescriptionTokens(ns, ds)
}

// DescriptionTokens estimates the cost using metadata already in memory.
func DescriptionTokens(name, description string) int {
	return (runeLen(name)+runeLen(collapseWS(description)))/4 + 8
}
