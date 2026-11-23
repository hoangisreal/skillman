package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func managedMirror(p string, e *Entry) bool {
	return e != nil && regularSkill(p) && TreeHash(p) == e.TreeHash
}

func checkMirrorTarget(path string, prev *Entry, copies bool) error {
	root := filepath.Dir(path)
	for _, dir := range []string{filepath.Dir(root), root} {
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
	if copies && !managedMirror(path, prev) {
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
		if copies && managedMirror(lp, m.Skills[n]) {
			continue
		}
		if TreeHash(src) != m.Skills[n].TreeHash {
			return msgs, rollback(errf("review changed project copy before conversion: %s", src))
		}
		target := "../../.agents/skills/" + n
		if Lexists(lp) {
			valid := false
			if IsSymlink(lp) {
				v, _ := os.Readlink(lp)
				valid = filepath.ToSlash(v) == target
			} else if st, e := os.Lstat(lp); e == nil && st.Mode().IsRegular() && st.Size() == int64(len(target)) {
				rel, _ := filepath.Rel(d, lp)
				g := Git([]string{"ls-files", "--stage", "--", filepath.ToSlash(rel)}, d, 10)
				b, _ := os.ReadFile(lp)
				if g.Code == 0 && strings.HasPrefix(g.Stdout, "120000 ") && string(b) == target {
					blob := Git([]string{"show", ":" + filepath.ToSlash(rel)}, d, 10)
					valid = blob.Code == 0 && blob.Stdout == target
				}
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
		if err := createMirror(src, lp); err != nil {
			return msgs, rollback(err)
		}
		changes[len(changes)-1].mirror = true
	}
	if !dry {
		m.top.Set("claude_mode", "copy")
		if err := WriteManifest(d, m); err != nil {
			return msgs, rollback(err)
		}
		for _, c := range changes {
			c.finish()
		}
	}
	return msgs, nil
}
