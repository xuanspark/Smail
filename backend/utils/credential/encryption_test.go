package credential

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	root := t.TempDir()
	encrypted, err := Encrypt(root, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, encryptedPrefix) || strings.Contains(encrypted, "secret-value") {
		t.Fatal("credential was not encrypted")
	}
	decrypted, err := Decrypt(root, encrypted)
	if err != nil || decrypted != "secret-value" {
		t.Fatal("credential could not be decrypted")
	}
	info, err := os.Stat(filepath.Join(root, "config", "credential.key"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("credential key permissions are too broad: %o", info.Mode().Perm())
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	root := t.TempDir()
	encrypted, err := Encrypt(root, "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	replacement := byte('A')
	if encrypted[len(encrypted)-1] == replacement {
		replacement = 'B'
	}
	encrypted = encrypted[:len(encrypted)-1] + string(replacement)
	if _, err := Decrypt(root, encrypted); err == nil {
		t.Fatal("tampered credential was accepted")
	}
}

func TestDecryptRejectsPlaintext(t *testing.T) {
	if _, err := Decrypt(t.TempDir(), "legacy-plaintext-secret"); err == nil {
		t.Fatal("plaintext credential was accepted by strict decryption")
	}
}

func TestEncryptKeepsEmptyAndProtectsPrefixValues(t *testing.T) {
	root := t.TempDir()
	if value, err := Encrypt(root, ""); err != nil || value != "" {
		t.Fatal("empty credential changed")
	}
	plaintext := "enc:v1:third-party-secret"
	encrypted, err := Encrypt(root, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == plaintext || !strings.HasPrefix(encrypted, encryptedPrefix) {
		t.Fatal("prefix credential was not encrypted")
	}
	decrypted, err := Decrypt(root, encrypted)
	if err != nil || decrypted != plaintext {
		t.Fatal("prefix credential did not round trip")
	}
}

func TestCredentialKeyTemporaryNamesAreIndependentRandomValues(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 32; i++ {
		name, err := newCredentialKeyTemporaryName()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(name, ".credential.key-") {
			t.Fatalf("unexpected temporary name: %s", name)
		}
		if _, ok := seen[name]; ok {
			t.Fatalf("duplicate temporary name: %s", name)
		}
		seen[name] = struct{}{}
	}
}

func TestReadKeyRejectsDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "credential.key"), make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "config")); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := readKey(root); err == nil {
		t.Fatal("credential directory symlink was accepted")
	}
}

func TestReadKeyRejectsKeySymlink(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside.key")
	if err := os.WriteFile(target, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(configDir, "credential.key")); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}
	if _, err := readKey(root); err == nil {
		t.Fatal("credential key symlink was accepted")
	}
}

func TestConcurrentEncryptUsesSingleCompleteKey(t *testing.T) {
	root := t.TempDir()
	const workers = 16
	values := make(chan string, workers)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			value, err := Encrypt(root, "shared-secret")
			if err != nil {
				errors <- err
				return
			}
			values <- value
		}()
	}
	group.Wait()
	close(values)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	for value := range values {
		plaintext, err := Decrypt(root, value)
		if err != nil || plaintext != "shared-secret" {
			t.Fatalf("concurrent credential could not be decrypted: %v", err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "credential.key" {
		t.Fatalf("unexpected credential directory contents: %#v", entries)
	}
}
