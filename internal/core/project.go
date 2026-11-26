package core

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// ResolveProject returns an absolute project dir (optionally the enclosing git root).
func ResolveProject(d string, gitRoot bool) (string, error) {
	if d == "" {
		d = "."
	}
	p := expand(d)
	if !IsDir(p) {
		return "", errf("target directory does not exist: %s", p)
	}
	p, _ = filepath.Abs(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	if gitRoot {
		r := Git([]string{"rev-parse", "--show-toplevel"}, p, 10)
		if r.Code == 0 && strings.TrimSpace(r.Stdout) != "" {
			p = strings.TrimSpace(r.Stdout)
		}
	}
	return p, nil
}

func CheckName(name string) error {
	if !KEBAB.MatchString(name) {
		return errf("invalid skill name: %s", pyRepr(name))
	}
	return nil
}

// CheckProjectDirs refuses to write through symlinked .agents/.claude dirs (a repo could point them anywhere).
func CheckProjectDirs(d string) error {
	for _, sub := range []string{".agents", ".agents/skills", ".claude", ".claude/skills"} {
		path := filepath.Join(d, sub)
		if IsSymlink(path) {
			return errf("%s is a symlink; refusing to write through it", path)
		}
	}
	return nil
}

func ProjectLock(d string) (func(), error) {
	if err := CheckProjectDirs(d); err != nil {
		return nil, err
	}
	return DirLock(filepath.Join(d, ".agents"))
}

func Dest(d, name string) string  { return filepath.Join(d, ".agents", "skills", name) }
func LinkP(d, name string) string { return filepath.Join(d, ".claude", "skills", name) }

func link(d, name string, copies bool) error {
	if copies {
		return createMirror(Dest(d, name), LinkP(d, name))
	}
	return setSkillLink(LinkP(d, name), "../../.agents/skills/"+name)
}

func setSkillLink(lp, target string) error {
	dir := filepath.Dir(lp)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if IsSymlink(lp) {
		if t, _ := os.Readlink(lp); filepath.ToSlash(t) == filepath.ToSlash(target) {
			return nil
		}
	}
	if Lexists(lp) && !IsSymlink(lp) {
		return errf("%s exists and is not a symlink; refusing to overwrite", lp)
	}
	if IsSymlink(lp) {
		// Create the new link beside the old one: Windows resolves a relative
		// target from the link's folder to choose a directory or file link.
		next := filepath.Join(dir, fmt.Sprintf(".%s.link-%d", filepath.Base(lp), time.Now().UnixNano()))
		if err := os.Symlink(target, next); err != nil {
			return err
		}
		if err := os.Rename(next, lp); err == nil {
			return nil
		}
		// Windows cannot rename over a directory link; swap it instead.
		old, _ := os.Readlink(lp)
		if err := os.Remove(lp); err != nil {
			os.Remove(next)
			return err
		}
		if err := os.Rename(next, lp); err != nil {
			os.Remove(next)
			return errors.Join(err, os.Symlink(old, lp))
		}
		return nil
	}
	return os.Symlink(target, lp)
}

func LinkOK(d, name string) bool {
	return linkOK(d, name, ReadManifest(d).copyMode(), TreeHash)
}

func linkOK(d, name string, copies bool, hash func(string) string) bool {
	lp, dest := LinkP(d, name), Dest(d, name)
	if !regularSkill(dest) {
		return false
	}
	if copies {
		return regularSkill(lp) && hash(lp) == hash(dest)
	}
	target, err := os.Readlink(lp)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(lp), target)
	}
	rel, err := filepath.Rel(dest, target)
	return err == nil && rel == "."
}

// checkPortableSkill refuses skills whose committed bytes would change on another
// checkout: symlinks (Git for Windows checks them out as plain files by default)
// and .gitattributes rules that override the project's "* -text".
func checkPortableSkill(name, dir string) error {
	var links, attrs []string
	for _, e := range Rglob(dir) {
		if hasIgnoredPart(e.Rel, HashIgnore) {
			continue
		}
		if e.IsSymlink() {
			links = append(links, e.Rel)
		} else if path.Base(e.Rel) == ".gitattributes" {
			if b, err := os.ReadFile(e.Path); err == nil && unsafeTextAttributes(b) {
				attrs = append(attrs, e.Rel)
			}
		}
	}
	switch {
	case len(links) > 0:
		return errf("%s contains symlinks (%s); replace them with regular files in %s so every checkout gets the same bytes",
			name, strings.Join(links, ", "), dir)
	case len(attrs) > 0:
		return errf("%s sets line-ending attributes in %s; remove the text/eol rules in %s so every checkout gets the same bytes",
			name, strings.Join(attrs, ", "), dir)
	}
	return nil
}

