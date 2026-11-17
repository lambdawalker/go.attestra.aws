//go:build !linux && !darwin && !windows

package ui

import "errors"

func flushInput(fd uintptr) error {
	return errors.New("timed defaults are unsupported on this console; cannot safely discard buffered input")
}
