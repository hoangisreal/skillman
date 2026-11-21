package core

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

func dumpAnyYAML(m map[string]any) string {
	b, err := yaml.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// ───────────── CANDIDATES.md ─────────────

var (
	candRx     = regexp.MustCompile("^- \\[([ xX])\\] `([^`]+)`(.*)$")
	candRepoRx = regexp.MustCompile(`^### (\S+) \(`)
	candSubRx  = regexp.MustCompile(`<sub>([^ ]+) ·([^<]*)</sub>`)
	candTailRx = regexp.MustCompile(`\s*<sub>.*$`)
)

type Candidate struct {
	Line        int    `json:"line"`
	Repo        string `json:"repo"`
	Name        string `json:"name"`
	Ticked      bool   `json:"ticked"`
	Path        string `json:"path"`
	Flags       string `json:"flags"`
	Collides    bool   `json:"collides"`
	Description string `json:"description"`
}

func ReadCandidates() []Candidate {
	t, err := ReadText(filepath.Join(STAGING, "CANDIDATES.md"))
	if err != nil {
		return nil
	}
	var out []Candidate
	repo := ""
	for i, line := range strings.Split(strings.TrimSuffix(t, "\n"), "\n") {
		if m := candRepoRx.FindStringSubmatch(line); m != nil {
			repo = m[1]
		}
		m := candRx.FindStringSubmatch(line)
		if m == nil || repo == "" {
			continue
		}
		c := Candidate{Line: i, Repo: repo, Name: m[2], Ticked: m[1] != " "}
		if sub := candSubRx.FindStringSubmatch(m[3]); sub != nil {
			c.Path, c.Flags = sub[1], strings.TrimSpace(sub[2])
			c.Collides = strings.Contains(sub[2], "collides")
		}
		c.Description = strings.TrimLeft(candTailRx.ReplaceAllString(m[3], ""), " —")
		out = append(out, c)
	}
	return out
}

func SetTicks(changes map[int]bool) error {
	md := filepath.Join(STAGING, "CANDIDATES.md")
	unlock, err := DirLock(STAGING)
	if err != nil {
		return err
	}
	defer unlock()
	t, err := ReadText(md)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSuffix(t, "\n"), "\n")
	for i, on := range changes {
		if i >= 0 && i < len(lines) && candRx.MatchString(lines[i]) {
			mark := "- [ ]"
			if on {
				mark = "- [x]"
			}
			lines[i] = mark + lines[i][5:]
		}
	}
	return AtomicWrite(md, []byte(strings.Join(lines, "\n")+"\n"))
}

// ───────────── verify upstream repos (verify-repos.py) ─────────────

type RepoEntry struct{ Tier, Group, Repo, Hint string }

func ReadRepos(p string) ([]RepoEntry, error) {
	t, err := ReadText(p)
	if err != nil {
		return nil, err
	}
	var out []RepoEntry
	for _, l := range strings.Split(t, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		e := RepoEntry{f[0], f[1], f[2], ""}
		if len(f) > 3 {
			e.Hint = f[3]
		}
		out = append(out, e)
	}
	return out, nil
}

type CandSkill struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Lines       int    `json:"lines"`
	Description string `json:"description"`
	Collides    bool   `json:"collides"`
	YAMLError   bool   `json:"yaml_error"`
	HasHooks    bool   `json:"has_hooks"`
}

type RepoResult struct {
	Repo     string      `json:"repo"`
	Tier     string      `json:"tier"`
	Group    string      `json:"group"`
	Status   string      `json:"status"`
	Commit   string      `json:"commit,omitempty"`
	Error    string      `json:"error,omitempty"`
	Licenses []string    `json:"licenses,omitempty"`
	Skills   []CandSkill `json:"skills"`
	Seconds  float64     `json:"seconds,omitempty"`
}

var skipParts = set("node_modules", "test", "tests", "fixtures", "examples", "templates", ".github")

