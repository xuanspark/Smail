//go:build windows

package config

func hasMultipleFileLinks(path string) bool {
	return false
}
