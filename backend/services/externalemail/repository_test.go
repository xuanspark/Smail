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
	pending, err := repo.FindPending(1, "OWNER@example.com", models.ExternalEmailTypePrimary)
	if err != nil || pending.ID != item.ID {
		t.Fatalf("pending binding = %+v, err = %v", pending, err)
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
	if _, err := repo.FindPending(1, "owner@example.com", models.ExternalEmailTypePrimary); err != ErrBindingNotFound {
		t.Fatalf("verified binding remained pending: %v", err)
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

func TestRepositoryReplacesExternalEmailOnlyAfterVerification(t *testing.T) {
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
	oldBinding, err := repo.Bind(BindRequest{UserID: 1, Email: "old@example.com", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(1, oldBinding.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := repo.BeginReplacement(BindRequest{UserID: 1, Email: "new@example.com", Type: models.ExternalEmailTypePrimary})
	if err != nil {
		t.Fatal(err)
	}
	before, err := repo.FindVerifiedPrimaryByUser(1)
	if err != nil || before.ID != oldBinding.ID {
		t.Fatalf("old binding was not retained before verification: %+v, %v", before, err)
	}
	if err := repo.VerifyReplacement(2, pending.ID); err != ErrBindingOwnership {
		t.Fatalf("cross-user replacement error = %v", err)
	}
	if err := repo.VerifyReplacement(1, pending.ID); err != nil {
		t.Fatal(err)
	}
	after, err := repo.FindVerifiedPrimaryByUser(1)
	if err != nil || after.ID != pending.ID {
		t.Fatalf("new binding was not activated: %+v, %v", after, err)
	}
	items, err := repo.List(1)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[int]string{}
	for _, item := range items {
		statuses[item.ID] = item.Status
	}
	if statuses[oldBinding.ID] != models.ExternalEmailStatusDisabled || statuses[pending.ID] != models.ExternalEmailStatusVerified {
		t.Fatalf("unexpected replacement statuses: %+v", statuses)
	}
}

func TestRepositoryRebindsDisabledEmailWithFreshVerification(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		name := "bind"
		if replacement {
			name = "replacement"
		}
		t.Run(name, func(t *testing.T) {
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
			item, err := repo.Bind(BindRequest{UserID: 1, Email: "owner@example.com", Type: models.ExternalEmailTypeBackup})
			if err != nil {
				t.Fatal(err)
			}
			if err := repo.Verify(1, item.ID); err != nil {
				t.Fatal(err)
			}
			bind := repo.Bind
			verify := repo.Verify
			if replacement {
				bind = repo.BeginReplacement
				verify = repo.VerifyReplacement
			}
			request := BindRequest{UserID: 1, Email: "OWNER@example.com", Type: models.ExternalEmailTypePrimary}
			if _, err := bind(request); err != ErrBindingExists {
				t.Fatalf("已验证邮箱不应直接更改用途：%v", err)
			}
			if err := repo.Disable(1, item.ID); err != nil {
				t.Fatal(err)
			}
			other := request
			other.UserID = 2
			if _, err := bind(other); err != ErrBindingExists {
				t.Fatalf("不能复用其他用户的停用记录：%v", err)
			}
			pending, err := bind(request)
			if err != nil {
				t.Fatal(err)
			}
			if pending.ID != item.ID || pending.Status != models.ExternalEmailStatusPending || pending.VerifiedAt != nil || pending.EmailType != models.ExternalEmailTypePrimary || pending.CredentialVersion <= item.CredentialVersion {
				t.Fatalf("重新绑定必须保留记录并重新验证：%+v", pending)
			}
			if _, err := repo.FindVerifiedPrimaryByUser(1); err != ErrBindingNotFound {
				t.Fatalf("未验证邮箱不能参与登录：%v", err)
			}
			stored, err := repo.List(1)
			if err != nil || len(stored) != 1 || stored[0].VerifiedAt != nil || stored[0].Status != models.ExternalEmailStatusPending {
				t.Fatalf("旧验证状态未清除：%+v，%v", stored, err)
			}
			if err := verify(1, pending.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.FindVerifiedPrimaryByUser(1); err != nil {
				t.Fatalf("重新验证后应可用于登录：%v", err)
			}
			if !replacement {
				if err := repo.Disable(1, item.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := repo.Bind(BindRequest{UserID: 1, Email: "other@example.com", Type: models.ExternalEmailTypePrimary}); err != nil {
					t.Fatal(err)
				}
				if _, err := bind(request); err != ErrPrimaryLimit {
					t.Fatalf("重新绑定不能绕过主要邮箱数量限制：%v", err)
				}
			}
		})
	}
}
