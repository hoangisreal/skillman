package core

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

func DirLock(d string) (func(), error) {
	if err := os.MkdirAll(d, 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(d, ".skillman.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	var ov windows.Overlapped
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ov); err != nil {
		f.Close()
		return nil, err
	}
	return func() { windows.UnlockFileEx(h, 0, 1, 0, &ov); f.Close() }, nil
}
