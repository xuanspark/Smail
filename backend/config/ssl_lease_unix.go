//go:build !windows

package config

import (
	"os"
	"path/filepath"
)

func openSSLLease(path string) (*os.File, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(filepath.Base(absolute), os.O_RDWR, 0600)
	_ = root.Close()
	return file, err
}
