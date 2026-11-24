//go:build !windows

package core

// isDirLink: other systems have a single kind of symlink.
func isDirLink(p string) bool { return IsSymlink(p) }
