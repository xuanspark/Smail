//go:build windows

package config

import "os"

func hasMultipleInfoLinks(info os.FileInfo) bool {
	return false
}

func hasMultipleFileLinks(path string) bool {
	return false
}
