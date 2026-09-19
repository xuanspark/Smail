//go:build !windows

package config

import (
	"os"
	"syscall"
)

func hasMultipleInfoLinks(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return !ok || stat.Nlink != 1
}

func hasMultipleFileLinks(path string) bool {
	var stat syscall.Stat_t
	if err := syscall.Lstat(path, &stat); err != nil {
		return true
	}
	return stat.Nlink > 1
}
