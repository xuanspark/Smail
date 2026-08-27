package password

import (
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
	for _, plain := range []string{"short", strings.Repeat("a", maxPasswordSize+1)} {
		if _, err := Hash(plain); err == nil {
			t.Fatalf("invalid password length was accepted: %d", len([]byte(plain)))
		}
	}
	if Verify(mustHash(t, "StrongPass123"), strings.Repeat("a", maxPasswordSize+1)) {
		t.Fatal("oversized verification input was accepted")
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
