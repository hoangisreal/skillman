package core

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// committedSkill returns the skill's files as recorded in the Git index, when
// Git reports the working tree as clean: any difference is then only checkout
// conversion such as CRLF line endings. Link entries are not supported.
func committedSkill(project, dir string) (map[string][]byte, bool) {
	rel, err := filepath.Rel(project, dir)
	if err != nil {
		return nil, false
	}
	rel = filepath.ToSlash(rel)
	st := Git([]string{"status", "--porcelain", "--untracked-files=all", "--", rel}, project, 30)
	if st.Code != 0 || strings.TrimSpace(st.Stdout) != "" {
		return nil, false
	}
	// git status hides edits to assume-unchanged and skip-worktree entries.
	tags := Git([]string{"ls-files", "-v", "-z", "--", rel}, project, 30)
	if tags.Code != 0 {
		return nil, false
	}
	for _, line := range strings.Split(tags.Stdout, "\x00") {
		if line != "" && !strings.HasPrefix(line, "H ") {
			return nil, false
		}
	}
	ls := Git([]string{"ls-files", "-s", "-z", "--", rel}, project, 30)
	if ls.Code != 0 || ls.Stdout == "" {
		return nil, false
	}
	files := map[string][]byte{}
	for _, line := range strings.Split(ls.Stdout, "\x00") {
		meta, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		f := strings.Fields(meta)
		if len(f) != 3 || f[2] != "0" || (f[0] != "100644" && f[0] != "100755") {
			return nil, false
		}
		blob := Git([]string{"cat-file", "blob", f[1]}, project, 30)
		if blob.Code != 0 {
			return nil, false
		}
		files[strings.TrimPrefix(path, rel+"/")] = []byte(blob.Stdout)
	}
	return files, true
}

// hashFiles is TreeHash over in-memory files (regular files only).
func hashFiles(files map[string][]byte) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return pyPathLess(names[i], names[j]) })
	h := sha256.New()
	for _, n := range names {
		if hasIgnoredPart(n, HashIgnore) {
			continue
		}
		h.Write([]byte("F" + n + "\x00"))
		s := sha256.Sum256(files[n])
		h.Write(s[:])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// checkoutConverted reports whether a skill differs from want only because Git
// converted it on checkout, returning the committed bytes to restore.
func checkoutConverted(project, dir, want string) (map[string][]byte, bool) {
	if TreeHash(dir) == want {
		return nil, false
	}
	files, ok := committedSkill(project, dir)
	if !ok || hashFiles(files) != want {
		return nil, false
	}
	// Ignored files are invisible to git status but part of the hash.
	n := 0
	for _, e := range Rglob(dir) {
		if hasIgnoredPart(e.Rel, HashIgnore) || e.Mode.IsDir() {
			continue
		}
		if _, tracked := files[e.Rel]; !tracked || e.IsSymlink() {
			return nil, false
		}
		n++
	}
	if n != len(files) {
		return nil, false
	}
	return files, true
}

// restoreCommitted writes the committed bytes back over a converted checkout.
func restoreCommitted(dir string, files map[string][]byte) error {
	for rel, b := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if cur, err := os.ReadFile(p); err == nil && string(cur) == string(b) {
			continue
		}
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() {
			return errf("cannot restore %s", p)
		}
		if err := AtomicWrite(p, b); err != nil {
			return err
		}
		if err := os.Chmod(p, st.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

// unsafeTextAttributes reports whether a .gitattributes file sets attributes
// that change bytes on checkout (text, eol, crlf, ident, filter,
// working-tree-encoding), including "!attr", which re-enables core.autocrlf.
// "-attr" and binary are safe.
func unsafeTextAttributes(b []byte) bool {
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		for _, attr := range fields[1:] {
			if strings.HasPrefix(attr, "-") {
				continue
			}
			name, _, _ := strings.Cut(strings.TrimPrefix(attr, "!"), "=")
			switch name {
			case "text", "eol", "crlf", "ident", "filter", "working-tree-encoding":
				return true
			}
		}
	}
	return false
}

// gitLinkPlaceholder reports whether lp is the plain file Git writes for a
// committed symlink when symlinks are disabled (Git for Windows default).
func gitLinkPlaceholder(project, lp, target string) bool {
	st, err := os.Lstat(lp)
	if err != nil || !st.Mode().IsRegular() || st.Size() != int64(len(target)) {
		return false
	}
	rel, _ := filepath.Rel(project, lp)
	g := Git([]string{"ls-files", "--stage", "--", filepath.ToSlash(rel)}, project, 10)
	b, _ := os.ReadFile(lp)
	if g.Code != 0 || !strings.HasPrefix(g.Stdout, "120000 ") || string(b) != target {
		return false
	}
	blob := Git([]string{"show", ":" + filepath.ToSlash(rel)}, project, 10)
	return blob.Code == 0 && blob.Stdout == target
}
