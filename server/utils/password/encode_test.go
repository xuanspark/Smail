package password

import (
	"errors"
	"strings"
	"testing"
)

func TestHashAndVerify(t *testing.T) {
	encoded, err := Hash("StrongPass123")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(encoded, "StrongPass123") || Verify(encoded, "WrongPass123") {
		t.Fatal("password verification returned an invalid result")
	}
}

func TestHashRejectsInvalidLengths(t *testing.T) {
	for _, plain := range []string{"short", strings.Repeat("a", maxSecretSize+1)} {
		if _, err := Hash(plain); err == nil {
			t.Fatalf("invalid secret length was accepted: %d", len([]byte(plain)))
		}
	}
	if Verify(mustHash(t, "StrongPass123"), strings.Repeat("a", maxSecretSize+1)) {
		t.Fatal("oversized verification input was accepted")
	}
}

func TestUserPasswordLengthBoundaries(t *testing.T) {
	for _, plain := range []string{"12345678", "123456789012", "密码aa"} {
		if err := ValidateUserPassword(plain); err != nil {
			t.Fatalf("valid password length %d was rejected: %v", len([]byte(plain)), err)
		}
		if _, err := HashUserPassword(plain); err != nil {
			t.Fatalf("valid user password was not hashed: %v", err)
		}
	}
	for _, plain := range []string{"1234567", "1234567890123", "密码密码密码"} {
		if err := ValidateUserPassword(plain); !errors.Is(err, ErrInvalidLength) {
			t.Fatalf("invalid password length %d returned %v", len([]byte(plain)), err)
		}
		if _, err := HashUserPassword(plain); !errors.Is(err, ErrInvalidLength) {
			t.Fatalf("invalid user password length %d returned %v", len([]byte(plain)), err)
		}
	}
}

func TestVerifyRejectsNonCanonicalArgon2Parameters(t *testing.T) {
	encoded := mustHash(t, "StrongPass123")
	for _, modified := range []string{
		strings.Replace(encoded, "m=65536", "m=8192", 1),
		strings.Replace(encoded, "t=3", "t=1", 1),
		strings.Replace(encoded, "p=1", "p=2", 1),
		strings.Replace(encoded, "v=19", "v=16", 1),
	} {
		if Verify(modified, "StrongPass123") {
			t.Fatalf("non-canonical hash was accepted: %s", modified)
		}
	}
}

func mustHash(t *testing.T, plain string) string {
	t.Helper()
	encoded, err := Hash(plain)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