func regularSkill(dir string) bool {
	p := filepath.Join(dir, "SKILL.md")
	return !IsSymlink(dir) && !IsSymlink(p) && IsFile(p)
}

func inspectionHashes() func(string) string {
	hashes := map[string]string{}
	return func(path string) string {
		if h, ok := hashes[path]; ok {
			return h
		}
		h := TreeHash(path)
		hashes[path] = h
		return h
	}
}

func newEntry(src, pack string, prev *Entry) *Entry {
	s := LoadMap(filepath.Join(src, "SOURCE.json"))
	e := &Entry{}
	if prev != nil {
		cp := *prev
		e = &cp
		e.Packs = append([]string{}, prev.Packs...)
	}
	e.Source = SourceOf(src)
	e.UpstreamCommit = s["commit"]
	e.TreeHash = TreeHash(src)
	e.InstalledAt = Now()
	if pack != "" && !contains(e.Packs, pack) {
		e.Packs = append(e.Packs, pack)
	}
	if e.Packs == nil {
		e.Packs = []string{}
	}
	e.Manual = e.Manual || pack == ""
	return e
}

type InstallSummary struct {
	Installed int `json:"installed"`
	Present   int `json:"already_present"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// NeedsProjectCopy reports whether installing a skill adds its description to the project.
func NeedsProjectCopy(state string) bool { return state == "" || state == "missing" }

func (s InstallSummary) OK() bool {
	return s.Failed == 0 && s.Installed+s.Present > 0
}

func SummarizeInstall(results []Result) InstallSummary {
	var s InstallSummary
	for _, r := range results {
		switch {
		case !r.OK:
			s.Failed++
		case strings.HasPrefix(r.Message, "skipped:"):
			s.Skipped++
		case r.Message == "already up to date":
			s.Present++
		default:
			s.Installed++
		}
	}
	return s
}

// Install copies skills from the library into d, links them for Claude Code and records them.
func Install(d string, items []Item, force bool) ([]Result, error) {
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	var out []Result
	var changes []installChange
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		unlock()
		return nil, err
	}
	copies := man.copyMode()
	for _, it := range items {
		name := it.Name
		src := filepath.Join(SKILLS, name)
		decision := decideInstall(d, src, it, man.Skills[name], force, copies)
		if decision.action == "blocked" || decision.action == "skipped" {
			out = append(out, decision.result(name))
			continue
		}
		change, err := installFiles(d, name, src, decision.copyNeeded, copies, man.Skills[name], "replaced by --force")
		if err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		changes = append(changes, change)
		entry := newEntry(src, it.Pack, man.Skills[name])
		entry.Manual = entry.Manual || it.Manual
		for _, p := range it.OtherPacks {
			if !entry.HasPack(p) {
				entry.Packs = append(entry.Packs, p)
			}
		}
		man.Skills[name] = entry
		for _, p := range entry.Packs {
			if !man.Packs.Has(p) {
				man.Packs.Set(p, map[string]string{"installed_at": Now()})
			}
		}
		out = append(out, decision.result(name))
	}
	summary := SummarizeInstall(out)
	if summary.Installed+summary.Present == 0 {
		unlock()
		return out, nil
	}
	if copies {
		man.top.Set("claude_mode", "copy")
	}
	err = WriteManifest(d, man)
	if err != nil {
		for i := len(changes) - 1; i >= 0; i-- {
			err = errors.Join(err, changes[i].rollback())
		}
		unlock()
		return out, err
	}
	for _, c := range changes {
		c.finish()
	}
	unlock()
	RegisterProject(d)
	return out, nil
}

// Keep the old copy in trash on success. On failure restore both paths while
// the project lock is still held, including if the final manifest write fails.
type installChange struct {
	dest, link, previousLink, trash, mirrorBackup string
	mirror                                        bool
	copied, linked                                bool
}

func (c installChange) rollback() error {
	var linkErr error
	if c.mirror {
		linkErr = os.RemoveAll(c.link)
	}
	if c.mirrorBackup != "" {
		linkErr = errors.Join(linkErr, os.Rename(c.mirrorBackup, c.link))
		os.Remove(filepath.Dir(c.mirrorBackup))
	}
	if c.linked {
		if c.previousLink == "" {
			linkErr = os.Remove(c.link)
		} else {
			linkErr = setSkillLink(c.link, c.previousLink)
		}
	}
	if c.copied {
		if err := os.RemoveAll(c.dest); err != nil {
			return errors.Join(linkErr, err)
		}
	}
	if c.trash != "" {
		if err := Move(filepath.Join(c.trash, filepath.Base(c.dest)), c.dest); err != nil {
			return errors.Join(linkErr, err)
		}
		return errors.Join(linkErr, os.RemoveAll(c.trash))
	}
	return linkErr
}

func installFiles(d, name, src string, copyNeeded, copies bool, previous *Entry, reason string) (c installChange, err error) {
	c.dest, c.link = Dest(d, name), LinkP(d, name)
	hasMirror := copies && Lexists(c.link)
	if hasMirror && !replaceableMirror(d, name, previous) {
		return c, errf("modified or unmanaged Claude copy: %s", c.link)
	}
	if IsSymlink(c.link) {
		c.previousLink, err = os.Readlink(c.link)
		if err != nil {
			return c, err
		}
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, c.rollback())
		}
	}()
	if copyNeeded {
		parent := filepath.Dir(c.dest)
		if err = os.MkdirAll(parent, 0755); err != nil {
			return c, err
		}
		if err := checkPortableSkill(name, src); err != nil {
			return c, err
		}
		tmp, err := os.MkdirTemp(parent, ".install-")
		if err != nil {
			return c, err
		}
		defer os.RemoveAll(tmp)
		staged := filepath.Join(tmp, name)
		if err := CopyTree(src, staged, nil); err != nil {
			return c, err
		}
		if IsSymlink(filepath.Join(staged, "SKILL.md")) || !IsFile(filepath.Join(staged, "SKILL.md")) {
			return c, errf("%s: expected a regular SKILL.md", name)
		}
		if Lexists(c.dest) {
			c.trash, err = toTrashSkill(d, name, previous, reason)
			if err != nil {
				return c, err
			}
		}
		if err := os.Rename(staged, c.dest); err != nil {
			return c, err
		}
		c.copied = true
	}
	if copies {
		if hasMirror {
			if TreeHash(c.link) == TreeHash(c.dest) {
				return c, nil
			}
			if err = c.backupMirror(); err != nil {
				return c, err
			}
		}
		err = createMirror(c.dest, c.link)
		c.mirror = err == nil
		return c, err
	}
	if err := link(d, name, copies); err != nil {
		return c, err
	}
	c.linked = filepath.ToSlash(c.previousLink) != "../../.agents/skills/"+name
	if !linkOK(d, name, copies, TreeHash) {
		return c, errf("link does not resolve: %s", c.link)
	}
	return c, nil
}

func toTrashSkill(d, name string, entry *Entry, reason string) (string, error) {
	dest := Dest(d, name)
	slot, err := trashSlot("skill", name, d)
	if err != nil {
		return "", err
	}
	if err := Move(dest, filepath.Join(slot, name)); err != nil {
		return "", err
	}
	var em any
	if entry != nil {
		em = entry.AsMap()
	}
	writeTrashMeta(slot, map[string]any{"kind": "skill", "name": name, "project": d, "origin": dest,
		"removed_at": Now(), "reason": reason, "manifest_entry": em})
	return slot, nil
}

// Uninstall moves project copies to the trash and drops them from the manifest.
func Uninstall(d string, names []string) ([]Result, error) {
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	defer unlock()
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, err
	}
	return uninstallLocked(d, names, man)
}

func uninstallLocked(d string, names []string, man *ManifestData) ([]Result, error) {
	var out []Result
	var changes []installChange
	for _, name := range names {
		if !KEBAB.MatchString(name) {
			out = append(out, res(name, false, "invalid skill name"))
			continue
		}
		dest, lp := Dest(d, name), LinkP(d, name)
		mirror := man.copyMode() && Lexists(lp)
		if mirror && !replaceableMirror(d, name, man.Skills[name]) {
			out = append(out, res(name, false, "modified or unmanaged Claude copy: %s", lp))
			continue
		}
		if IsSymlink(dest) {
			out = append(out, res(name, false, "%s is a symlink; refusing", dest))
			continue
		}
		c := installChange{dest: dest, link: lp}
		hadView := mirror || IsSymlink(lp)
		if hadView {
			if err := c.backupMirror(); err != nil {
				out = append(out, res(name, false, "%v", err))
				continue
			}
		}
		if Lexists(dest) {
			var err error
			c.trash, err = toTrashSkill(d, name, man.Skills[name], "uninstall")
			if err != nil {
				err = errors.Join(err, c.rollback())
				out = append(out, res(name, false, "%v", err))
				continue
			}
			out = append(out, res(name, true, "moved to trash: %s", c.trash))
		} else if man.Skills[name] != nil || hadView {
			out = append(out, res(name, true, "removed stale manifest entry/link"))
		} else {
			out = append(out, res(name, false, "not installed"))
			continue
		}
		delete(man.Skills, name)
		changes = append(changes, c)
	}
	for _, p := range man.PackNames() {
		used := false
		for _, e := range man.Skills {
			if e.HasPack(p) {
				used = true
				break
			}
		}
		if !used {
			man.Packs.Delete(p)
		}
	}
	if err := WriteManifest(d, man); err != nil {
		for i := len(changes) - 1; i >= 0; i-- {
			err = errors.Join(err, changes[i].rollback())
		}
		return out, err
	}
	for _, c := range changes {
		c.finish()
	}
	return out, nil
}

// UninstallPack drops a pack; only skills no other pack (or manual add) still needs are removed.
func UninstallPack(d, pack string) ([]Result, error) {
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	defer unlock()
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, err
	}
	used := man.Packs.Has(pack)
	for _, e := range man.Skills {
		used = used || e.HasPack(pack)
	}
	if !used {
		return []Result{res(pack, false, "pack not installed in this project")}, nil
	}
	var victims []string
	var kept []Result
	for _, name := range man.SkillNames() {
		e := man.Skills[name]
		if !e.HasPack(pack) {
			continue
		}
		var ps []string
		for _, p := range e.Packs {
			if p != pack {
				ps = append(ps, p)
			}
		}
		if ps == nil {
			ps = []string{}
		}
		e.Packs = ps
		if len(e.Packs) == 0 && !e.Manual {
			e.Packs = []string{pack} // retain ownership until moving this copy succeeds
			victims = append(victims, name)
		} else {
			why := strings.Join(e.Packs, ", ")
			if why == "" {
				why = "manual add"
			}
			kept = append(kept, res(name, true, "kept (still used by %s)", why))
		}
	}
	r, err := uninstallLocked(d, victims, man)
	return append(r, kept...), err
}

// SkillStatus is one row of project_status.
type SkillStatus struct {
	Name          string   `json:"name"`
	Packs         []string `json:"packs"`
	Pinned        bool     `json:"pinned"`
	Manual        bool     `json:"manual"`
	Tracked       bool     `json:"tracked"`
	InLibrary     bool     `json:"in_library"`
	LinkOK        bool     `json:"link_ok"`
	State         string   `json:"state"`
	SameAsLibrary *bool    `json:"same_as_library,omitempty"`
	// MatchesManifest compares the project copy with the committed manifest,
	// which is what verify and teammates share; Library compares it with this
	// machine's library: "same", "differs" or "absent". Both are set for
	// tracked skills with a project copy.
	MatchesManifest *bool  `json:"matches_manifest,omitempty"`
	Library         string `json:"library,omitempty"`
}

// ProjectStatus: ok | outdated | modified | conflict | missing | orphan | untracked | invalid per skill.
func ProjectStatus(d string) []SkillStatus {
	st, _, _ := InspectProject(d)
	return st
}

func InspectProject(d string) ([]SkillStatus, *ManifestData, error) {
	if err := CheckProjectDirs(d); err != nil {
		return nil, nil, err
	}
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, nil, err
	}
	return projectStatus(d, man), man, nil
}

func projectStatus(d string, man *ManifestData) []SkillStatus {
	names := map[string]bool{}
	for n := range man.Skills {
		names[n] = true
	}
	for _, e := range SortedDir(filepath.Join(d, ".agents", "skills")) {
		if !strings.HasPrefix(e.Name(), ".") {
			names[e.Name()] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	out := make([]SkillStatus, 0, len(sorted))
	copies, hash := man.copyMode(), inspectionHashes()
	for _, name := range sorted {
		e := man.Skills[name]
		dest, src := Dest(d, name), filepath.Join(SKILLS, name)
		r := SkillStatus{Name: name, Packs: []string{}, Tracked: e != nil, InLibrary: IsFile(filepath.Join(src, "SKILL.md"))}
		if e != nil {
			r.Packs, r.Pinned, r.Manual = append([]string{}, e.Packs...), e.Pinned, e.Manual
		}
		if Exists(dest) {
			r.LinkOK = linkOK(d, name, copies, hash)
		}
		switch {
		case IsSymlink(dest) || (Exists(dest) && !regularSkill(dest)):
			r.State = "invalid"
		case !Exists(dest):
			r.State = "missing"
		case e == nil:
			r.State = "untracked"
			if r.InLibrary {
				same := hash(dest) == hash(src)
				r.SameAsLibrary = &same
			}
		default:
			cur, rec := hash(dest), e.TreeHash
			var lib string
			if r.InLibrary {
				lib = hash(src)
			}
			matches := cur == rec
			r.MatchesManifest = &matches
			switch {
			case !r.InLibrary:
				r.Library = "absent"
			case cur == lib:
				r.Library = "same"
			default:
				r.Library = "differs"
			}
			switch {
			case !r.InLibrary:
				if cur == rec {
					r.State = "orphan"
				} else {
					r.State = "modified"
				}
			case cur == lib:
				r.State = "ok"
			case cur != rec && lib != rec:
				r.State = "conflict"
			case cur != rec:
				r.State = "modified"
			default:
				r.State = "outdated"
			}
		}
		out = append(out, r)
	}
	return out
}

func StatusMap(st []SkillStatus) map[string]SkillStatus {
	m := make(map[string]SkillStatus, len(st))
	for _, r := range st {
		m[r.Name] = r
	}
	return m
}

func DanglingLinks(d string) []string {
	ld := filepath.Join(d, ".claude", "skills")
	var out []string
	for _, e := range SortedDir(ld) {
		p := filepath.Join(ld, e.Name())
		if IsSymlink(p) && !Exists(p) {
			if t, _ := os.Readlink(p); strings.HasPrefix(filepath.ToSlash(t), "../../.agents/skills/") {
				out = append(out, p)
			}
		}
	}
	return out
}

func RepairLinks(d string) ([]string, error) {
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	defer unlock()
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, err
	}
	copies := man.copyMode()
	var msgs []string
	var problems []error
	if !copies {
		for _, p := range DanglingLinks(d) {
			if err := os.Remove(p); err != nil {
				problems = append(problems, err)
				continue
			}
			msgs = append(msgs, "removed dangling link "+filepath.Base(p))
		}
	}
	sk := filepath.Join(d, ".agents", "skills")
	for _, e := range SortedDir(sk) {
		name := e.Name()
		if !KEBAB.MatchString(name) || !regularSkill(Dest(d, name)) || linkOK(d, name, copies, TreeHash) {
			continue
		}
		prev := man.Skills[name]
		if copies && prev != nil && Lexists(LinkP(d, name)) && !replaceableMirror(d, name, prev) {
			problems = append(problems, errf("%s: %s", name, mirrorProblem(d, name, prev, TreeHash)))
			continue
		}
		change, err := installFiles(d, name, "", false, copies, prev, "")
		if err != nil {
			problems = append(problems, errf("%s: %v", name, err))
			continue
		}
		change.finish()
		if copies {
			msgs = append(msgs, "refreshed Claude copy "+name)
		} else {
			msgs = append(msgs, "linked "+name)
		}
	}
	return msgs, errors.Join(problems...)
}

// UpdateProject refreshes outdated skills (pinned skipped; modified/conflict need force).
func UpdateProject(d string, names []string, force bool) ([]Result, error) {
	return updateProject(d, names, force, false)
}

// PreviewProjectUpdate reports which copies can be refreshed without writing to disk.
func PreviewProjectUpdate(d string, names []string, force bool) ([]Result, error) {
	return updateProject(d, names, force, true)
}

func updateProject(d string, names []string, force, dry bool) ([]Result, error) {
	for _, name := range names {
		if err := CheckName(name); err != nil {
			return nil, err
		}
	}
	if dry {
		if err := CheckProjectDirs(d); err != nil {
			return nil, err
		}
	} else {
		unlock, err := ProjectLock(d)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, err
	}
	rows := projectStatus(d, man)
	st := StatusMap(rows)
	targets := dedup(names)
	if len(targets) == 0 {
		for _, r := range rows {
			if r.State == "outdated" {
				if err := CheckName(r.Name); err != nil {
					return nil, err
				}
				targets = append(targets, r.Name)
			}
		}
	}
	var out []Result
	var changes []installChange
	copies := man.copyMode()
	for _, name := range targets {
		r, ok := st[name]
		switch {
		case !ok:
			out = append(out, res(name, false, "not installed"))
			continue
		case r.State == "ok":
			out = append(out, res(name, true, "already up to date"))
			continue
		case r.Pinned && !force:
			out = append(out, res(name, false, "pinned (unpin or --force)"))
			continue
		case r.State == "missing" && (!force || !r.InLibrary):
			msg := "project copy is missing and unavailable in the library"
			if r.InLibrary {
				msg = "project copy is missing; run skillman add or update --force to restore it"
			}
			out = append(out, res(name, false, "%s", msg))
			continue
		case (r.State == "modified" || r.State == "conflict") && !force:
			out = append(out, res(name, false, "%s locally (review diff; --force to replace)", r.State))
			continue
		case !(r.State == "outdated" || r.State == "modified" || r.State == "conflict" || r.State == "missing") || !r.InLibrary:
			out = append(out, res(name, false, "cannot update from state %s", r.State))
			continue
		}
		src := filepath.Join(SKILLS, name)
		prev := man.Skills[name]
		verb := "update"
		if r.State == "missing" {
			verb = "restore missing copy"
		}
		if dry {
			switch {
			case IsSymlink(filepath.Join(src, "SKILL.md")):
				out = append(out, res(name, false, "%s: expected a regular SKILL.md", name))
			default:
				err := checkPortableSkill(name, src)
				if err == nil {
					err = checkInstallTargets(d, name, prev, copies)
				}
				if err != nil {
					out = append(out, res(name, false, "%v", err))
				} else {
					out = append(out, res(name, true, "would %s", verb))
				}
			}
			continue
		}
		change, err := installFiles(d, name, src, true, copies, prev, "update")
		if err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		changes = append(changes, change)
		e := newEntry(src, "", prev)
		e.Manual = prev != nil && prev.Manual
		man.Skills[name] = e
		message := "updated"
		if r.State == "missing" {
			message = "restored missing copy"
		}
		out = append(out, res(name, true, "%s", message))
	}
	if dry || len(changes) == 0 {
		return out, nil
	}
	if err := WriteManifest(d, man); err != nil {
		for i := len(changes) - 1; i >= 0; i-- {
			err = errors.Join(err, changes[i].rollback())
		}
		return out, err
	}
	for _, c := range changes {
		c.finish()
	}
	return out, nil
}

// Adopt starts tracking copies that predate the manifest.
func Adopt(d string, names []string) ([]Result, error) {
	for _, n := range names {
		if err := CheckName(n); err != nil {
			return nil, err
		}
	}
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	rows, man, err := InspectProject(d)
	if err != nil {
		unlock()
		return nil, err
	}
	st := StatusMap(rows)
	targets := dedup(names)
	if len(targets) == 0 {
		for _, r := range rows {
			if r.State == "untracked" {
				targets = append(targets, r.Name)
			}
		}
	}
	for _, n := range targets {
		if err := CheckName(n); err != nil {
			unlock()
			return nil, err
		}
	}
	copies := man.copyMode()
	var out []Result
	var changes []installChange
	adopted := false
	for _, name := range targets {
		r, ok := st[name]
		if ok && !r.Tracked && r.State == "invalid" {
			out = append(out, res(name, false, "invalid project skill: expected a real folder with a regular SKILL.md"))
			continue
		}
		if !ok || r.State != "untracked" {
			out = append(out, res(name, false, "not untracked"))
			continue
		}
		if !r.InLibrary {
			out = append(out, res(name, false, "not in library (project-local skill; left as is)"))
			continue
		}
		if err := checkPortableSkill(name, Dest(d, name)); err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		e := newEntry(filepath.Join(SKILLS, name), "", nil)
		same := r.SameAsLibrary != nil && *r.SameAsLibrary
		if !same {
			e.TreeHash = TreeHash(Dest(d, name)) // current copy is the baseline -> outdated
		}
		if !linkOK(d, name, copies, TreeHash) {
			change, err := installFiles(d, name, "", false, copies, nil, "")
			if err != nil {
				out = append(out, res(name, false, "%v", err))
				continue
			}
			changes = append(changes, change)
		}
		man.Skills[name] = e
		adopted = true
		msg := "adopted"
		if !same {
			msg += " (differs from library → outdated)"
		}
		out = append(out, res(name, true, "%s", msg))
	}
	if !adopted {
		unlock()
		return out, nil
	}
	if err := WriteManifest(d, man); err != nil {
		for i := len(changes) - 1; i >= 0; i-- {
			err = errors.Join(err, changes[i].rollback())
		}
		unlock()
		return out, err
	}
	for _, c := range changes {
		c.finish()
	}
	unlock()
	RegisterProject(d)
	return out, nil
}

func SetPin(d string, names []string, pinned bool) ([]Result, error) {
	for _, n := range names {
		if err := CheckName(n); err != nil {
			return nil, err
		}
	}
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	defer unlock()
	man, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, err
	}
	var out []Result
	for _, n := range names {
		if e, ok := man.Skills[n]; ok {
			e.Pinned = pinned
			out = append(out, res(n, true, "%s", map[bool]string{true: "pinned", false: "unpinned"}[pinned]))
		} else {
			out = append(out, res(n, false, "not tracked"))
		}
	}
	return out, WriteManifest(d, man)
}

// ── tokens / doctor ──

type Budget struct {
	ProjectSkills int `json:"project_skills"`
	ProjectTokens int `json:"project_tokens"`
	GlobalSkills  int `json:"global_skills"`
	GlobalTokens  int `json:"global_tokens"`
	Total         int `json:"total"`
	Budget        int `json:"budget"`
}

func globSkillMDs(dir string) []string {
	var out []string
	for _, e := range SortedDir(dir) {
		p := filepath.Join(dir, e.Name(), "SKILL.md")
		if Exists(p) {
			out = append(out, p)
		}
	}
	return out
}

func TokenBudgetOf(d string) Budget {
	var b Budget
	for _, p := range globSkillMDs(filepath.Join(d, ".agents", "skills")) {
		b.ProjectSkills++
		b.ProjectTokens += SkillTokens(filepath.Dir(p))
	}
	seen := map[string]bool{}
	for _, gd := range GlobalSkillDirs {
		for _, p := range globSkillMDs(gd) {
			rp, err := filepath.EvalSymlinks(p)
			if err != nil {
				rp = p
			}
			if !seen[rp] {
				seen[rp] = true
				b.GlobalTokens += SkillTokens(filepath.Dir(p))
			}
		}
	}
	b.GlobalSkills = len(seen)
	b.Total = b.ProjectTokens + b.GlobalTokens
	b.Budget, _ = DescriptionBudget()
	return b
}

type Shadow struct{ Name, Dir string }

func Shadowing(d string) []Shadow {
	var out []Shadow
	sk := filepath.Join(d, ".agents", "skills")
	for _, e := range SortedDir(sk) {
		if e.Name() == SelfSkill {
			continue
		}
		p := filepath.Join(sk, e.Name())
		rp, _ := filepath.EvalSymlinks(p)
		for _, gd := range GlobalSkillDirs {
			g := filepath.Join(gd, e.Name())
			if Exists(filepath.Join(g, "SKILL.md")) {
				rg, _ := filepath.EvalSymlinks(g)
				if rg != rp {
					out = append(out, Shadow{e.Name(), gd})
				}
			}
		}
	}
	return out
}

// Diagnostic is a (level, message) pair; level is ok, warn or error.
type Diagnostic struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

func Doctor(d string) []Diagnostic {
	rows, man, err := InspectProject(d)
	if err != nil {
		return []Diagnostic{{"error", err.Error()}}
	}
	var out []Diagnostic
	add := func(lv, f string, a ...any) { out = append(out, Diagnostic{lv, sprintf(f, a...)}) }
	copies := man.copyMode()
	for _, r := range rows {
		if copies && r.Tracked && !r.LinkOK && r.State != "missing" && r.State != "invalid" {
			add("warn", "%s: %s", r.Name, mirrorProblem(d, r.Name, man.Skills[r.Name], TreeHash))
		}
		switch s := r.State; {
		case s == "ok" && !r.LinkOK && !copies:
			add("warn", "%s: .claude/skills link missing/broken (fix: repair-links)", r.Name)
		case s == "invalid" && !r.Tracked:
			add("error", "%s: invalid project skill (expected a real folder with a regular SKILL.md)", r.Name)
		case s == "missing" || s == "invalid":
			add("error", "%s: %s (manifest says installed)", r.Name, s)
		case s == "outdated" || s == "modified" || s == "conflict" || s == "orphan" || s == "untracked":
			add("warn", "%s: %s", r.Name, s)
		}
	}
	for _, p := range DanglingLinks(d) {
		add("warn", "dangling link .claude/skills/%s", filepath.Base(p))
	}
	var installed []string
	for _, r := range rows {
		if r.State != "missing" && r.State != "invalid" {
			installed = append(installed, r.Name)
		}
	}
	for name, sims := range SimilarSkills(LoadIndex(), installed) {
		for _, s := range sims {
			if name < s.Name && slices.Contains(installed, s.Name) {
				a, b := SkillTokens(Dest(d, name)), SkillTokens(Dest(d, s.Name))
				add("warn", "%s and %s cover the same topic (%s): together ≈ %d description tokens; consider removing one", name, s.Name, s.Reason, a+b)
			}
		}
	}
	for _, s := range Shadowing(d) {
		add("warn", "%s: also installed globally in %s (name shadowing)", s.Name, s.Dir)
	}
	tb := TokenBudgetOf(d)
	lvl := "ok"
	if tb.Total > tb.Budget {
		lvl = "warn"
	}
	add(lvl, "skill descriptions ≈ %d tokens (%d project ≈ %d, %d global ≈ %d) / budget %d",
		tb.Total, tb.ProjectSkills, tb.ProjectTokens, tb.GlobalSkills, tb.GlobalTokens, tb.Budget)
	for _, p := range man.PackNames() {
		skills, err := ExpandPack(p)
		if err != nil {
			add("warn", "pack %s: %v", p, err)
			continue
		}
		var missing []string
		for _, s := range skills {
			if _, ok := man.Skills[s]; !ok {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 {
			show := missing
			if len(show) > 8 {
				show = show[:8]
			}
			add("warn", "pack %s: %d skill(s) not installed: %s", p, len(missing), strings.Join(show, ", "))
		}
	}
	bad := false
	for _, x := range out {
		bad = bad || x.Level != "ok"
	}
	if !bad {
		out = append([]Diagnostic{{"ok", "no problems found"}}, out...)
	}
	return out
}

// Verify is the frozen CI check: committed copies must match the manifest hashes. Needs no library.
func Verify(d string) []Result {
	if err := CheckProjectDirs(d); err != nil {
		return []Result{res(Manifest, false, "%v", err)}
	}
	man, present, err := LoadProjectManifest(d)
	if err != nil {
		return []Result{res(Manifest, false, "%v", err)}
	}
	if !present {
		return []Result{res(Manifest, false, "no .agents/skill-manifest.json in %s", d)}
	}
	copies, hash := man.copyMode(), inspectionHashes()
	names := map[string]bool{}
	for n := range man.Skills {
		names[n] = true
	}
	sk := filepath.Join(d, ".agents", "skills")
	for _, e := range SortedDir(sk) {
		p := filepath.Join(sk, e.Name())
		if (IsDir(p) || IsSymlink(p)) && !strings.HasPrefix(e.Name(), ".") {
			names[e.Name()] = true
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	var out []Result
	for _, name := range sorted {
		e, dest := man.Skills[name], Dest(d, name)
		switch {
		case e == nil:
			out = append(out, res(name, false, "untracked: on disk but not in the manifest (run: skillman adopt)"))
		case !regularSkill(dest):
			out = append(out, res(name, false, "missing, or not a real directory"))
		case !linkOK(d, name, copies, hash):
			msg := "broken .claude/skills link (run: skillman repair-links)"
			if copies {
				msg = mirrorProblem(d, name, e, hash)
			} else if gitLinkPlaceholder(d, LinkP(d, name), "../../.agents/skills/"+name) {
				msg = "Git checked out the .claude/skills link as a plain file (symlinks disabled); run: skillman repair-links -C <dir> --mode copy"
			}
			out = append(out, res(name, false, "%s", msg))
		case hash(dest) != e.TreeHash:
			if e.Pinned {
				out = append(out, res(name, true, "pinned, edited locally"))
			} else if _, converted := checkoutConverted(d, dest, e.TreeHash); converted {
				out = append(out, res(name, false, "Git converted line endings on checkout; run: skillman repair-links -C <dir> --mode copy to restore the committed bytes"))
			} else {
				out = append(out, res(name, false, "content differs from the manifest hash (edited? pin it, or run update)"))
			}
		default:
			out = append(out, res(name, true, "ok"))
		}
	}
	return out
}

// ProjectsAffected lists registered projects with outdated skills (optionally restricted to names).
type Affected struct {
	Project string   `json:"project"`
	Skills  []string `json:"skills"`
}

func ProjectsAffected(names []string) []Affected {
	out := []Affected{}
	for _, p := range RegisteredProjects() {
		var st []string
		for _, r := range ProjectStatus(p) {
			if r.State == "outdated" && !r.Pinned && (len(names) == 0 || contains(names, r.Name)) {
				st = append(st, r.Name)
			}
		}
		if len(st) > 0 {
			out = append(out, Affected{p, st})
		}
	}
	return out
}
