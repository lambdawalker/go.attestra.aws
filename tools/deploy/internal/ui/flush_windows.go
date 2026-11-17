//go:build windows

package ui

import "golang.org/x/sys/windows"

var flushConsoleInput = windows.NewLazySystemDLL("kernel32.dll").NewProc("FlushConsoleInputBuffer")

func flushInput(fd uintptr) error {
	ok, _, e := flushConsoleInput.Call(fd)
	if ok == 0 {
		return e
	}
	return nil
}
