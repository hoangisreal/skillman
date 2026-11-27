package core

import (
	"crypto/sha256"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var repoSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)
var fullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type ImportSkill struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

type ImportSource struct {
	Repo    string        `json:"repo"`
	Commit  string        `json:"commit"`
	Subpath string        `json:"subpath"`
	Skills  []ImportSkill `json:"skills"`
}

// ImportRepo returns the owner/repo of an import source.
func ImportRepo(source string) (string, bool) {
	repo, _, err := sourceParts(source)
	return repo, err == nil
}

// sourceParts accepts a GitHub slug or HTTPS repository/tree URL.
func sourceParts(source string) (repo, tree string, err error) {
	raw := source
	if strings.Contains(source, "://") {
		u, e := url.Parse(source)
		if e != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, "github.com") || u.User != nil {
			return "", "", errf("expected a GitHub HTTPS URL")
		}
		raw = strings.TrimPrefix(u.Path, "/")
	}
	parts := strings.Split(strings.TrimSuffix(raw, "/"), "/")
	for _, p := range parts {
		if p == "" || p == "." || p == ".." || strings.ContainsAny(p, "\\\x00\r\n") {
			return "", "", errf("invalid GitHub source path")
		}
	}
	if len(parts) < 2 {
		return "", "", errf("expected owner/repo")
	}
	repo = parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	if !repoSlug.MatchString(repo) || path.Base(repo) == "." || path.Base(repo) == ".." {
		return "", "", errf("invalid GitHub repository")
	}
	if len(parts) > 2 {
		if raw == source || parts[2] != "tree" || len(parts) < 4 {
			return "", "", errf("expected a GitHub tree URL")
		}
		tree = strings.Join(parts[3:], "/")
	}
	return repo, tree, nil
}

// Branch names can contain slashes. Prefer the longest matching advertised ref.
func treeRevision(repo, tree string) (commit, dir string, err error) {
	first, rest, _ := strings.Cut(tree, "/")
	if fullSHA.MatchString(first) {
		return first, rest, nil
	}
	refs := Git([]string{"ls-remote", "--heads", "--tags", RemoteURL(repo)}, "", 60)
	if refs.Code != 0 {
		return "", "", errf("cannot resolve %s: %s", repo, tail(refs.Stderr, 200))
	}
	best := ""
	for _, line := range strings.Split(refs.Stdout, "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/tags/"), "^{}")
		if (tree == name || strings.HasPrefix(tree, name+"/")) && len(name) >= len(best) {
			best, commit = name, sha
		}
	}
	if best == "" {
		return "", "", errf("no matching branch or tag for %s", tree)
	}
	return commit, strings.TrimPrefix(strings.TrimPrefix(tree, best), "/"), nil
}

func DiscoverImport(source, pin string) (ImportSource, error) {
	r := ImportSource{Skills: []ImportSkill{}}
	repo, tree, err := sourceParts(source)
	if err != nil {
		return r, err
	}
	r.Repo = repo
	if pin != "" && !fullSHA.MatchString(pin) {
		return r, errf("--commit requires a full commit SHA")
	}
	dir := ""
	switch {
	case tree != "":
		r.Commit, dir, err = treeRevision(repo, tree)
	case pin == "":
		r.Commit, err = RemoteHead(repo)
	}
	if err != nil {
		return r, err
	}
	if pin != "" {
		r.Commit = strings.ToLower(pin)
	}
	r.Subpath = dir
	if r.Subpath == "" {
		r.Subpath = "."
	}
	// Serialize clone creation across direct imports, including list and dry-run.
	unlock, err := DirLock(REPO_CACHE)
	if err != nil {
		return r, err
	}
	clone, err := EnsureClone(repo)
	unlock()
	if err != nil {
		return r, err
	}
	if err = fetchCommit(clone, r.Commit); err != nil {
		return r, err
	}
	tr := Git([]string{"ls-tree", "-rz", r.Commit}, clone, 180)
	if tr.Code != 0 {
		return r, errf("cannot list tree: %s", tail(tr.Stderr, 200))
	}
	for _, line := range strings.Split(tr.Stdout, "\x00") {
		meta, file, ok := strings.Cut(line, "\t")
		if !ok || path.Base(file) != "SKILL.md" || !strings.HasPrefix(meta, "100") {
			continue
		}
		folder := path.Dir(file)
		if dir != "" && folder != dir {
			continue
		}
		if dir == "" && hasIgnoredPart(folder, skipParts) {
			continue
		}
		blob := Git([]string{"cat-file", "blob", r.Commit + ":" + file}, clone, 30)
		if blob.Code != 0 {
			return r, errf("cannot read %s: %s", file, tail(blob.Stderr, 200))
		}
		body := blob.Stdout
		if m := commentFM.FindStringSubmatch(body); m != nil {
			body = m[2]
		}
		fm, _ := ParseFrontmatter(body)
		name := getStr(fm, "name")
		if name == "" {
			name = path.Base(folder)
			if folder == "." {
				name = path.Base(repo)
			}
		}
		r.Skills = append(r.Skills, ImportSkill{name, folder, collapseWS(getStr(fm, "description"))})
	}
	if len(r.Skills) == 0 {
		return r, errf("no skills found at %s:%s", repo, r.Subpath)
	}
	return r, nil
}

