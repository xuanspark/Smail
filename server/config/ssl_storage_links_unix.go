//go:build !windows

package config

import "syscall"

func hasMultipleFileLinks(path string) bool {
	var stat syscall.Stat_t
	if err := syscall.Lstat(path, &stat); err != nil {
		return true
	}
	return stat.Nlink > 1
}
