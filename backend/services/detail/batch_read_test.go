package detail

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func initBatchReadTestDB(t *testing.T) {
	t.Helper()
	oldConfig := config.Instance
	oldDB := db.Instance
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "batch-read.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
		db.Instance = oldDB
		config.Instance = oldConfig
	})
}

func TestMarkEmailsReadRejectsPartiallyOwnedBatch(t *testing.T) {
	initBatchReadTestDB(t)
	if _, err := db.Instance.Insert(&models.UserEmail{UserID: 1, EmailID: 101}, &models.UserEmail{UserID: 2, EmailID: 202}); err != nil {
		t.Fatal(err)
	}

	err := MarkEmailsRead(&context.Context{UserID: 1}, []int{101, 202})
	if err == nil {
		t.Fatal("partially owned batch was accepted")
	}

	var relation models.UserEmail
	if has, loadErr := db.Instance.Where("user_id = ? and email_id = ?", 1, 101).Get(&relation); loadErr != nil || !has {
		t.Fatalf("load owned relation: has=%v err=%v", has, loadErr)
	}
	if relation.IsRead != 0 {
		t.Fatalf("owned relation changed after rejected batch: is_read=%d", relation.IsRead)
	}
}

func TestMarkEmailsReadRollsBackWhenUpdateFails(t *testing.T) {
	initBatchReadTestDB(t)
	if _, err := db.Instance.Insert(&models.UserEmail{UserID: 1, EmailID: 301}, &models.UserEmail{UserID: 1, EmailID: 302}); err != nil {
		t.Fatal(err)
	}
	trigger := "CREATE TRIGGER fail_batch_read BEFORE UPDATE OF is_read ON user_email " +
		"WHEN NEW.email_id = 302 BEGIN SELECT RAISE(ABORT, 'forced batch read failure'); END"
	if _, err := db.Instance.Exec(trigger); err != nil {
		t.Fatal(err)
	}

	err := MarkEmailsRead(&context.Context{UserID: 1}, []int{301, 302})
	if err == nil {
		t.Fatal("batch read update unexpectedly succeeded")
	}

	var relations []models.UserEmail
	if err := db.Instance.Where("user_id = ?", 1).OrderBy("email_id").Find(&relations); err != nil {
		t.Fatal(err)
	}
	if len(relations) != 2 || relations[0].IsRead != 0 || relations[1].IsRead != 0 {
		t.Fatalf("batch read was partially committed: %+v", relations)
	}
}

func TestMarkEmailsReadUpdatesOwnedBatchOnce(t *testing.T) {
	initBatchReadTestDB(t)
	if _, err := db.Instance.Insert(&models.UserEmail{UserID: 1, EmailID: 401}, &models.UserEmail{UserID: 1, EmailID: 402, IsRead: 1}); err != nil {
		t.Fatal(err)
	}

	if err := MarkEmailsRead(&context.Context{UserID: 1}, []int{401, 401, 402}); err != nil {
		t.Fatal(err)
	}

	var unread int64
	if unread, _ = db.Instance.Where("user_id = ? and is_read = 0", 1).Count(&models.UserEmail{}); unread != 0 {
		t.Fatalf("unread relation count = %d", unread)
	}
}

func TestMarkEmailsReadRejectsInvalidID(t *testing.T) {
	initBatchReadTestDB(t)
	if err := MarkEmailsRead(&context.Context{UserID: 1}, []int{0}); err == nil {
		t.Fatal("invalid email id was accepted")
	}
	if err := MarkEmailsRead(&context.Context{UserID: 1}, nil); err == nil {
		t.Fatal("empty email id list was accepted")
	}
}
