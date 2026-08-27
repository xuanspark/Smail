package file

import (
	"os"
	"path/filepath"
	"testing"
)

func TestControlledRelativePath(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "nested", "key.pem")
	relative, err := ControlledRelativePath(base, inside)
	if err != nil || relative != filepath.Join("nested", "key.pem") {
		t.Fatalf("unexpected controlled path result: %q, %v", relative, err)
	}
	if _, err := ControlledRelativePath(base, filepath.Join(base, "..", "outside.pem")); err == nil {
		t.Fatal("outside path must be rejected")
	}
	if _, err := ControlledRelativePath(base, ""); err == nil {
		t.Fatal("empty path must be rejected")
	}
}

func TestReadRegularFileRejectsSymlinkAndOversizedFile(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "regular.txt")
	if err := os.WriteFile(regular, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := ReadRegularFile(regular, 16)
	if err != nil || string(data) != "safe" {
		t.Fatalf("regular file read failed: %q, %v", data, err)
	}
	if err := os.WriteFile(regular, []byte("too large"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegularFile(regular, 4); err == nil {
		t.Fatal("oversized file was accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := ReadRegularFile(link, 16); err == nil {
		t.Fatal("symlink was accepted")
	}
}
