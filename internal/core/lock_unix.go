//go:build !windows

package core

import (
	"os"
	"syscall"
)

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
