package runtimepath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCheckReadyRemovesInterruptedAtomicConfigurationTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(root, "config", ".config.json-0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(temporary, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckReady(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatalf("temporary file still exists: %v", err)
	}
}

func TestCheckReadyRejectsAtomicTemporaryDirectory(t *testing.T) {
	root := t.TempDir()
	configDirectory := filepath.Join(root, "config")
	if err := os.Mkdir(configDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDirectory, ".config.json-0123456789abcdef0123456789abcdef")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := CheckReady(root); err == nil {
		t.Fatal("expected invalid temporary directory to be rejected")
	}
}

func TestCheckReadyRejectsAtomicTemporarySymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("当前 Windows 测试环境通常没有创建符号链接的权限")
	}
	root := t.TempDir()
	configDirectory := filepath.Join(root, "config")
	if err := os.Mkdir(configDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDirectory, ".config.json-0123456789abcdef0123456789abcdef")
	if err := os.Symlink(filepath.Join(configDirectory, "missing"), path); err != nil {
		t.Fatal(err)
	}
	if err := CheckReady(root); err == nil {
		t.Fatal("expected invalid temporary symlink to be rejected")
	}
}

func TestPlanIgnoresKnownAtomicTemporaryFilesButRejectsUnknownFiles(t *testing.T) {
	root := t.TempDir()
	configDirectory := filepath.Join(root, "config")
	if err := os.Mkdir(configDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(configDirectory, ".smtp.json-0123456789abcdef0123456789abcdef")
	if err := os.WriteFile(known, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if moves, err := Plan(root); err != nil || len(moves) != 0 {
		t.Fatalf("known temporary file should be ignored, moves=%v err=%v", moves, err)
	}
	unknown := filepath.Join(configDirectory, ".not-an-atomic-file")
	if err := os.WriteFile(unknown, []byte("unexpected"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(root); err == nil {
		t.Fatal("unknown file should be rejected")
	}
}
