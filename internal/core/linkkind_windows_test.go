package core

import (
	"os"
	"syscall"
)

// isDirLink reports whether p is a directory symlink (Windows has two link kinds).
func isDirLink(p string) bool {
	fi, err := os.Lstat(p)
	if err != nil {
		return false
	}
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
}
