package list

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
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

func TestWebListOrdersByDisplayedDateAcrossPages(t *testing.T) {
	initListTestDB(t)
	base := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	// 旧草稿后发；相同投递时间的邮件必须有稳定次序。
	for _, item := range []struct {
		id   int
		hour int
	}{
		{101, 4},
		{102, 2},
		{103, 3},
		{104, 3},
	} {
		message := models.Email{
			Id: item.id, Type: consts.EmailTypeSend,
			SendDate: base.Add(time.Duration(item.hour) * time.Hour),
		}
		if _, err := db.Instance.Insert(&message); err != nil {
			t.Fatal(err)
		}
		relation := models.UserEmail{UserID: 1, EmailID: item.id, GroupId: models.Sent}
		if _, err := db.Instance.Insert(&relation); err != nil {
			t.Fatal(err)
		}
	}
	tag := dto.SearchTag{Type: consts.EmailTypeSend, Status: -1, GroupId: models.Sent}
	ctx := &context.Context{UserID: 1}
	for page, want := range [][]int{{101, 104}, {103, 102}} {
		messages, total, err := GetEmailList(ctx, tag, "", false, page*2, 2)
		if err != nil {
			t.Fatal(err)
		}
		if total != 4 || len(messages) != len(want) {
			t.Fatalf("page %d: total=%d, length=%d", page, total, len(messages))
		}
		for i, id := range want {
			if messages[i].Id != id {
				t.Fatalf("page %d item %d: got %d, want %d", page, i, messages[i].Id, id)
			}
		}
	}
	query, _ := genSQL(ctx, false, tag, "", true, 0, 2)
	if !strings.Contains(query, " order by e.id desc") || strings.Contains(query, "order by e.send_date") {
		t.Fatalf("POP3 ordering changed: %s", query)
	}
	query, _ = genSQL(ctx, true, tag, "", false, 0, 2)
	if strings.Contains(query, "order by") {
		t.Fatalf("count query should not sort: %s", query)
	}
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
