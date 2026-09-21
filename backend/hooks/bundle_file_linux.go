//go:build linux

package hooks

import (
	"bytes"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

func writeBundledPlugin(path string, data []byte) error {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("create bundled plugin returned an invalid file")
	}
	remove := true
	defer func() {
		if remove {
			_ = unix.Unlink(path)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(data)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0700); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	remove = false
	return nil
}
