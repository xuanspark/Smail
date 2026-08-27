package del_email

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func initDeleteTestDB(t *testing.T) {
	t.Helper()
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "delete.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})
}

func insertDeleteTestEmail(t *testing.T) models.Email {
	t.Helper()
	email := models.Email{Subject: "security test"}
	if _, err := db.Instance.Insert(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

func TestForcedDeleteDoesNotDeleteUnownedOrphanEmail(t *testing.T) {
	initDeleteTestDB(t)
	email := insertDeleteTestEmail(t)

	if err := DelEmail(&context.Context{UserID: 1}, []int{email.Id}, true); err != nil {
		t.Fatal(err)
	}

	found, err := db.Instance.ID(email.Id).Get(&models.Email{})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("unowned email was deleted")
	}
}

func TestForcedDeleteKeepsSharedEmail(t *testing.T) {
	initDeleteTestDB(t)
	email := insertDeleteTestEmail(t)
	relations := []models.UserEmail{
		{UserID: 1, EmailID: email.Id},
		{UserID: 2, EmailID: email.Id},
	}
	if _, err := db.Instance.Insert(&relations); err != nil {
		t.Fatal(err)
	}

	if err := DelEmail(&context.Context{UserID: 1}, []int{email.Id}, true); err != nil {
		t.Fatal(err)
	}

	found, err := db.Instance.ID(email.Id).Get(&models.Email{})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("shared email was deleted")
	}
	remaining, err := db.Instance.Where("email_id=? and user_id=?", email.Id, 2).Count(&models.UserEmail{})
	if err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining relation count = %d, want 1", remaining)
	}
}

func TestForcedDeleteRemovesLastOwnedEmail(t *testing.T) {
	initDeleteTestDB(t)
	email := insertDeleteTestEmail(t)
	relation := models.UserEmail{UserID: 1, EmailID: email.Id}
	if _, err := db.Instance.Insert(&relation); err != nil {
		t.Fatal(err)
	}

	if err := DelEmail(&context.Context{UserID: 1}, []int{email.Id}, true); err != nil {
		t.Fatal(err)
	}

	found, err := db.Instance.ID(email.Id).Get(&models.Email{})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("unreferenced owned email was not deleted")
	}
}
