//go:build !windows

package core

import "syscall"

// accessWrite asks the kernel whether the directory is writable (W_OK),
// which also reports read-only mounts.
func accessWrite(dir string) error { return syscall.Access(dir, 0x2) }