func verifyRepo(e RepoEntry, existing map[string]bool) RepoResult {
	t0 := time.Now()
	r := RepoResult{Repo: e.Repo, Tier: e.Tier, Group: e.Group, Skills: []CandSkill{}}
	dest := CloneDir(e.Repo)
	url := RemoteURL(e.Repo)
	g := Git([]string{"ls-remote", url, "HEAD"}, "", 60)
	if g.Code != 0 || strings.TrimSpace(g.Stdout) == "" {
		r.Status = "unreachable"
		lines := strings.Split(strings.TrimSpace(g.Stderr), "\n")
		r.Error = short(lines[len(lines)-1], 200)
		if r.Error == "" {
			r.Error = "no HEAD"
		}
		return r
	}
	r.Commit = strings.Fields(g.Stdout)[0]
	os.RemoveAll(dest)
	g = Git([]string{"clone", "--depth", "1", "--filter=blob:none", "--sparse", "--no-checkout", "-q", url, dest}, "", 180)
	if g.Code != 0 {
		r.Status, r.Error = "clone-failed", tail(g.Stderr, 200)
		return r
	}
	tree := strings.Split(Git([]string{"ls-tree", "-r", "--name-only", "HEAD"}, dest, 180).Stdout, "\n")
	var paths, lic []string
	for _, p := range tree {
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") && licenseRx.MatchString(p) {
			lic = append(lic, p)
		}
		if !(strings.HasSuffix(p, "/SKILL.md") || p == "SKILL.md") {
			continue
		}
		skip := false
		parts := strings.Split(p, "/")
		for _, x := range parts[:len(parts)-1] {
			skip = skip || skipParts[x]
		}
		if !skip {
			paths = append(paths, p)
		}
	}
	r.Licenses, r.Status = lic, "ok"
	if e.Hint != "" {
		h := strings.TrimRight(e.Hint, "/")
		var hinted []string
		for _, p := range paths {
			if strings.HasPrefix(p, h+"/") || p == h+"/SKILL.md" {
				hinted = append(hinted, p)
			}
		}
		if len(hinted) > 0 {
			paths = hinted
		} else {
			r.Status = "subpath-changed"
		}
	}
	if len(paths) == 0 {
		r.Status = "no-skills"
		return r
	}
	Git(append(append([]string{"sparse-checkout", "set", "--no-cone"}, paths...), lic...), dest, 180)
	Git([]string{"checkout", "-q", "HEAD"}, dest, 300)
	for _, p := range paths {
		text, err := ReadText(filepath.Join(dest, p))
		lines := 0
		fm := map[string]any{}
		yerr := false
		if err == nil {
			lines = strings.Count(text, "\n") + 1
			if strings.HasPrefix(text, "---") {
				parts := strings.SplitN(text, "---", 3)
				if len(parts) >= 2 {
					v, err := yamlLoad(parts[1])
					if err != nil {
						yerr = true
					} else if m, ok := v.(map[string]any); ok {
						fm = m
					}
				}
			}
		}
		dir := "."
		if strings.Contains(p, "/") {
			dir = path.Dir(p)
		}
		name := ""
		if v, ok := fm["name"]; ok && v != nil && v != "" {
			name = pyStr(v)
		}
		if name == "" {
			name = path.Base(dir)
			if dir == "." {
				name = strings.SplitN(e.Repo, "/", 2)[1]
			}
		}
		desc := ""
		if v, ok := fm["description"]; ok && v != nil {
			desc = collapseWS(pyStr(v))
		}
		_, hooks := fm["hooks"]
		r.Skills = append(r.Skills, CandSkill{Name: name, Path: dir, Lines: lines, Description: runeCut(desc, 220),
			Collides: existing[name], YAMLError: yerr, HasHooks: hooks})
	}
	r.Seconds = float64(time.Since(t0).Milliseconds()/100) / 10
	return r
}

