package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// SandboxHint is appended to errors that an agent sandbox typically causes.
const SandboxHint = "an agent sandbox may hide or protect this path; run skillman outside the sandbox, " +
	"or exclude it from the sandbox, e.g. add \"skillman\" to sandbox.excludedCommands in Claude Code"

// occupiedReason explains why an existing path cannot be used as a directory.
func occupiedReason(p string, info fs.FileInfo) string {
	m := info.Mode()
	switch {
	case m&fs.ModeDevice != 0 || m&fs.ModeCharDevice != 0:
		return sprintf("%s is masked by a device file (%s)", p, SandboxHint)
	case m&fs.ModeNamedPipe != 0:
		return sprintf("%s is a named pipe, not a directory", p)
	case m&fs.ModeSocket != 0:
		return sprintf("%s is a socket, not a directory", p)
	case m&fs.ModeSymlink != 0:
		return sprintf("%s is a symlink, not a directory", p)
	default:
		return sprintf("%s is a file, not a directory", p)
	}
}

// pathReason explains why p cannot be read as a skill folder, or "" when it can be inspected.
func pathReason(p string) string {
	info, err := os.Lstat(p)
	switch {
	case os.IsNotExist(err):
		return ""
	case err != nil:
		return ExplainFSError(err).Error()
	case info.Mode()&(fs.ModeDevice|fs.ModeCharDevice) != 0:
		return occupiedReason(p, info)
	}
	return ""
}

// ExplainFSError adds the sandbox hint to errors that sandboxes cause.
func ExplainFSError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EROFS) || errors.Is(err, fs.ErrPermission) {
		return errf("%v (%s)", err, SandboxHint)
	}
	return err
}

// StateWarnings explains what an install cannot record when the state
// directory (project registry and trash) is not writable.
func StateWarnings() []string {
	if err := WritableDir(STATE); err != nil {
		return []string{sprintf("the project registry and trash in %s cannot be written, so library updates will not list this project: %v", STATE, err)}
	}
	return []string{}
}

// nearestDir returns p or its closest existing ancestor.
func nearestDir(p string) string {
	for {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return p
		}
		p = parent
	}
}

// WritableDir reports why files cannot be created under dir (or its nearest
// existing ancestor), or nil. It does not write anything.
func WritableDir(dir string) error {
	d := nearestDir(dir)
	if err := accessWrite(d); err != nil {
		return ExplainFSError(&fs.PathError{Op: "write", Path: d, Err: err})
	}
	return nil
}
