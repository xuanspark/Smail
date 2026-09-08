//go:build windows

package config

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockStateFile(file *os.File) (func(), error) {
	position := &windows.Overlapped{Offset: 0x60000000}
	err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, position)
	return func() { _ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, position) }, err
}
