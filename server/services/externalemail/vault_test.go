package externalemail

import (
	"strings"
	"testing"
)

func TestVaultEncryptsAndFingerprintsWithoutPlaintext(t *testing.T) {
	vault := NewVaultWithKeys([]byte(strings.Repeat("a", 32)), []byte(strings.Repeat("b", 32)))
	ciphertext, err := vault.Encrypt("User@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ciphertext, "User@example.com") || strings.Contains(ciphertext, "user@example.com") {
		t.Fatal("ciphertext contains plaintext email")
	}
	first, err := vault.Fingerprint("User@example.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := vault.Fingerprint("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !EqualFingerprint(first, second) {
		t.Fatal("normalized fingerprints differ")
	}
	decrypted, err := vault.Decrypt(ciphertext)
	if err != nil || decrypted != "user@example.com" {
		t.Fatalf("decrypted email = %q, error = %v", decrypted, err)
	}
}

func TestVaultRejectsTamperingAndInvalidEmail(t *testing.T) {
	vault := NewVaultWithKeys([]byte(strings.Repeat("a", 32)), []byte(strings.Repeat("b", 32)))
	if _, err := vault.Encrypt("bad"); err == nil {
		t.Fatal("invalid email accepted")
	}
	ciphertext, err := vault.Encrypt("user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	last := ciphertext[len(ciphertext)-1]
	replacement := byte('A')
	if last == replacement {
		replacement = 'B'
	}
	tampered := ciphertext[:len(ciphertext)-1] + string(replacement)
	if _, err := vault.Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}
