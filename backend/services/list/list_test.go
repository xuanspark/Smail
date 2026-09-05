package list

import (
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func initListTestDB(t *testing.T) {
	t.Helper()
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, DbDSN: filepath.Join(t.TempDir(), "list.db")}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})
}

func TestGetUEListByUIDUsesFullPath(t *testing.T) {
	initListTestDB(t)
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

	result, err := GetUEListByUID(&context.Context{UserID: 1}, "first/leaf", 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != firstMail.ID {
		t.Fatalf("GetUEListByUID returned %#v, want relation %d", result, firstMail.ID)
	}
}

func TestGetUEListByUIDDefaultMailboxExcludesGroupedMail(t *testing.T) {
	initListTestDB(t)
	group := models.Group{Name: "custom", UserId: 1, FullPath: "custom"}
	if _, err := db.Instance.Insert(&group); err != nil {
		t.Fatal(err)
	}
	inboxMail := models.UserEmail{UserID: 1, EmailID: 201, GroupId: 0, Status: 0}
	groupedMail := models.UserEmail{UserID: 1, EmailID: 202, GroupId: group.ID, Status: 0}
	if _, err := db.Instance.Insert(&inboxMail, &groupedMail); err != nil {
		t.Fatal(err)
	}

	result, err := GetUEListByUID(&context.Context{UserID: 1}, "INBOX", 0, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != inboxMail.ID {
		t.Fatalf("GetUEListByUID returned %#v, want inbox relation %d", result, inboxMail.ID)
	}
}
