package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

func sprintf(format string, a ...any) string {
	if len(a) == 0 {
		return format
	}
	return fmt.Sprintf(format, a...)
}

// Now formats like Python time.strftime("%Y-%m-%dT%H:%M:%S%z").
func Now() string { return time.Now().Format("2006-01-02T15:04:05-0700") }

// ── filesystem predicates (Python pathlib semantics) ──

func Exists(p string) bool { _, err := os.Stat(p); return err == nil } // follows links

func Lexists(p string) bool { _, err := os.Lstat(p); return err == nil }

func IsDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }

func IsFile(p string) bool { st, err := os.Stat(p); return err == nil && st.Mode().IsRegular() }

func IsSymlink(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

// ── JSON ──

// LoadJSON decodes p into v; returns false (leaving v untouched) when missing or invalid.
func LoadJSON(p string, v any) bool {
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// LoadMap reads a JSON object; missing/invalid -> empty map (Python load_json default {}).
func LoadMap(p string) map[string]any {
	m := map[string]any{}
	if !LoadJSON(p, &m) || m == nil {
		return map[string]any{}
	}
	return m
}

// MarshalPy renders like Python json.dumps(v, indent=n, ensure_ascii=False) + "\n".
func MarshalPy(v any, indent int) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent > 0 {
		enc.SetIndent("", strings.Repeat(" ", indent))
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func AtomicWrite(p string, data []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(p)+".")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(0o644)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, p)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// WriteJSON = Python write_json (indent=2, ensure_ascii=False, trailing newline).
func WriteJSON(p string, v any) error {
	b, err := MarshalPy(v, 2)
	if err != nil {
		return err
	}
	return AtomicWrite(p, b)
}

// ── locking ──

// DirLock takes an exclusive flock on a directory (no lock file left behind). Call the returned func to unlock.
func DirLock(d string) (func(), error) {
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	f, err := os.Open(d)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// ── git ──

type GitResult struct {
	Code           int
	Stdout, Stderr string
}

// Git runs git with GIT_TERMINAL_PROMPT=0 and a timeout (seconds).
func Git(args []string, cwd string, timeout int, env ...string) GitResult {
	if timeout <= 0 {
		timeout = 300
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := GitResult{Stdout: out.String(), Stderr: errb.String()}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			r.Code = ee.ExitCode()
		} else {
			r.Code = -1
			r.Stderr += err.Error()
		}
		if ctx.Err() != nil {
			r.Code = -1
			r.Stderr += " (timeout)"
		}
	}
	return r
}

// ── tree copy / move ──

// IgnoreFunc returns names (of entries directly in dir) to skip, like shutil.copytree(ignore=...).
type IgnoreFunc func(dir string, names []string) map[string]bool

// CopyTree = shutil.copytree(src, dst, symlinks=True, ignore=ignore): symlinks are copied as links,
// file modes and mtimes preserved. dst must not exist.
func CopyTree(src, dst string, ignore IgnoreFunc) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if Lexists(dst) {
		return fmt.Errorf("%s exists", dst)
	}
	if err := os.MkdirAll(dst, st.Mode().Perm()|0o700); err != nil {
		return err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name()
	}
	var skip map[string]bool
	if ignore != nil {
		skip = ignore(src, names)
	}
	for _, e := range ents {
		if skip[e.Name()] {
			continue
		}
		s, d := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		info, err := os.Lstat(s)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			t, err := os.Readlink(s)
			if err != nil {
				return err
			}
			if err := os.Symlink(t, d); err != nil {
				return err
			}
		case info.IsDir():
			if err := CopyTree(s, d, ignore); err != nil {
				return err
			}
		default:
			if err := CopyFile(s, d); err != nil {
				return err
			}
		}
	}
	os.Chmod(dst, st.Mode().Perm())
	os.Chtimes(dst, st.ModTime(), st.ModTime())
	return nil
}

// CopyFile = shutil.copy2 (content, mode, mtime).
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	os.Chmod(dst, st.Mode().Perm())
	return os.Chtimes(dst, st.ModTime(), st.ModTime())
}

// Move = shutil.move: rename, falling back to copy + remove across filesystems.
func Move(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		t, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Symlink(t, dst); err != nil {
			return err
		}
	case st.IsDir():
		if err := CopyTree(src, dst, nil); err != nil {
			return err
		}
	default:
		if err := CopyFile(src, dst); err != nil {
			return err
		}
	}
	return os.RemoveAll(src)
}

// ── walking with Python Path ordering ──

// relParts splits a slash path into components.
func relParts(rel string) []string { return strings.Split(filepath.ToSlash(rel), "/") }

// pyPathLess orders relative paths like sorted() of pathlib Paths: component-wise, not as plain strings.
func pyPathLess(a, b string) bool {
	pa, pb := relParts(a), relParts(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}

// WalkEntry is one path below a root, as Path.rglob("*") yields it (links are not followed).
type WalkEntry struct {
	Rel  string // slash-separated, relative to the root
	Path string
	Mode fs.FileMode // Lstat mode
}

func (w WalkEntry) IsSymlink() bool { return w.Mode&os.ModeSymlink != 0 }

// IsFile follows symlinks, like Path.is_file().
func (w WalkEntry) IsFile() bool {
	if w.IsSymlink() {
		return IsFile(w.Path)
	}
	return w.Mode.IsRegular()
}

// Rglob lists every entry under root (not root itself), sorted like sorted(root.rglob("*")).
func Rglob(root string) []WalkEntry {
	var out []WalkEntry
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		out = append(out, WalkEntry{Rel: filepath.ToSlash(rel), Path: p, Mode: info.Mode()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return pyPathLess(out[i].Rel, out[j].Rel) })
	return out
}

func hasIgnoredPart(rel string, ignore map[string]bool) bool {
	for _, p := range relParts(rel) {
		if ignore[p] {
			return true
		}
	}
	return false
}

// SortedDir lists names in dir (sorted). Missing dir -> nil.
func SortedDir(dir string) []os.DirEntry {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	return ents // os.ReadDir sorts by name
}

// ReadText reads a file as UTF-8 with replacement of invalid bytes (errors="replace").
func ReadText(p string) (string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return strings.ToValidUTF8(string(b), "�"), nil
}

// Contains reports whether xs has x.
func Contains(xs []string, x string) bool { return contains(xs, x) }

// Short truncates s to n bytes (for commit ids).
func Short(s string, n int) string { return short(s, n) }