// VerifyRepos clones every repo in repos.txt (sparse, blob-less), lists their skills and writes
// staging/CANDIDATES.md, candidates.json and VERIFY_LOG.md. Nothing is imported.
func VerifyRepos(reposFile string, jobs int, progress func(RepoResult)) ([]RepoResult, error) {
	if reposFile == "" {
		reposFile = filepath.Join(STAGING, "repos.txt")
	}
	entries, err := ReadRepos(reposFile)
	if err != nil {
		return nil, err
	}
	if jobs <= 0 {
		jobs = 8
	}
	existing := map[string]bool{}
	for _, e := range SortedDir(SKILLS) {
		if e.IsDir() {
			existing[e.Name()] = true
		}
	}
	os.MkdirAll(REPO_CACHE, 0o755)
	results := make([]RepoResult, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, jobs)
	var mu sync.Mutex
	for i, e := range entries {
		wg.Add(1)
		go func(i int, e RepoEntry) {
			defer wg.Done()
			sem <- struct{}{}
			r := verifyRepo(e, existing)
			<-sem
			results[i] = r
			if progress != nil {
				mu.Lock()
				progress(r)
				mu.Unlock()
			}
		}(i, e)
	}
	wg.Wait()
	b, _ := MarshalPy(results, 1)
	if err := AtomicWrite(filepath.Join(STAGING, "candidates.json"), b); err != nil {
		return results, err
	}
	var ok []RepoResult
	total := 0
	for _, r := range results {
		if len(r.Skills) > 0 {
			ok = append(ok, r)
			total += len(r.Skills)
		}
	}
	md := []string{"# Upstream skill candidates", "",
		fmt.Sprintf("Verified %d repos · %d with skills · %d candidate skills · generated %s", len(results), len(ok), total,
			time.Now().Format("2006-01-02 15:04")), "",
		"Mark the ones to import by changing `[ ]` to `[x]`. `collides` = a skill with that name already exists in the library.", ""}
	for _, group := range []string{"core", "data", "ops"} {
		md = append(md, "## "+group)
		for _, r := range ok {
			if r.Group != group {
				continue
			}
			lic := strings.Join(r.Licenses, ",")
			if lic == "" {
				lic = "NO ROOT LICENSE"
			}
			head := fmt.Sprintf("\n### %s (%s) · `%s` · %s", r.Repo, r.Tier, short(r.Commit, 12), lic)
			if r.Status == "subpath-changed" {
				head += " · ⚠ subpath changed"
			}
			md = append(md, head)
			skills := append([]CandSkill{}, r.Skills...)
			sort.SliceStable(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
			for _, s := range skills {
				var flags []string
				for _, f := range []struct {
					n  string
					on bool
				}{{"collides", s.Collides}, {"hooks", s.HasHooks}, {"yaml-error", s.YAMLError}, {">500 lines", s.Lines > 500}} {
					if f.on {
						flags = append(flags, f.n)
					}
				}
				fl := ""
				if len(flags) > 0 {
					fl = " · " + strings.Join(flags, " ")
				}
				md = append(md, fmt.Sprintf("- [ ] `%s` — %s <sub>%s · %dL%s</sub>", s.Name, runeCut(s.Description, 140), s.Path, s.Lines, fl))
			}
		}
		md = append(md, "")
	}
	if err := AtomicWrite(filepath.Join(STAGING, "CANDIDATES.md"), []byte(strings.Join(md, "\n")+"\n")); err != nil {
		return results, err
	}
	log := []string{"# Verify log", "", "| repo | status | detail |", "|---|---|---|"}
	for _, r := range results {
		if r.Status != "ok" {
			d := r.Error
			if d == "" {
				d = fmt.Sprint(len(r.Skills))
			}
			log = append(log, fmt.Sprintf("| %s | %s | %s |", r.Repo, r.Status, d))
		}
	}
	return results, AtomicWrite(filepath.Join(STAGING, "VERIFY_LOG.md"), []byte(strings.Join(log, "\n")+"\n"))
}

func loadCandidatesMeta() map[string]RepoResult {
	var rs []RepoResult
	LoadJSON(filepath.Join(STAGING, "candidates.json"), &rs)
	m := map[string]RepoResult{}
	for _, r := range rs {
		m[r.Repo] = r
	}
	return m
}

// TickOptions = tick-candidates.py flags.
type TickOptions struct {
	Tier       string
	MaxPerRepo int
	Repos      []string // nil = any
	Packs      []string // nil = not by pack
	Reset      bool
}

// Tick marks candidates by rule; never ticks `collides`. Returns a summary line.
func Tick(o TickOptions) (string, error) {
	meta := loadCandidatesMeta()
	if len(meta) == 0 {
		return "", errf("no staging/candidates.json (run: skillman upstream verify)")
	}
	maxPer := o.MaxPerRepo
	if maxPer <= 0 {
		maxPer = 1 << 30
	}
	okRepos := map[string]bool{}
	for name, r := range meta {
		if (o.Tier == "" || r.Tier == o.Tier) && len(r.Skills) <= maxPer && (o.Repos == nil || contains(o.Repos, name)) {
			okRepos[name] = true
		}
	}
	var wanted map[string]bool
	if o.Packs != nil {
		wanted = map[string]bool{}
		for _, p := range o.Packs {
			skills, err := ExpandPack(strings.TrimPrefix(p, "@"))
			if err != nil {
				return "", err
			}
			for _, s := range skills {
				wanted[s] = true
			}
		}
		if o.Repos == nil && o.Tier == "" {
			okRepos = map[string]bool{}
		}
	}
	cands := ReadCandidates()
	pick := map[string]int{}
	if wanted != nil {
		ordered := append([]Candidate{}, cands...)
		sort.SliceStable(ordered, func(i, j int) bool {
			return len(meta[ordered[i].Repo].Licenses) > 0 && len(meta[ordered[j].Repo].Licenses) == 0
		})
		for _, c := range ordered {
			if _, done := pick[c.Name]; wanted[c.Name] && !done && !c.Collides && (okRepos[c.Repo] || len(okRepos) == 0) {
				pick[c.Name] = c.Line
			}
		}
	}
	changes := map[int]bool{}
	for _, c := range cands {
		on := c.Ticked && !o.Reset
		if wanted != nil {
			if l, ok := pick[c.Name]; ok && l == c.Line {
				on = true
			}
		} else if okRepos[c.Repo] && !c.Collides {
			on = true
		}
		if on != c.Ticked {
			changes[c.Line] = on
		}
	}
	if err := SetTicks(changes); err != nil {
		return "", err
	}
	after := ReadCandidates()
	n := 0
	found := map[string]bool{}
	for _, c := range after {
		if c.Ticked {
			n++
			found[c.Name] = true
		}
	}
	msg := ""
	if wanted != nil {
		lib := map[string]bool{}
		for _, s := range LibraryNames() {
			lib[s] = true
		}
		f, inLib := 0, 0
		var missing []string
		for s := range wanted {
			if found[s] {
				f++
			}
			if lib[s] {
				inLib++
			}
			if !found[s] && !lib[s] {
				missing = append(missing, s)
			}
		}
		sort.Strings(missing)
		msg = fmt.Sprintf("pack skills: %d · ticked: %d · already in library: %d", len(wanted), f, inLib)
		if len(missing) > 0 {
			msg += " · not in any verified repo: " + strings.Join(missing, ", ")
		}
		msg += "\n"
	}
	return msg + fmt.Sprintf("ticked total: %d", n), nil
}

// ImportLine is one result of ImportTicked.
type ImportLine struct {
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	Result string `json:"result"` // IMPORTED UPDATED SKIPPED FAILED DRY-RUN
	Detail string `json:"detail"`
}

// ImportTicked imports skills ticked [x] in CANDIDATES.md through the safety pipeline (import-skills.py).
func ImportTicked(dry, update, renameCollisions bool, progress func(ImportLine)) ([]ImportLine, error) {
	meta := loadCandidatesMeta()
	var picks []Candidate
	for _, c := range ReadCandidates() {
		if c.Ticked {
			picks = append(picks, c)
		}
	}
	if len(picks) == 0 {
		return nil, nil
	}
	if len(meta) == 0 {
		return nil, errf("no staging/candidates.json (run: skillman upstream verify)")
	}
	os.MkdirAll(CACHE, 0o755)
	stamp := Now()
	var log []ImportLine
	add := func(l ImportLine) {
		log = append(log, l)
		if progress != nil {
			progress(l)
		}
	}
	for _, c := range picks {
		r, ok := meta[c.Repo]
		if !ok || r.Commit == "" {
			add(ImportLine{c.Repo, c.Name, "FAILED", "repo not in candidates.json; rerun upstream verify"})
			continue
		}
		destName, target := c.Name, filepath.Join(SKILLS, c.Name)
		if Lexists(target) {
			src := LoadMap(filepath.Join(target, "SOURCE.json"))
			same := getStr(src, "repo") == c.Repo
			mods := len(strList(src["local_modifications"])) > 0
			switch {
			case same && update && !mods:
			case same:
				d := "exists (use --update)"
				if mods {
					d = "has local_modifications (use skillman library check/apply)"
				}
				add(ImportLine{c.Repo, c.Name, "SKIPPED", d})
				continue
			case renameCollisions:
				destName = strings.ToLower(strings.SplitN(c.Repo, "/", 2)[0]) + "-" + c.Name
				target = filepath.Join(SKILLS, destName)
				if Lexists(target) {
					add(ImportLine{c.Repo, c.Name, "SKIPPED", destName + " also exists"})
					continue
				}
			default:
				o := getStr(src, "repo")
				if o == "" {
					o = "local skill"
				}
				add(ImportLine{c.Repo, c.Name, "SKIPPED", "name collides with " + o})
				continue
			}
		}
		if !KEBAB.MatchString(destName) {
			add(ImportLine{c.Repo, c.Name, "SKIPPED", "name not kebab-case"})
			continue
		}
		staged, mods, err := Materialize(c.Repo, r.Commit, c.Path, destName, r.Tier, nil)
		if err != nil {
			kind := "FAILED"
			if strings.Contains(err.Error(), "license") {
				kind = "SKIPPED"
			}
			add(ImportLine{c.Repo, c.Name, kind, err.Error()})
			continue
		}
		if dry {
			os.RemoveAll(staged)
			d := "-> skills/" + destName
			if len(mods) > 0 {
				d += " (" + strings.Join(mods, "; ") + ")"
			}
			add(ImportLine{c.Repo, c.Name, "DRY-RUN", d})
			continue
		}
		existed, err := InstallIntoLibrary(staged, destName)
		if err != nil {
			add(ImportLine{c.Repo, c.Name, "FAILED", err.Error()})
			continue
		}
		kind := "IMPORTED"
		if existed {
			kind = "UPDATED"
		}
		add(ImportLine{c.Repo, c.Name, kind, "skills/" + destName})
	}
	p := filepath.Join(LIB, "IMPORT_LOG.md")
	head := ""
	if !Exists(p) {
		head = "# Import log\n"
	}
	dr := ""
	if dry {
		dr = " (dry-run)"
	}
	lines := []string{fmt.Sprintf("\n## %s%s", stamp, dr), "", "| repo | skill | result | detail |", "|---|---|---|---|"}
	for _, l := range log {
		lines = append(lines, fmt.Sprintf("| %s | %s | %s | %s |", l.Repo, l.Name, l.Result, strings.ReplaceAll(l.Detail, "|", "/")))
	}
	if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.WriteString(head + strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	return log, nil
}

// BuildVendorPacks regenerates packs/vendor/<owner>.yaml for every upstream owner with >= 3 skills.
func BuildVendorPacks() (int, error) {
	byOwner := map[string][]string{}
	repos := map[string]map[string]bool{}
	norm := regexp.MustCompile(`[^a-z0-9-]+`)
	for _, n := range LibraryNames() {
		src := getStr(LoadMap(filepath.Join(SKILLS, n, "SOURCE.json")), "repo")
		if src == "" {
			continue
		}
		owner := strings.Trim(norm.ReplaceAllString(strings.ToLower(strings.SplitN(src, "/", 2)[0]), "-"), "-")
		byOwner[owner] = append(byOwner[owner], n)
		if repos[owner] == nil {
			repos[owner] = map[string]bool{}
		}
		repos[owner][src] = true
	}
	keep := map[string]bool{}
	for owner, skills := range byOwner {
		if len(skills) < 3 {
			continue
		}
		var rs []string
		for r := range repos[owner] {
			rs = append(rs, r)
		}
		sort.Strings(rs)
		if err := SavePack(&Pack{Name: "vendor/" + owner, Description: "All library skills from " + strings.Join(rs, ", "), Skills: skills}); err != nil {
			return 0, err
		}
		keep[owner+".yaml"] = true
	}
	vd := filepath.Join(PACKS, "vendor")
	for _, e := range SortedDir(vd) {
		if strings.HasSuffix(e.Name(), ".yaml") && !keep[e.Name()] {
			os.Remove(filepath.Join(vd, e.Name()))
		}
	}
	return len(keep), nil
}
