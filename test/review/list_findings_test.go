package list

import (
	"fmt"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// 审查专用：每次新建临时邮箱，不读取真实账户或邮件。
func TestAuditPaginationMustPreserveTotal(t *testing.T) {
	previousConfig, previousDB := config.Instance, db.Instance
	t.Cleanup(func() {
		config.Instance, db.Instance = previousConfig, previousDB
	})
	initListTestDB(t)
	for index := 0; index < 31; index++ {
		message := models.Email{Subject: fmt.Sprintf("audit-%d", index)}
		if _, err := db.Instance.Insert(&message); err != nil {
			t.Fatal(err)
		}
		relation := models.UserEmail{UserID: 1, EmailID: message.Id}
		if _, err := db.Instance.Insert(&relation); err != nil {
			t.Fatal(err)
		}
	}
	tag := dto.SearchTag{Type: 0, Status: -1, GroupId: -1}
	for _, offset := range []int{0, 15, 30} {
		messages, total, err := GetEmailList(&context.Context{UserID: 1}, tag, "", false, offset, 15)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("offset=%d rows=%d total=%d", offset, len(messages), total)
		if total != 31 {
			t.Errorf("分页计数应保持为 31，实际为 %d", total)
		}
	}
}
