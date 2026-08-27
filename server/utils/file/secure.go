package file

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrUnsafeRegularFile = errors.New("unsafe regular file")

// ReadRegularFile reads a regular file through a directory-scoped handle.
func ReadRegularFile(path string, maxSize int64) ([]byte, error) {
	if strings.TrimSpace(path) == "" || maxSize <= 0 {
		return nil, ErrUnsafeRegularFile
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return nil, ErrUnsafeRegularFile
	}
	root, err := os.OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return nil, ErrUnsafeRegularFile
	}
	defer root.Close()

	name := filepath.Base(absolute)
	info, err := root.Lstat(name)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxSize {
		return nil, ErrUnsafeRegularFile
	}
	data, err := root.ReadFile(name)
	if err != nil || int64(len(data)) > maxSize {
		return nil, ErrUnsafeRegularFile
	}
	return data, nil
}

func ControlledRelativePath(base, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", errors.New("path is required")
	}
	basePath, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", err
	}
	targetPath := value
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(basePath, targetPath)
	}
	targetPath, err = filepath.Abs(filepath.Clean(targetPath))
	if err != nil {
		return "", err
	}
	relativePath, err := filepath.Rel(basePath, targetPath)
	if err != nil || relativePath == "." || filepath.IsAbs(relativePath) || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes controlled directory")
	}
	return relativePath, nil
}
