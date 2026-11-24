package core

import (
	"errors"
	"os"
	"path/filepath"
)

func managedMirror(p string, e *Entry) bool {
	return e != nil && regularSkill(p) && TreeHash(p) == e.TreeHash
}

// A tracked copy can be replaced if it matches either the recorded version
// or the current project directory, whose contents are preserved separately.
func replaceableMirror(project, name string, entry *Entry) bool {
	return entry != nil && (managedMirror(LinkP(project, name), entry) || linkOK(project, name, true, inspectionHashes()))
}

func mirrorProblem(project, name string, entry *Entry, hash func(string) string) string {
	mirror := LinkP(project, name)
	switch {
	case !Lexists(mirror):
		return "missing Claude copy (run: skillman repair-links)"
	case !regularSkill(mirror):
		return sprintf("Claude copy path is occupied: %s", mirror)
	case hash(mirror) == entry.TreeHash:
		return "Claude copy differs from the project (run: skillman repair-links)"
	default:
		return sprintf("Claude copy has local changes at %s; compare with %s and move the Claude copy aside before running skillman repair-links", mirror, Dest(project, name))
	}
}

func checkInstallTargets(project, name string, prev *Entry, copies bool) error {
	dest, path := Dest(project, name), LinkP(project, name)
	for _, dir := range []string{filepath.Dir(dest), filepath.Dir(filepath.Dir(path)), filepath.Dir(path)} {
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errf("%s is not a directory", dir)
		}
	}
	if !Lexists(path) {
		return nil
	}
	if copies && !replaceableMirror(project, name, prev) {
		return errf("modified or unmanaged Claude copy: %s", path)
	}
	if !copies && !IsSymlink(path) {
		return errf("%s exists and is not a symlink; refusing to overwrite", path)
	}
	return nil
}
func createMirror(src, dst string) error {
	if Lexists(dst) {
		return errf("destination exists: %s", dst)
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(dir, ".mirror-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	next := filepath.Join(tmp, "copy")
	if err := CopyTree(src, next, nil); err != nil {
		return err
	}
	return os.Rename(next, dst)
}
func (c *installChange) backupMirror() error {
	tmp, err := os.MkdirTemp(filepath.Dir(c.link), ".previous-")
	if err != nil {
		return err
	}
	backup := filepath.Join(tmp, "copy")
	if err = os.Rename(c.link, backup); err != nil {
		os.Remove(tmp)
		return err
	}
	c.mirrorBackup = backup
	return nil
}
func (c installChange) finish() {
	if c.mirrorBackup != "" {
		os.RemoveAll(filepath.Dir(c.mirrorBackup))
	}
}

func ConvertCopies(d string, dry bool) ([]string, error) {
	return ConvertClaudeMode(d, "copy", dry)
}

func ClaudeMode(d string) (string, error) {
	m, _, err := LoadProjectManifest(d)
	if err != nil {
		return "", err
	}
	if m.copyMode() {
		return "copy", nil
	}
	return "symlink", nil
}

func ConvertClaudeMode(d, mode string, dry bool) ([]string, error) {
	if mode != "copy" && mode != "symlink" {
		return nil, errf("unknown Claude mode: %s", mode)
	}
	if err := CheckProjectDirs(d); err != nil {
		return nil, err
	}
	if !dry {
		unlock, err := ProjectLock(d)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	m, _, err := LoadProjectManifest(d)
	if err != nil {
		return nil, err
	}
	copies := m.copyMode()
	wantCopies := mode == "copy"
	var msgs []string
	var changes []installChange
	rollback := func(err error) error {
		for i := len(changes) - 1; i >= 0; i-- {
			err = errors.Join(err, changes[i].rollback())
		}
		return err
	}
	for _, n := range m.SkillNames() {
		if err := CheckName(n); err != nil {
			return msgs, rollback(err)
		}
		src, lp := Dest(d, n), LinkP(d, n)
		if IsSymlink(src) || !IsFile(filepath.Join(src, "SKILL.md")) {
			return msgs, rollback(errf("invalid source: %s", src))
		}
		if copies == wantCopies && linkOK(d, n, copies, TreeHash) {
			continue
		}
		if TreeHash(src) != m.Skills[n].TreeHash {
			files, converted := checkoutConverted(d, src, m.Skills[n].TreeHash)
			if !converted {
				return msgs, rollback(errf("review changed project copy before conversion: %s", src))
			}
			// Git reports the checkout as clean: restore the committed bytes.
			if !dry {
				if err := restoreCommitted(src, files); err != nil || TreeHash(src) != m.Skills[n].TreeHash {
					return msgs, rollback(errors.Join(errf("cannot restore committed bytes: %s", src), err))
				}
			}
		}
		target := "../../.agents/skills/" + n
		if Lexists(lp) {
			valid := false
			if IsSymlink(lp) {
				v, _ := os.Readlink(lp)
				valid = filepath.ToSlash(v) == target
			} else if copies && !wantCopies {
				valid = replaceableMirror(d, n, m.Skills[n])
			} else {
				valid = gitLinkPlaceholder(d, lp, target)
			}
			if !valid {
				return msgs, rollback(errf("unmanaged or modified Claude path: %s", lp))
			}
		}
		msgs = append(msgs, lp)
		if dry {
			continue
		}
		c := installChange{link: lp}
		if Lexists(lp) {
			if err := c.backupMirror(); err != nil {
				return msgs, rollback(err)
			}
		}
		changes = append(changes, c)
		if err := link(d, n, wantCopies); err != nil {
			return msgs, rollback(err)
		}
		changes[len(changes)-1].mirror = true
	}
	if !dry {
		m.top.Set("claude_mode", mode)
		if err := WriteManifest(d, m); err != nil {
			return msgs, rollback(err)
		}
		for _, c := range changes {
			c.finish()
		}
	}
	return msgs, nil
}
