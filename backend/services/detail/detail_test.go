package detail

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	listservice "git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func TestFindUEUsesCurrentUserForUID(t *testing.T) {
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "detail.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})

	owned := models.UserEmail{UserID: 1, EmailID: 10}
	other := models.UserEmail{UserID: 2, EmailID: 20}
	if _, err := db.Instance.Insert(&owned, &other); err != nil {
		t.Fatal(err)
	}
	ctx := &context.Context{UserID: 1}
	result, err := FindUE(ctx, "INBOX", listservice.ImapListReq{UidList: []int{owned.ID, other.ID}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != owned.ID || result[0].UserID != ctx.UserID {
		t.Fatalf("FindUE returned %#v, want only user %d relation", result, ctx.UserID)
	}
}

func TestFindUEUsesFullPath(t *testing.T) {
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "detail-path.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})

	first := models.Group{Name: "leaf", UserId: 1, FullPath: "first/leaf"}
	second := models.Group{Name: "leaf", UserId: 1, FullPath: "second/leaf"}
	if _, err := db.Instance.Insert(&first, &second); err != nil {
		t.Fatal(err)
	}
	firstMail := models.UserEmail{UserID: 1, EmailID: 101, GroupId: first.ID}
	secondMail := models.UserEmail{UserID: 1, EmailID: 102, GroupId: second.ID}
	if _, err := db.Instance.Insert(&firstMail, &secondMail); err != nil {
		t.Fatal(err)
	}

	result, err := FindUE(&context.Context{UserID: 1}, "first/leaf", listservice.ImapListReq{Star: 1, End: 10}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != firstMail.ID {
		t.Fatalf("FindUE returned %#v, want relation %d", result, firstMail.ID)
	}
}

func TestFindUEDefaultMailboxExcludesGroupedMail(t *testing.T) {
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "detail-default.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})

	group := models.Group{Name: "custom", UserId: 1, FullPath: "custom"}
	if _, err := db.Instance.Insert(&group); err != nil {
		t.Fatal(err)
	}
	inboxMail := models.UserEmail{UserID: 1, EmailID: 201, GroupId: 0, Status: 0}
	groupedMail := models.UserEmail{UserID: 1, EmailID: 202, GroupId: group.ID, Status: 0}
	if _, err := db.Instance.Insert(&inboxMail, &groupedMail); err != nil {
		t.Fatal(err)
	}

	result, err := FindUE(&context.Context{UserID: 1}, "INBOX", listservice.ImapListReq{Star: 1, End: 10}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != inboxMail.ID {
		t.Fatalf("FindUE returned %#v, want inbox relation %d", result, inboxMail.ID)
	}
}

func TestGetEmailDetailRejectsCorruptedStoredAttachments(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "detail-corrupted.db")}
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

	email := models.Email{Attachments: "{", MsgID: "detail-corrupted@example.com"}
	if _, err := db.Instance.Insert(&email); err != nil {
		t.Fatal(err)
	}
	relation := models.UserEmail{UserID: 1, EmailID: email.Id}
	if _, err := db.Instance.Insert(&relation); err != nil {
		t.Fatal(err)
	}

	if _, err := GetEmailDetail(&context.Context{UserID: 1}, email.Id, true); err == nil {
		t.Fatal("corrupted attachment JSON was accepted")
	}
	var stored models.UserEmail
	has, err := db.Instance.ID(relation.ID).Get(&stored)
	if err != nil || !has {
		t.Fatalf("load relation: has=%v err=%v", has, err)
	}
	if stored.IsRead != 0 {
		t.Fatal("email was marked read before stored data validation completed")
	}
}

func TestGetEmailDetailMarksOwnedEmailRead(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "detail-read.db")}
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

	email := models.Email{Attachments: "", MsgID: "detail-read@example.com"}
	if _, err := db.Instance.Insert(&email); err != nil {
		t.Fatal(err)
	}
	relation := models.UserEmail{UserID: 1, EmailID: email.Id}
	if _, err := db.Instance.Insert(&relation); err != nil {
		t.Fatal(err)
	}

	result, err := GetEmailDetail(&context.Context{UserID: 1}, email.Id, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsRead != 1 {
		t.Fatalf("unexpected read state: %d", result.IsRead)
	}
}

func TestGetEmailDetailRequiresMailboxOwnershipForAdministrators(t *testing.T) {
	oldConfig := config.Instance
	oldDB := db.Instance
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "detail-admin.db")}
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

	email := models.Email{MsgID: "detail-admin@example.com"}
	if _, err := db.Instance.Insert(&email); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Instance.Insert(&models.UserEmail{UserID: 1, EmailID: email.Id}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetEmailDetail(&context.Context{UserID: 2, IsAdmin: true}, email.Id, false); err == nil {
		t.Fatal("administrator read another user's email without mailbox ownership")
	}
}
