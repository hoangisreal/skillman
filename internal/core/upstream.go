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
)

// ───────────── clones ─────────────

func RemoteURL(repo string) string { return GitBase + repo + ".git" }

func CloneDir(repo string) string {
	return filepath.Join(REPO_CACHE, strings.ReplaceAll(repo, "/", "__"))
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// EnsureClone returns a persistent blob-less clone (no checkout).
func EnsureClone(repo string) (string, error) {
	dest := CloneDir(repo)
	if Exists(filepath.Join(dest, ".git")) || Exists(filepath.Join(dest, "HEAD")) {
		return dest, nil
	}
	os.MkdirAll(REPO_CACHE, 0o755)
	legacy := filepath.Join(LegacyWork, strings.ReplaceAll(repo, "/", "__"))
	if Exists(filepath.Join(legacy, ".git")) {
		if err := CopyTree(legacy, dest, nil); err == nil {
			return dest, nil
		}
	}
	r := Git([]string{"clone", "--depth", "1", "--filter=blob:none", "--sparse", "--no-checkout", "-q", RemoteURL(repo), dest}, "", 300)
	if r.Code != 0 {
		os.RemoveAll(dest)
		return "", errf("clone failed for %s: %s", repo, tail(r.Stderr, 200))
	}
	return dest, nil
}

func haveCommit(clone, commit string) bool {
	return Git([]string{"cat-file", "-e", commit + "^{commit}"}, clone, 30).Code == 0
}

func fetchCommit(clone, commit string) error {
	if haveCommit(clone, commit) {
		return nil
	}
	r := Git([]string{"fetch", "-q", "--depth", "1", "--filter=blob:none", "origin", commit}, clone, 300)
	if r.Code != 0 || !haveCommit(clone, commit) {
		return errf("cannot fetch %s: %s", short(commit, 12), tail(r.Stderr, 200))
	}
	return nil
}

func RemoteHead(repo string) (string, error) {
	r := Git([]string{"ls-remote", RemoteURL(repo), "HEAD"}, "", 60)
	if r.Code != 0 || strings.TrimSpace(r.Stdout) == "" {
		lines := strings.Split(strings.TrimSpace(r.Stderr), "\n")
		last := lines[len(lines)-1]
		if last == "" {
			last = "no HEAD"
		}
		if len(last) > 200 {
			last = last[:200]
		}
		return "", errf("ls-remote failed for %s: %s", repo, last)
	}
	return strings.Fields(r.Stdout)[0], nil
}

func treeOID(clone, commit, p string) string {
	if p == "." {
		p = ""
	}
	r := Git([]string{"rev-parse", commit + ":" + p}, clone, 30)
	if r.Code != 0 {
		return ""
	}
	return strings.TrimSpace(r.Stdout)
}

var licenseRx = regexp.MustCompile(`(?i)^(license|licence|copying)`)
var licenseLocalRx = regexp.MustCompile(`(?i)^(licen[cs]e|copying)`)

func rootLicenses(clone, commit string) []string {
	r := Git([]string{"ls-tree", "--name-only", commit}, clone, 30)
	var out []string
	for _, l := range strings.Split(r.Stdout, "\n") {
		if l != "" && licenseRx.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

func short(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ───────────── materialize ─────────────

var commentFM = regexp.MustCompile(`(?s)^(\s*<!--.*?-->\s*)(---\n.*?\n---\n?)(.*)$`)

func importIgnore(dir string, names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		if ExcludeDirs[n] || ExcludeFiles[n] {
			out[n] = true
		}
		if filepath.Base(dir) == "agents" && strings.HasSuffix(n, ".md") {
			out[n] = true // keep agents/openai.yaml, drop subagent definitions
		}
		if IsSymlink(filepath.Join(dir, n)) {
			out[n] = true // never follow/copy symlinks
		}
	}
	return out
}

// inlineRepoLinks: upstream symlinks are replaced by the target's content if it stays inside the repo, else dropped.
func inlineRepoLinks(clone, commit, wt, sdir, staged, work string) (inlined, dropped []string) {
	for _, e := range Rglob(sdir) {
		if !e.IsSymlink() {
			continue
		}
		if hasIgnoredPart(e.Rel, ExcludeDirs) || ExcludeFiles[path.Base(e.Rel)] {
			continue
		}
		tgt, _ := os.Readlink(e.Path)
		relToWT, _ := filepath.Rel(wt, e.Path)
		repoRel := path.Clean(path.Join(path.Dir(filepath.ToSlash(relToWT)), tgt))
		if path.IsAbs(tgt) || repoRel == ".." || strings.HasPrefix(repoRel, "../") {
			dropped = append(dropped, e.Rel)
			continue
		}
		Git([]string{"-c", "core.sparseCheckout=false", "--work-tree", wt, "checkout", commit, "--", repoRel}, clone, 300,
			"GIT_INDEX_FILE="+filepath.Join(work, "index"))
		src := filepath.Join(wt, filepath.FromSlash(repoRel))
		real, err := filepath.EvalSymlinks(src)
		wtReal, _ := filepath.EvalSymlinks(wt)
		if IsSymlink(src) || !Exists(src) || err != nil || !(real == wtReal || strings.HasPrefix(real, wtReal+string(os.PathSeparator))) {
			dropped = append(dropped, e.Rel)
			continue
		}
		dest := filepath.Join(staged, filepath.FromSlash(e.Rel))
		if !IsDir(filepath.Dir(dest)) {
			continue
		}
		if IsDir(src) {
			CopyTree(src, dest, importIgnore)
		} else {
			CopyFile(src, dest)
		}
		inlined = append(inlined, e.Rel)
	}
	return
}

var fmNameLine = regexp.MustCompile(`(?m)^name[ \t]*:.*$`)

// renameFrontmatter sets the frontmatter name, changing only the name line when possible.
func renameFrontmatter(text, dest string) (string, string, bool) {
	parts := strings.SplitN(text, "---", 3)
	if !strings.HasPrefix(text, "---") || len(parts) != 3 {
		return text, "", false
	}
	fm, _ := yamlLoad(parts[1])
	m, ok := fm.(map[string]any)
	if !ok || m == nil {
		return text, "", false
	}
	if s, ok := m["name"].(string); ok && s == dest {
		return text, "", false
	}
	old, has := m["name"]
	if !has {
		old = nil
	}
	msg := fmt.Sprintf("frontmatter name %s -> %s", pyRepr(old), pyRepr(dest))
	line := "name:" + yamlScalar(dest, 5)
	var newFM string
	if loc := fmNameLine.FindStringIndex(parts[1]); loc != nil {
		newFM = parts[1][:loc[0]] + line + parts[1][loc[1]:]
	} else {
		newFM = "\n" + line + strings.TrimPrefix(parts[1], "\n")
		if !strings.HasPrefix(parts[1], "\n") {
			newFM = "\n" + line + "\n" + parts[1]
		}
	}
	// verify; fall back to a full re-dump when the name line was not a simple scalar
	chk, _ := yamlLoad(newFM)
	cm, _ := chk.(map[string]any)
	if s, _ := cm["name"].(string); s != dest || len(cm) != len(m)+map[bool]int{true: 0, false: 1}[has] {
		m["name"] = dest
		newFM = "\n" + dumpAnyYAML(m)
	}
	return "---" + newFM + "---" + parts[2], msg, true
}

// Staged is an upstream version materialized into the cache, ready to replace skills/<name>.
type Staged struct {
	UpdateItem
	Staged       string   `json:"staged"`
	Risk         []string `json:"risk"`
	Added        []string `json:"added"`
	Removed      []string `json:"removed"`
	ChangedFiles []string `json:"changed_files"`
}

// Materialize checks out <repo>@<commit>:<subpath> into a staging dir ready to become skills/<dest>.
func Materialize(repo, commit, subpath, dest, tier string, extra map[string]any) (string, []string, error) {
	clone, err := EnsureClone(repo)
	if err != nil {
		return "", nil, err
	}
	if err := fetchCommit(clone, commit); err != nil {
		return "", nil, err
	}
	os.MkdirAll(CACHE, 0o755)
	work, err := os.MkdirTemp(CACHE, "mat-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(work)
	wt := filepath.Join(work, "wt")
	os.Mkdir(wt, 0o755)
	licRoot := rootLicenses(clone, commit)
	whole := subpath == "" || subpath == "."
	paths := []string{subpath}
	if whole {
		paths = []string{"."}
	}
	paths = append(paths, licRoot...)
	r := Git(append([]string{"-c", "core.sparseCheckout=false", "--work-tree", wt, "checkout", commit, "--"}, paths...), clone, 600,
		"GIT_INDEX_FILE="+filepath.Join(work, "index"))
	if r.Code != 0 {
		return "", nil, errf("checkout failed: %s", tail(r.Stderr, 200))
	}
	sdir := wt
	if !whole {
		sdir = filepath.Join(wt, filepath.FromSlash(subpath))
	}
	md := filepath.Join(sdir, "SKILL.md")
	if IsSymlink(md) {
		return "", nil, errf("SKILL.md is a symlink (not imported by policy)")
	}
	if !IsFile(md) {
		return "", nil, errf("%s/SKILL.md missing at %s", subpath, short(commit, 12))
	}
	var licLocal, licRootP []string
	for _, e := range SortedDir(sdir) {
		if licenseLocalRx.MatchString(e.Name()) {
			licLocal = append(licLocal, e.Name())
		}
	}
	for _, p := range licRoot {
		if IsFile(filepath.Join(wt, p)) {
			licRootP = append(licRootP, filepath.Join(wt, p))
		}
	}
	if len(licLocal) == 0 && len(licRootP) == 0 {
		return "", nil, errf("no license found")
	}
	staged := filepath.Join(work, "out")
	if err := CopyTree(sdir, staged, importIgnore); err != nil {
		return "", nil, err
	}
	inlined, dropped := inlineRepoLinks(clone, commit, wt, sdir, staged, work)
	if len(licLocal) == 0 {
		CopyFile(licRootP[0], filepath.Join(staged, "UPSTREAM-LICENSE.txt"))
	}
	var mods []string
	first5 := func(xs []string) string {
		if len(xs) > 5 {
			xs = xs[:5]
		}
		return strings.Join(xs, ", ")
	}
	if len(inlined) > 0 {
		mods = append(mods, fmt.Sprintf("inlined %d in-repo symlink(s): %s", len(inlined), first5(inlined)))
	}
	if len(dropped) > 0 {
		mods = append(mods, fmt.Sprintf("dropped %d symlink(s): %s", len(dropped), first5(dropped)))
	}
	smd := filepath.Join(staged, "SKILL.md")
	text, _ := ReadText(smd)
	if !strings.HasPrefix(text, "---") {
		if m := commentFM.FindStringSubmatch(text); m != nil {
			text = strings.TrimRight(m[2], "\n") + "\n\n" + strings.TrimSpace(m[1]) + "\n\n" + strings.TrimLeft(m[3], "\n")
			mods = append(mods, "moved leading license comment below frontmatter so SKILL.md starts with ---")
		}
	}
	if t2, msg, changed := renameFrontmatter(text, dest); changed {
		text = t2
		mods = append(mods, msg)
	}
	os.WriteFile(smd, []byte(text), 0o644)
	src := NewOMap()
	src.Set("repo", repo)
	src.Set("subpath", subpath)
	src.Set("commit", commit)
	src.Set("fetched_at", Now())
	src.Set("tier", tier)
	src.Set("license_review_required", true)
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		src.Set(k, extra[k])
	}
	if len(mods) > 0 {
		src.Set("local_modifications", mods)
	}
	if err := WriteJSON(filepath.Join(staged, "SOURCE.json"), src); err != nil {
		return "", nil, err
	}
	final, err := os.MkdirTemp(CACHE, "staged-"+dest+"-")
	if err != nil {
		return "", nil, err
	}
	os.Remove(final)
	if err := Move(staged, final); err != nil {
		return "", nil, err
	}
	return final, mods, nil
}

// InstallIntoLibrary atomically swaps a staged dir into skills/<name>. Returns whether it existed.
func InstallIntoLibrary(staged, name string) (bool, error) {
	target := filepath.Join(SKILLS, name)
	existed := Lexists(target)
	var old string
	os.MkdirAll(SKILLS, 0o755)
	if existed {
		old = filepath.Join(CACHE, fmt.Sprintf("old-%s-%d", name, os.Getpid()))
		os.RemoveAll(old)
		if err := os.Rename(target, old); err != nil {
			return existed, err
		}
	}
	if err := Move(staged, target); err != nil {
		if old != "" {
			os.Rename(old, target)
		}
		return existed, err
	}
	if old != "" {
		os.RemoveAll(old)
	}
	return existed, nil
}

// ───────────── updates ─────────────

var mechanical = []*regexp.Regexp{regexp.MustCompile(`^frontmatter name `), regexp.MustCompile(`^moved leading license comment`),
	regexp.MustCompile(`^dropped \d+ symlink`), regexp.MustCompile(`^inlined \d+ in-repo symlink`)}

// UpdateItem is one library skill with upstream provenance and its check status.
type UpdateItem struct {
	Name      string   `json:"name"`
	Repo      string   `json:"repo"`
	Subpath   string   `json:"subpath"`
	Commit    string   `json:"commit"`
	Tier      string   `json:"tier"`
	Pinned    bool     `json:"pinned"`
	Mods      []string `json:"mods"`
	Status    string   `json:"status,omitempty"`
	NewCommit string   `json:"new_commit,omitempty"`
	OldTree   string   `json:"old_tree,omitempty"`
	NewTree   string   `json:"new_tree,omitempty"`
	Error     string   `json:"error,omitempty"`
}

func UpstreamSkills(names []string) ([]UpdateItem, []string) {
	pool := names
	if len(pool) == 0 {
		pool = LibraryNames()
	}
	var ups []UpdateItem
	none := []string{}
	for _, n := range pool {
		s := LoadMap(filepath.Join(SKILLS, n, "SOURCE.json"))
		repo, commit := getStr(s, "repo"), getStr(s, "commit")
		sub, hasSub := s["subpath"].(string)
		if repo != "" && commit != "" && hasSub {
			it := UpdateItem{Name: n, Repo: repo, Subpath: sub, Commit: commit, Tier: getStr(s, "tier"), Mods: []string{}}
			it.Pinned, _ = s["pinned"].(bool)
			for _, m := range strList(s["local_modifications"]) {
				it.Mods = append(it.Mods, m)
			}
			ups = append(ups, it)
		} else {
			none = append(none, n)
		}
	}
	return ups, none
}

func changedFiles(old, nw string) (added, removed, changed []string) {
	files := func(b string) map[string]string {
		m := map[string]string{}
		for _, e := range Rglob(b) {
			if e.IsFile() && path.Base(e.Rel) != "SOURCE.json" {
				m[e.Rel] = e.Path
			}
		}
		return m
	}
	fo, fn := files(old), files(nw)
	for k := range fn {
		if _, ok := fo[k]; !ok {
			added = append(added, k)
		}
	}
	for k, p := range fo {
		q, ok := fn[k]
		if !ok {
			removed = append(removed, k)
			continue
		}
		a, _ := os.ReadFile(p)
		b, _ := os.ReadFile(q)
		if string(a) != string(b) {
			changed = append(changed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)
	return nz(added), nz(removed), nz(changed)
}

func nz(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

// Assess lists risk reasons for replacing skills/<name> with staged (empty = safe).
func Assess(name, staged string, oldMods, newMods []string) []string {
	cur := filepath.Join(SKILLS, name)
	reasons := []string{}
	eNew, wNew, _ := CheckSkill(staged, name)
	var wOld []string
	if Exists(cur) {
		_, wOld, _ = CheckSkill(cur, name)
	}
	for _, e := range eNew {
		reasons = append(reasons, "validation error: "+e)
	}
	oldSet := map[string]bool{}
	for _, w := range wOld {
		oldSet[w] = true
	}
	newW := dedup(wNew)
	sort.Strings(newW)
	for _, w := range newW {
		if !oldSet[w] && (strings.HasPrefix(w, "REVIEW:") || strings.Contains(w, "allowed-tools")) {
			reasons = append(reasons, "new "+w)
		}
	}
	if Exists(cur) {
		added, _, _ := changedFiles(cur, staged)
		for _, a := range added {
			p := filepath.Join(staged, a)
			st, err := os.Stat(p)
			if err == nil && (ScriptExt[filepath.Ext(a)] || st.Mode()&0o111 != 0) {
				reasons = append(reasons, "new executable/script: "+a)
			}
		}
	}
	for _, m := range oldMods {
		mech := false
		for _, rx := range mechanical {
			mech = mech || rx.MatchString(m)
		}
		if !mech && !contains(newMods, m) {
			reasons = append(reasons, "manual local modification would be lost: "+m)
		}
	}
	return reasons
}

// UpdateReport is the result of CheckUpdates / UpdateLibrary.
type UpdateReport struct {
	Items      []UpdateItem `json:"items"`
	NoUpstream []string     `json:"no_upstream"`
	Errors     []string     `json:"errors"`
	CheckedAt  string       `json:"checked_at"`
	Staged     []Staged     `json:"staged"`
	Results    []Result     `json:"results"`
}

func checkRepo(repo string, group []UpdateItem) ([]UpdateItem, error) {
	head, err := RemoteHead(repo)
	if err != nil {
		return nil, err
	}
	var out []UpdateItem
	all := true
	for _, s := range group {
		all = all && s.Commit == head
	}
	if all {
		for _, s := range group {
			s.Status, s.NewCommit = "up-to-date", head
			out = append(out, s)
		}
		return out, nil
	}
	clone, err := EnsureClone(repo)
	if err != nil {
		return nil, err
	}
	if err := fetchCommit(clone, head); err != nil {
		return nil, err
	}
	for _, s := range group {
		s.NewCommit = head
		if s.Commit == head {
			s.Status = "up-to-date"
			out = append(out, s)
			continue
		}
		oldOID := ""
		if fetchCommit(clone, s.Commit) == nil {
			oldOID = treeOID(clone, s.Commit, s.Subpath)
		}
		newOID := treeOID(clone, head, s.Subpath)
		switch {
		case newOID == "":
			s.Status = "removed-upstream"
		case oldOID == newOID:
			s.Status = "up-to-date"
		default:
			s.Status = "changed"
			if len(rootLicenses(clone, head)) == 0 {
				sp := s.Subpath
				if sp == "." {
					sp = ""
				}
				local := false
				for _, n := range strings.Split(Git([]string{"ls-tree", "--name-only", head + ":" + sp}, clone, 30).Stdout, "\n") {
					local = local || (n != "" && licenseLocalRx.MatchString(n))
				}
				if !local {
					s.Status = "license-gone"
				}
			}
		}
		s.OldTree, s.NewTree = oldOID, newOID
		out = append(out, s)
	}
	return out, nil
}

// CheckUpdates compares recorded commits with upstream HEAD (tree OID per subpath, no blobs).
// progress (optional) is called once per repo, possibly from several goroutines.
func CheckUpdates(names []string, jobs int, progress func(repo string, done, total int)) *UpdateReport {
	if jobs <= 0 {
		jobs = 8
	}
	ups, none := UpstreamSkills(names)
	byRepo := map[string][]UpdateItem{}
	var repos []string
	for _, s := range ups {
		if _, ok := byRepo[s.Repo]; !ok {
			repos = append(repos, s.Repo)
		}
		byRepo[s.Repo] = append(byRepo[s.Repo], s)
	}
	type res struct {
		items []UpdateItem
		err   string
	}
	results := make([]res, len(repos))
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	sem := make(chan struct{}, jobs)
	for i, repo := range repos {
		wg.Add(1)
		go func(i int, repo string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items, err := checkRepo(repo, byRepo[repo])
			if err != nil {
				for _, s := range byRepo[repo] {
					s.Status, s.Error = "error", err.Error()
					items = append(items, s)
				}
				results[i] = res{items, fmt.Sprintf("%s: %v", repo, err)}
			} else {
				results[i] = res{items: items}
			}
			mu.Lock()
			done++
			n := done
			mu.Unlock()
			if progress != nil {
				progress(repo, n, len(repos))
			}
		}(i, repo)
	}
	wg.Wait()
	rep := &UpdateReport{Items: []UpdateItem{}, NoUpstream: none, Errors: []string{}, CheckedAt: Now(), Staged: []Staged{}, Results: []Result{}}
	for _, r := range results {
		rep.Items = append(rep.Items, r.items...)
		if r.err != "" {
			rep.Errors = append(rep.Errors, r.err)
		}
	}
	for i := range rep.Items {
		if rep.Items[i].Pinned && rep.Items[i].Status == "changed" {
			rep.Items[i].Status = "pinned"
		}
	}
	sort.SliceStable(rep.Items, func(i, j int) bool {
		a, b := rep.Items[i], rep.Items[j]
		if (a.Status != "changed") != (b.Status != "changed") {
			return a.Status == "changed"
		}
		if a.Repo != b.Repo {
			return a.Repo < b.Repo
		}
		return a.Name < b.Name
	})
	return rep
}

// StageUpdate materializes the new upstream version of one changed skill and assesses its risk.
func StageUpdate(it UpdateItem) (Staged, error) {
	staged, mods, err := Materialize(it.Repo, it.NewCommit, it.Subpath, it.Name, it.Tier, map[string]any{"updated_from": it.Commit})
	if err != nil {
		return Staged{}, err
	}
	reasons := Assess(it.Name, staged, it.Mods, mods)
	a, r, c := changedFiles(filepath.Join(SKILLS, it.Name), staged)
	return Staged{UpdateItem: it, Staged: staged, Risk: reasons, Added: a, Removed: r, ChangedFiles: c}, nil
}

// Changelog is the upstream git log for the skill's subpath between the two commits.
func Changelog(it UpdateItem, limit int) string {
	if limit <= 0 {
		limit = 30
	}
	clone, err := EnsureClone(it.Repo)
	if err != nil {
		return "(changelog unavailable)"
	}
	r := Git([]string{"show", "-s", "--format=%ct", it.Commit}, clone, 30)
	if r.Code == 0 && strings.TrimSpace(r.Stdout) != "" {
		var since int64
		fmt.Sscan(strings.TrimSpace(r.Stdout), &since)
		Git([]string{"fetch", "-q", "--filter=blob:none", fmt.Sprintf("--shallow-since=%d", since-1), "origin", it.NewCommit}, clone, 180)
	}
	args := []string{"log", "--oneline", fmt.Sprintf("-n%d", limit), it.Commit + ".." + it.NewCommit}
	if it.Subpath != "" && it.Subpath != "." {
		args = append(args, "--", it.Subpath)
	}
	lg := Git(args, clone, 120)
	if lg.Code == 0 && strings.TrimSpace(lg.Stdout) != "" {
		return strings.TrimSpace(lg.Stdout)
	}
	return "(changelog unavailable)"
}

func libDirty(paths []string) []string {
	r := Git(append([]string{"status", "--porcelain", "--"}, paths...), LIB, 60)
	if r.Code != 0 {
		return nil
	}
	var out []string
	for _, l := range strings.Split(r.Stdout, "\n") {
		if len(l) > 3 {
			out = append(out, l[3:])
		}
	}
	return out
}

// ApplyUpdates swaps staged updates into the library (risky ones only with allowRisky) and commits.
func ApplyUpdates(items []Staged, allowRisky, commit bool) []Result {
	var out []Result
	var done []Staged
	for _, it := range items {
		if len(it.Risk) > 0 && !allowRisky {
			out = append(out, res(it.Name, false, "risky: %s", strings.Join(it.Risk, "; ")))
			continue
		}
		if !IsDir(it.Staged) {
			out = append(out, res(it.Name, false, "staged copy missing; re-run check"))
			continue
		}
		if len(libDirty([]string{"skills/" + it.Name})) > 0 {
			out = append(out, res(it.Name, false, "uncommitted changes in library copy; commit or discard first"))
			continue
		}
		if _, err := InstallIntoLibrary(it.Staged, it.Name); err != nil {
			out = append(out, res(it.Name, false, "%v", err))
			continue
		}
		done = append(done, it)
		out = append(out, res(it.Name, true, "updated %s → %s", short(it.Commit, 8), short(it.NewCommit, 8)))
	}
	if len(done) > 0 {
		RebuildIndex(true)
		probs := ValidatePacks()
		appendUpdateLog(out, probs)
		if commit && Exists(filepath.Join(LIB, ".git")) {
			args := []string{"add", "--"}
			var body []string
			for _, i := range done {
				args = append(args, "skills/"+i.Name)
				body = append(body, fmt.Sprintf("- %s: %s %s -> %s", i.Name, i.Repo, short(i.Commit, 12), short(i.NewCommit, 12)))
			}
			Git(append(args, "INDEX.md", "INDEX.json", "VALIDATION.md", "UPDATE_LOG.md"), LIB, 120)
			cr := Git([]string{"commit", "-q", "-m", fmt.Sprintf("Update %d skill(s) from upstream\n\n%s", len(done), strings.Join(body, "\n"))}, LIB, 120)
			if cr.Code != 0 {
				msg := cr.Stderr
				if strings.TrimSpace(msg) == "" {
					msg = cr.Stdout
				}
				out = append(out, res("git commit", false, "%s — changes are applied but uncommitted", tail(msg, 200)))
			}
		}
	}
	return out
}

func appendUpdateLog(rs []Result, probs []PackProblem) {
	p := filepath.Join(LIB, "UPDATE_LOG.md")
	var lines []string
	if !Exists(p) {
		lines = []string{"# Update log"}
	}
	lines = append(lines, "", "## "+Now(), "", "| skill | result | detail |", "|---|---|---|")
	for _, r := range rs {
		ok := "SKIP"
		if r.OK {
			ok = "OK"
		}
		lines = append(lines, fmt.Sprintf("| %s | %s | %s |", r.Name, ok, strings.ReplaceAll(r.Message, "|", "/")))
	}
	if len(probs) > 0 {
		lines = append(lines, "", "Pack problems after update:")
		for _, pp := range probs {
			lines = append(lines, fmt.Sprintf("- %s: %s", pp.Pack, pp.Problem))
		}
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
}

func SavePending(items []Staged) error {
	if items == nil {
		items = []Staged{}
	}
	o := NewOMap()
	o.Set("saved_at", Now())
	o.Set("items", items)
	return WriteJSON(filepath.Join(UPDATES, "pending.json"), o)
}

func LoadPending() []Staged {
	var data struct {
		Items []Staged `json:"items"`
	}
	LoadJSON(filepath.Join(UPDATES, "pending.json"), &data)
	var out []Staged
	for _, i := range data.Items {
		if i.Staged != "" && IsDir(i.Staged) {
			out = append(out, i)
		}
	}
	return out
}

// UpdateLibrary: mode check | apply | auto-safe.
func UpdateLibrary(names []string, mode string, allowRisky, commit bool, progress func(string, int, int)) *UpdateReport {
	rep := CheckUpdates(names, 8, progress)
	if mode == "apply" || mode == "auto-safe" {
		var staged []Staged
		for _, it := range rep.Items {
			if it.Status != "changed" {
				continue
			}
			s, err := StageUpdate(it)
			if err != nil {
				rep.Results = append(rep.Results, res(it.Name, false, "stage failed: %v", err))
				continue
			}
			staged = append(staged, s)
		}
		allow := allowRisky && mode == "apply"
		rep.Results = append(rep.Results, ApplyUpdates(staged, allow, commit)...)
		var risky []Staged
		for _, s := range staged {
			if len(s.Risk) > 0 && !allow {
				risky = append(risky, s)
			} else {
				os.RemoveAll(s.Staged)
			}
		}
		if len(risky) > 0 {
			SavePending(risky)
		}
		rep.Staged = append(rep.Staged, staged...)
	}
	return rep
}

func SetLibraryPin(name string, pinned bool) error {
	if err := CheckName(name); err != nil {
		return err
	}
	p := filepath.Join(SKILLS, name, "SOURCE.json")
	if !Exists(p) {
		return errf("%s: no SOURCE.json (no upstream to pin against)", name)
	}
	s := LoadOMap(p)
	if pinned {
		s.Set("pinned", true)
	} else {
		s.Delete("pinned")
	}
	return WriteJSON(p, s)
}

// Rollback restores skills/<name> to the version before the last commit that touched it, and pins it.
func Rollback(name string, commit bool) (string, error) {
	if err := CheckName(name); err != nil {
		return "", err
	}
	r := Git([]string{"log", "-n", "1", "--format=%H", "--", "skills/" + name}, LIB, 60)
	last := strings.TrimSpace(r.Stdout)
	if last == "" {
		return "", errf("%s: no git history", name)
	}
	if Git([]string{"cat-file", "-e", last + "^:skills/" + name + "/SKILL.md"}, LIB, 30).Code != 0 {
		return "", errf("%s: did not exist before %s (nothing to roll back to)", name, short(last, 8))
	}
	if len(libDirty([]string{"skills/" + name})) > 0 {
		return "", errf("%s: uncommitted changes; commit or discard first", name)
	}
	Git([]string{"rm", "-rq", "--", "skills/" + name}, LIB, 60)
	co := Git([]string{"checkout", last + "^", "--", "skills/" + name}, LIB, 60)
	if co.Code != 0 {
		Git([]string{"checkout", "HEAD", "--", "skills/" + name}, LIB, 60)
		return "", errf("rollback failed: %s", strings.TrimSpace(co.Stderr))
	}
	if Exists(filepath.Join(SKILLS, name, "SOURCE.json")) {
		SetLibraryPin(name, true)
	}
	RebuildIndex(true)
	if commit {
		Git([]string{"add", "-A", "--", "skills/" + name, "INDEX.md", "INDEX.json", "VALIDATION.md"}, LIB, 60)
		cr := Git([]string{"commit", "-q", "-m", fmt.Sprintf("Roll back %s to %s^ (pinned)", name, short(last, 12))}, LIB, 60)
		if cr.Code != 0 {
			msg := cr.Stderr
			if strings.TrimSpace(msg) == "" {
				msg = cr.Stdout
			}
			return fmt.Sprintf("%s rolled back and pinned, but git commit failed: %s", name, tail(msg, 200)), nil
		}
	}
	return fmt.Sprintf("%s rolled back to %s^ and pinned (unpin: skillman library unpin %s)", name, short(last, 12), name), nil
}