func (r ImportSource) Select(names []string) ([]ImportSkill, error) {
	if len(names) == 0 && len(r.Skills) == 1 {
		names = []string{r.Skills[0].Name}
	}
	var selected []ImportSkill
	for _, name := range dedup(names) {
		var matches []ImportSkill
		for _, s := range r.Skills {
			if s.Name == name {
				matches = append(matches, s)
			}
		}
		switch len(matches) {
		case 0:
			return nil, errf("unknown skill %q", name)
		case 1:
			selected = append(selected, matches[0])
		default:
			return nil, errf("ambiguous skill %q; use its GitHub directory URL", name)
		}
	}
	return selected, nil
}

// Compare the materialized payload, including added files and executable bits.
// SOURCE.json also carries mutable library metadata such as the pin and fetch date.
func sameImport(existing, staged string) bool {
	type entry struct {
		mode   fs.FileMode
		digest [sha256.Size]byte
	}
	snapshot := func(root string) (map[string]entry, error) {
		files := map[string]entry{}
		err := fs.WalkDir(os.DirFS(root), ".", func(rel string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if rel == "." || rel == "SOURCE.json" {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			e := entry{mode: info.Mode() & (fs.ModeType | 0111)}
			if info.Mode().IsRegular() {
				b, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					return err
				}
				e.digest = sha256.Sum256(b)
			} else if !info.IsDir() {
				return errf("unexpected file type at %s", rel)
			}
			files[rel] = e
			return nil
		})
		return files, err
	}
	a, err := snapshot(existing)
	if err != nil {
		return false
	}
	b, err := snapshot(staged)
	return err == nil && maps.Equal(a, b)
}

type LibraryImport struct {
	ImportSkill
	Action  string `json:"action"`
	Detail  string `json:"detail,omitempty"`
	License string `json:"license,omitempty"` // first line of the license file that ships with the skill
	Audit   string `json:"audit,omitempty"`   // audit level of the staged copy: ok, info, review
}

// LicenseSummary names the license that ships in a skill folder: the first
// non-empty line of LICENSE*, COPYING* or UPSTREAM-LICENSE.txt.
func LicenseSummary(dir string) string {
	for _, e := range SortedDir(dir) {
		n := strings.ToUpper(e.Name())
		if !e.Type().IsRegular() || !(strings.HasPrefix(n, "LICENSE") || strings.HasPrefix(n, "LICENCE") || strings.HasPrefix(n, "COPYING") || n == "UPSTREAM-LICENSE.TXT") {
			continue
		}
		text, err := ReadText(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "#=*-")); line != "" {
				return runeCut(line, 80)
			}
		}
	}
	return ""
}

func importOne(src ImportSource, s ImportSkill, dry, allowRisky bool, preview func(name, source string), info *LibraryImport) (string, string, error) {
	if err := CheckName(s.Name); err != nil {
		return "", "", err
	}
	target := filepath.Join(SKILLS, s.Name)
	exists := Lexists(target)
	var provenance map[string]any
	if exists {
		manifest := filepath.Join(target, "SOURCE.json")
		if IsSymlink(target) || IsSymlink(manifest) {
			return "", "", errf("conflict: symlink at library destination")
		}
		provenance = LoadMap(manifest)
		if !strings.EqualFold(getStr(provenance, "repo"), src.Repo) || path.Clean(getStr(provenance, "subpath")) != s.Path {
			return "", "", errf("name conflict: existing library copy has a different source")
		}
		if getStr(provenance, "commit") != src.Commit {
			return "", "", errf("version conflict: existing library commit %s", getStr(provenance, "commit"))
		}
	}
	staged, mods, err := Materialize(src.Repo, src.Commit, s.Path, s.Name, "community", nil)
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(staged)
	info.License = LicenseSummary(staged)
	info.Audit = auditSkill(staged, s.Name).Level
	note, err := checkImportAudit(staged, s.Name, allowRisky)
	if err != nil {
		return "", "", err
	}
	if exists {
		if !slices.Equal(mods, strList(provenance["local_modifications"])) || !sameImport(target, staged) {
			return "", "", errf("conflict: library copy has local modifications")
		}
		if preview != nil {
			preview(s.Name, target)
		}
		return "reused", note, nil
	}
	if dry {
		if preview != nil {
			preview(s.Name, staged)
		}
		return "would_import", note, nil
	}
	// A direct import never replaces a populated destination.
	if err := os.Rename(staged, target); err != nil {
		return "", "", err
	}
	return "imported", note, nil
}

func ImportToLibrary(src ImportSource, selected []ImportSkill, dry, allowRisky bool, project string) ([]LibraryImport, []Result, error) {
	var plans []Result
	var preview func(name, source string)
	if dry && project != "" {
		if err := CheckProjectDirs(project); err != nil {
			return nil, nil, err
		}
		man, _, err := LoadProjectManifest(project)
		if err != nil {
			return nil, nil, err
		}
		preview = func(name, source string) {
			decision := decideInstall(project, source, Item{Name: name, Manual: true}, man.Skills[name], false, man.copyMode())
			plans = append(plans, res(name, decision.action != "blocked", "%s", decision.reason))
		}
	}
	if !dry {
		unlock, err := DirLock(SKILLS)
		if err != nil {
			return nil, nil, err
		}
		defer unlock()
	}
	out := make([]LibraryImport, 0, len(selected))
	refresh := false
	for _, s := range selected {
		info := LibraryImport{ImportSkill: s}
		action, detail, err := importOne(src, s, dry, allowRisky, preview, &info)
		if err != nil {
			action, detail = "failed", err.Error()
		}
		info.Action, info.Detail = action, detail
		out = append(out, info)
		refresh = refresh || action == "imported" || action == "reused"
	}
	if !dry && refresh {
		_, _, _, err := RebuildIndex(true)
		return out, plans, err
	}
	return out, plans, nil
}
