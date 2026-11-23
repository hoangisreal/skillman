package core

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetupSkillCopy tracks ownership outside the skill directory so user edits survive upgrades.
func SetupSkillCopy(src, dst string, remove, dry bool) error {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(filepath.Clean(dst))))
	record := filepath.Join(STATE, "agent-setup", key)
	if !dry {
		unlock, err := DirLock(filepath.Dir(record))
		if err != nil {
			return err
		}
		defer unlock()
	}
	previous, _ := os.ReadFile(record)
	exists := Lexists(dst)
	if exists && (IsSymlink(dst) || !IsDir(dst) || TreeHash(dst) != strings.TrimSpace(string(previous))) {
		return errf("modified or unmanaged skill: %s", dst)
	}
	if dry || (remove && !exists) {
		return nil
	}
	if !remove && exists && TreeHash(src) == string(previous) {
		return nil
	}
	change := installChange{link: dst}
	if exists {
		if err := change.backupMirror(); err != nil {
			return err
		}
	}
	if remove {
		if err := os.Remove(record); err != nil {
			change.rollback()
			return err
		}
	} else {
		if err := createMirror(src, dst); err != nil {
			change.rollback()
			return err
		}
		change.mirror = true
		if err := AtomicWrite(record, []byte(TreeHash(dst))); err != nil {
			change.rollback()
			return err
		}
	}
	change.finish()
	return nil
}
