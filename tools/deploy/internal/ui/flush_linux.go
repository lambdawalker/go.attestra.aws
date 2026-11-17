//go:build linux

package ui

import "golang.org/x/sys/unix"

func flushInput(fd uintptr) error { return unix.IoctlSetInt(int(fd), unix.TCFLSH, unix.TCIFLUSH) }
