package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteReplacesFileAtomically(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("atomic file content = %q, want new", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("atomic file permissions = %o, want 600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".config.json-") {
			t.Fatalf("temporary file was left behind: %s", entry.Name())
		}
	}
}

func TestWriteCreatesEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty")
	if err := Write(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("empty atomic file size = %d, want 0", info.Size())
	}
}
