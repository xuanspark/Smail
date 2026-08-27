package credential

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVaultRejectsUnsupportedVersionAndWrongKey(t *testing.T) {
	vault, err := NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := vault.Encrypt("provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Decrypt(CurrentVersion+1, ciphertext); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("unexpected version error: %v", err)
	}
	other, err := NewVault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decrypt(CurrentVersion, ciphertext); err == nil {
		t.Fatal("wrong key was accepted")
	}
}

func TestVaultRejectsBroadKeyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are unavailable")
	}
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.Mkdir(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "credential.key"), make([]byte, 32), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewVault(root); err == nil {
		t.Fatal("broad credential key permissions were accepted")
	}
}

func TestVaultRejectsBroadDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are unavailable")
	}
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.Mkdir(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewVault(root); err == nil {
		t.Fatal("broad credential directory permissions were accepted")
	}
}
