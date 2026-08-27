package externalemail

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/models"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestRepositoryBindsVerifiesAndScopesByUser(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "external.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(&models.ExternalEmail{}); err != nil {
		t.Fatal(err)
	}
	vault := NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	repo, err := NewRepository(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	item, err := repo.Bind(BindRequest{UserID: 1, Email: "Owner@Example.com", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != models.ExternalEmailStatusPending || item.EmailCiphertext == "" {
		t.Fatalf("unexpected binding: %+v", item)
	}
	if _, err := repo.Bind(BindRequest{UserID: 1, Email: "other@example.com", Type: models.ExternalEmailTypePrimary}); err != ErrPrimaryLimit {
		t.Fatalf("primary limit error = %v", err)
	}
	if err := repo.Verify(2, item.ID); err != ErrBindingOwnership {
		t.Fatalf("cross-user verify error = %v", err)
	}
	if err := repo.Verify(1, item.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.List(1)
	if err != nil || len(stored) != 1 {
		t.Fatalf("list result = %v, %v", stored, err)
	}
	if stored[0].Status != models.ExternalEmailStatusVerified {
		t.Fatalf("status = %s", stored[0].Status)
	}
	decrypted, err := repo.Decrypt(stored[0])
	if err != nil || decrypted != "owner@example.com" {
		t.Fatalf("decrypted email = %q, err = %v", decrypted, err)
	}
}

func TestRepositoryRejectsInvalidEmailType(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "external.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	vault := NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	repo, err := NewRepository(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Bind(BindRequest{UserID: 1, Email: "owner@example.com", Type: "unknown"}); err != ErrInvalidBinding {
		t.Fatalf("error = %v, want invalid binding", err)
	}
}

func TestRepositoryAbandonsOnlyOwnedPendingBinding(t *testing.T) {
	engine, err := xorm.NewEngine("sqlite", filepath.Join(t.TempDir(), "external.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if err := engine.Sync2(&models.ExternalEmail{}); err != nil {
		t.Fatal(err)
	}
	vault := NewVaultWithKeys([]byte("01234567890123456789012345678901"), []byte("abcdefabcdefabcdefabcdefabcdefab"))
	repo, err := NewRepository(engine, vault)
	if err != nil {
		t.Fatal(err)
	}
	item, err := repo.Bind(BindRequest{UserID: 1, Email: "pending@example.com", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AbandonPending(2, item.ID); err != ErrBindingOwnership {
		t.Fatalf("cross-user abandon error = %v", err)
	}
	if err := repo.AbandonPending(1, item.ID); err != nil {
		t.Fatal(err)
	}
	if count, err := engine.ID(item.ID).Count(&models.ExternalEmail{}); err != nil || count != 0 {
		t.Fatalf("pending binding remains: count=%d err=%v", count, err)
	}
	verified, err := repo.Bind(BindRequest{UserID: 1, Email: "verified@example.com", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(1, verified.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AbandonPending(1, verified.ID); err != ErrBindingOwnership {
		t.Fatalf("verified binding was abandoned: %v", err)
	}
}
