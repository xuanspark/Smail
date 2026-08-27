//go:build linux

package hooks

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyPluginFileRejectsGrowthPastLimit(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	targetPath := filepath.Join(directory, "target.json")
	if err := os.WriteFile(sourcePath, []byte(strings.Repeat("a", int(maxPluginConfigBytes)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyPluginFile(sourcePath, targetPath, os.Getuid(), os.Getgid(), 0600); err == nil {
		t.Fatal("oversized plugin configuration was copied")
	}
	if _, err := os.Lstat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("oversized target was not removed: %v", err)
	}
}

func TestInheritedAbstractSocketRemainsReachableAcrossListenerCopy(t *testing.T) {
	socketName, err := newPluginSocketName(1)
	if err != nil {
		t.Fatal(err)
	}
	address := "@" + socketName
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := listener.File()
	if closeErr := listener.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	inherited, err := net.FileListener(file)
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Close()
	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := inherited.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
		accepted <- acceptErr
	}()
	conn, err := net.Dial("unix", address)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}
