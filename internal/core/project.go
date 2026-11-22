package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
		if IsSymlink(filepath.Join(d, sub)) {
			return errf("%s is a symlink; refusing to write through it", filepath.Join(d, sub))
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

func link(d, name string) error {
	return setSkillLink(LinkP(d, name), "../../.agents/skills/"+name)
}

func setSkillLink(lp, target string) error {
	dir := filepath.Dir(lp)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if IsSymlink(lp) {
		if t, _ := os.Readlink(lp); t == target {
			return nil
		}
	}
	if Lexists(lp) && !IsSymlink(lp) {
		return errf("%s exists and is not a symlink; refusing to overwrite", lp)
	}
	if IsSymlink(lp) {
		tmp, err := os.MkdirTemp(dir, ".link-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		next := filepath.Join(tmp, "link")
		if err := os.Symlink(target, next); err != nil {
			return err
		}
		return os.Rename(next, lp)
	}
	return os.Symlink(target, lp)
}

func LinkOK(d, name string) bool {
	lp := LinkP(d, name)
	return IsSymlink(lp) && IsFile(filepath.Join(lp, "SKILL.md")) && !IsSymlink(filepath.Join(Dest(d, name), "SKILL.md"))
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
	man := ReadManifest(d)
	for _, it := range items {
		name := it.Name
		if !KEBAB.MatchString(name) {
			out = append(out, res(name, false, "invalid skill name"))
			continue
		}
		src := filepath.Join(SKILLS, name)
		if !IsFile(filepath.Join(src, "SKILL.md")) {
			if it.Pack != "" && !it.Manual {
				out = append(out, res(name, true, "skipped: not in the library yet (listed by @%s; import it: skillman upstream tick --pack %s)", it.Pack, it.Pack))
				continue
			}
			out = append(out, res(name, false, "not in library (try: skillman search %s)", strings.Split(name, "-")[0]))
			continue
		}
		dest, lp := Dest(d, name), LinkP(d, name)
		if Lexists(lp) && !IsSymlink(lp) {
			out = append(out, res(name, false, "%s exists and is not a symlink; refusing to overwrite", lp))
			continue
		}
		if IsSymlink(dest) {
			out = append(out, res(name, false, "%s is a symlink; refusing", dest))
			continue
		}
		var state string
		if Exists(dest) {
			prev := man.Skills[name]
			switch {
			case prev == nil && !force:
				out = append(out, res(name, false, "untracked project copy: inspect with skillman diff, then use skillman adopt"))
				continue
			case TreeHash(src) == TreeHash(dest):
				state = "already up to date"
			case force:
				state = "replaced (--force)"
			case prev != nil && prev.TreeHash == TreeHash(dest):
				out = append(out, res(name, false, "outdated (library is newer) — run: skillman update -C <dir> %s", name))
				continue
			default:
				out = append(out, res(name, false, "%s exists and differs from library (review with: skillman diff -C <dir> %s; or use --force)", dest, name))
				continue
			}
		} else {
			state = "copied"
		}
		change, err := installFiles(d, name, src, state != "already up to date", man.Skills[name])
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
		out = append(out, res(name, true, "%s", state))
	}
	summary := SummarizeInstall(out)
	if summary.Installed+summary.Present == 0 {
		unlock()
		return out, nil
	}
	err = WriteManifest(d, man)
	if err != nil {
		for i := len(changes) - 1; i >= 0; i-- {
			err = errors.Join(err, changes[i].rollback())
		}
		unlock()
		return out, err
	}
	unlock()
	RegisterProject(d)
	return out, nil
}

// Keep the old copy in trash on success. On failure restore both paths while
// the project lock is still held, including if the final manifest write fails.
type installChange struct {
	dest, link, previousLink, trash string
	copied, linked                  bool
}

func (c installChange) rollback() error {
	var linkErr error
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

func installFiles(d, name, src string, copyNeeded bool, previous *Entry) (c installChange, err error) {
	c.dest, c.link = Dest(d, name), LinkP(d, name)
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
			c.trash, err = toTrashSkill(d, name, previous, "replaced by --force")
			if err != nil {
				return c, err
			}
		}
		if err := os.Rename(staged, c.dest); err != nil {
			return c, err
		}
		c.copied = true
	}
	if err := link(d, name); err != nil {
		return c, err
	}
	c.linked = c.previousLink != "../../.agents/skills/"+name
	if !LinkOK(d, name) {
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
	return uninstallLocked(d, names, ReadManifest(d))
}

func uninstallLocked(d string, names []string, man *ManifestData) ([]Result, error) {
	var out []Result
	for _, name := range names {
		if !KEBAB.MatchString(name) {
			out = append(out, res(name, false, "invalid skill name"))
			continue
		}
		dest, lp := Dest(d, name), LinkP(d, name)
		if !Lexists(dest) {
			hadLink := IsSymlink(lp)
			if hadLink {
				os.Remove(lp)
			}
			_, tracked := man.Skills[name]
			delete(man.Skills, name)
			if tracked || hadLink {
				out = append(out, res(name, true, "removed stale manifest entry/link"))
			} else {
				out = append(out, res(name, false, "not installed"))
			}
			continue
		}
		if IsSymlink(dest) {
			out = append(out, res(name, false, "%s is a symlink; refusing", dest))
			continue
		}
		slot, err := toTrashSkill(d, name, man.Skills[name], "uninstall")
		if err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		if IsSymlink(lp) {
			os.Remove(lp)
		}
		delete(man.Skills, name)
		out = append(out, res(name, true, "moved to trash: %s", slot))
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
	return out, WriteManifest(d, man)
}

// UninstallPack drops a pack; only skills no other pack (or manual add) still needs are removed.
func UninstallPack(d, pack string) ([]Result, error) {
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	defer unlock()
	man := ReadManifest(d)
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
}

// ProjectStatus: ok | outdated | modified | conflict | missing | orphan | untracked | invalid per skill.
func ProjectStatus(d string) []SkillStatus {
	man := ReadManifest(d)
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
	for _, name := range sorted {
		e := man.Skills[name]
		dest, src := Dest(d, name), filepath.Join(SKILLS, name)
		r := SkillStatus{Name: name, Packs: []string{}, Tracked: e != nil, InLibrary: IsFile(filepath.Join(src, "SKILL.md"))}
		if e != nil {
			r.Packs, r.Pinned, r.Manual = append([]string{}, e.Packs...), e.Pinned, e.Manual
		}
		if Exists(dest) {
			r.LinkOK = LinkOK(d, name)
		}
		switch {
		case IsSymlink(dest) || (Exists(dest) && !IsDir(dest)):
			r.State = "invalid"
		case !Exists(dest):
			r.State = "missing"
		case e == nil:
			r.State = "untracked"
			if r.InLibrary {
				same := TreeHash(dest) == TreeHash(src)
				r.SameAsLibrary = &same
			}
		default:
			cur, rec := TreeHash(dest), e.TreeHash
			var lib string
			if r.InLibrary {
				lib = TreeHash(src)
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
			if t, _ := os.Readlink(p); strings.HasPrefix(t, "../../.agents/skills/") {
				out = append(out, p)
			}
		}
	}
	return out
}

func RepairLinks(d string) ([]string, error) {
	if err := CheckProjectDirs(d); err != nil {
		return nil, err
	}
	var msgs []string
	for _, p := range DanglingLinks(d) {
		os.Remove(p)
		msgs = append(msgs, "removed dangling link "+filepath.Base(p))
	}
	sk := filepath.Join(d, ".agents", "skills")
	for _, e := range SortedDir(sk) {
		p := filepath.Join(sk, e.Name())
		if IsDir(p) && !IsSymlink(p) && !strings.HasPrefix(e.Name(), ".") && !LinkOK(d, e.Name()) {
			lp := LinkP(d, e.Name())
			if Lexists(lp) && !IsSymlink(lp) {
				msgs = append(msgs, fmt.Sprintf("skip %s: %s is a real directory", e.Name(), lp))
				continue
			}
			if err := link(d, e.Name()); err != nil {
				msgs = append(msgs, err.Error())
				continue
			}
			msgs = append(msgs, "linked "+e.Name())
		}
	}
	return msgs, nil
}

// UpdateProject refreshes outdated skills (pinned skipped; modified/conflict need force).
func UpdateProject(d string, names []string, force bool) ([]Result, error) {
	rows := ProjectStatus(d)
	st := StatusMap(rows)
	targets := names
	if len(targets) == 0 {
		for _, r := range rows {
			if r.State == "outdated" {
				targets = append(targets, r.Name)
			}
		}
	}
	for _, n := range targets {
		if err := CheckName(n); err != nil {
			return nil, err
		}
	}
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	defer unlock()
	man := ReadManifest(d)
	var out []Result
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
		case (r.State == "modified" || r.State == "conflict") && !force:
			out = append(out, res(name, false, "%s locally (review diff; --force to replace)", r.State))
			continue
		case !(r.State == "outdated" || r.State == "modified" || r.State == "conflict") || !r.InLibrary:
			out = append(out, res(name, false, "cannot update from state %s", r.State))
			continue
		}
		src := filepath.Join(SKILLS, name)
		if _, err := toTrashSkill(d, name, man.Skills[name], "update"); err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		if err := CopyTree(src, Dest(d, name), nil); err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		link(d, name)
		prev := man.Skills[name]
		e := newEntry(src, "", prev)
		e.Manual = prev != nil && prev.Manual
		man.Skills[name] = e
		out = append(out, res(name, true, "updated"))
	}
	return out, WriteManifest(d, man)
}

// Adopt starts tracking copies that predate the manifest.
func Adopt(d string, names []string) ([]Result, error) {
	rows := ProjectStatus(d)
	st := StatusMap(rows)
	targets := names
	if len(targets) == 0 {
		for _, r := range rows {
			if r.State == "untracked" {
				targets = append(targets, r.Name)
			}
		}
	}
	for _, n := range targets {
		if err := CheckName(n); err != nil {
			return nil, err
		}
	}
	unlock, err := ProjectLock(d)
	if err != nil {
		return nil, err
	}
	man := ReadManifest(d)
	var out []Result
	for _, name := range targets {
		r, ok := st[name]
		if !ok || r.State != "untracked" {
			out = append(out, res(name, false, "not untracked"))
			continue
		}
		if !r.InLibrary {
			out = append(out, res(name, false, "not in library (project-local skill; left as is)"))
			continue
		}
		e := newEntry(filepath.Join(SKILLS, name), "", nil)
		same := r.SameAsLibrary != nil && *r.SameAsLibrary
		if !same {
			e.TreeHash = TreeHash(Dest(d, name)) // current copy is the baseline -> outdated
		}
		if err := link(d, name); err != nil {
			out = append(out, res(name, false, "%v", err))
			continue
		}
		man.Skills[name] = e
		msg := "adopted"
		if !same {
			msg += " (differs from library → outdated)"
		}
		out = append(out, res(name, true, "%s", msg))
	}
	err = WriteManifest(d, man)
	unlock()
	RegisterProject(d)
	return out, err
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
	man := ReadManifest(d)
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
	b.Budget = TokenBudget
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
	var out []Diagnostic
	add := func(lv, f string, a ...any) { out = append(out, Diagnostic{lv, sprintf(f, a...)}) }
	for _, r := range ProjectStatus(d) {
		switch s := r.State; {
		case s == "ok" && !r.LinkOK:
			add("warn", "%s: .claude/skills link missing/broken (fix: repair-links)", r.Name)
		case s == "missing" || s == "invalid":
			add("error", "%s: %s (manifest says installed)", r.Name, s)
		case s == "outdated" || s == "modified" || s == "conflict" || s == "orphan" || s == "untracked":
			add("warn", "%s: %s", r.Name, s)
		}
	}
	for _, p := range DanglingLinks(d) {
		add("warn", "dangling link .claude/skills/%s", filepath.Base(p))
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
	man := ReadManifest(d)
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
	man := ReadManifest(d)
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
		case IsSymlink(dest) || !IsFile(filepath.Join(dest, "SKILL.md")):
			out = append(out, res(name, false, "missing, or not a real directory"))
		case !LinkOK(d, name):
			out = append(out, res(name, false, "broken .claude/skills link (run: skillman repair-links)"))
		case TreeHash(dest) != e.TreeHash:
			if e.Pinned {
				out = append(out, res(name, true, "pinned, edited locally"))
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
	var out []Affected
	for _, p := range RegisteredProjects() {
		var st []string
		for _, r := range ProjectStatus(p) {
			if r.State == "outdated" && (len(names) == 0 || contains(names, r.Name)) {
				st = append(st, r.Name)
			}
		}
		if len(st) > 0 {
			out = append(out, Affected{p, st})
		}
	}
	return out
}
