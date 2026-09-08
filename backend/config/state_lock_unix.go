//go:build !windows

package config

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockStateFile(file *os.File) (func(), error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }, err
}
