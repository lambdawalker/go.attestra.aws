//go:build darwin

package ui

import "golang.org/x/sys/unix"

func flushInput(fd uintptr) error { return unix.IoctlSetPointerInt(int(fd), unix.TIOCFLUSH, 1) }
