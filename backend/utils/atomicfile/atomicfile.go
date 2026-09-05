package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func Write(path string, data []byte, perm os.FileMode) error {
	directory := filepath.Dir(path)
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	return WriteRoot(root, filepath.Base(path), data, perm)
}

func WriteRoot(root *os.Root, name string, data []byte, perm os.FileMode) error {
	if root == nil || name == "" || name == "." || filepath.IsAbs(name) {
		return fmt.Errorf("invalid atomic file path")
	}
	directory := filepath.Dir(name)
	base := filepath.Base(name)
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return err
	}
	temporaryName := filepath.Join(directory, "."+base+"-"+hex.EncodeToString(token))
	temporary, err := root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	closed := false
	committed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		if !committed {
			_ = root.Remove(temporaryName)
		}
	}()

	if err = temporary.Chmod(perm); err != nil {
		return err
	}
	written, err := temporary.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return fmt.Errorf("short atomic file write: wrote %d of %d bytes", written, len(data))
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		closed = true
		return err
	}
	closed = true
	if err = root.Rename(temporaryName, name); err != nil {
		return err
	}
	committed = true
	if err = root.Chmod(name, perm); err != nil {
		return err
	}
	return syncRootDirectory(root, directory)
}

func SyncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	return syncRootDirectory(root, ".")
}

func syncRootDirectory(root *os.Root, name string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := root.Open(name)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
