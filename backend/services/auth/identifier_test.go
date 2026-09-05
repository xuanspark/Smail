package auth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
)

func TestResolveIdentifierUsesVerifiedFullEmailBinding(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(t.TempDir(), "identifier.db"),
	}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil && db.Instance != oldDB {
			_ = db.Instance.Close()
		}
		db.Instance = oldDB
		config.Instance = oldConfig
	})

	user := models.User{Account: "alice", Name: "Alice"}
	if err := SetPassword(&user, "CorrectP12"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	verifiedAt := time.Now().UTC()
	canonicalAddress := "alice@example.com"
	if _, err := db.Instance.Insert(&models.UserEmail{
		UserID:           user.ID,
		Address:          "Alice@Example.com",
		AddressCanonical: &canonicalAddress,
		VerifiedAt:       &verifiedAt,
	}); err != nil {
		t.Fatal(err)
	}
	pendingAddress := "pending@example.com"
	if _, err := db.Instance.Insert(&models.UserEmail{
		UserID:           user.ID,
		Address:          "pending@example.com",
		AddressCanonical: &pendingAddress,
	}); err != nil {
		t.Fatal(err)
	}

	identity, err := ResolveIdentifier(" Alice@EXAMPLE.COM ")
	if err != nil {
		t.Fatalf("resolve verified email: %v", err)
	}
	if identity.Kind != IdentifierEmail || identity.UserID != user.ID || identity.Account != user.Account || identity.LoginAddress != "alice@example.com" {
		t.Fatalf("resolved identity = %+v", identity)
	}
	if _, err := AuthenticateIdentifier("Alice@Example.com", "CorrectP12"); err != nil {
		t.Fatalf("authenticate verified email: %v", err)
	}
	if _, err := AuthenticateIdentifier("pending@example.com", "CorrectP12"); err == nil {
		t.Fatal("unverified email binding was accepted")
	}
}

func TestResolveIdentifierRejectsMalformedOrOversizedEmail(t *testing.T) {
	for _, identifier := range []string{
		"alice@@example.com",
		"Alice <alice@example.com>",
		"alice@example.com\r\nX-Header: value",
		strings.Repeat("a", maxLoginIdentifierBytes+1),
		"alice@-invalid.example",
	} {
		if _, err := NormalizeIdentifierKey(identifier); err == nil {
			t.Fatalf("identifier %q was accepted", identifier)
		}
	}
}

func TestNormalizeIdentifierKeepsLegacyAccountCompatibility(t *testing.T) {
	if got, err := NormalizeIdentifier("alice"); err != nil || got != "alice" {
		t.Fatalf("normalized account = %q, err = %v", got, err)
	}
	if got, err := NormalizeIdentifierKey(" Alice@Example.com "); err != nil || got != "alice@example.com" {
		t.Fatalf("normalized email key = %q, err = %v", got, err)
	}
}
