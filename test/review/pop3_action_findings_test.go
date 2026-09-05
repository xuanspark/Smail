package pop3_server

import (
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/internal/gopop"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// 复用临时 SQLite 和协议授权码夹具，不访问用户运行库。
func auditPOP3Session(t *testing.T) *gopop.Session {
	t.Helper()
	engine, user, code := newPOP3ProtocolAuthFixture(t)
	oldDB, oldConfig := db.Instance, config.Instance
	db.Instance = engine
	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com"}}
	t.Cleanup(func() {
		db.Instance, config.Instance = oldDB, oldConfig
	})
	session := &gopop.Session{User: "alice@example.com", InTls: true, Ctx: &context.Context{}}
	if err := (action{}).Pass(session, code); err != nil {
		t.Fatal(err)
	}
	if session.Ctx.(*context.Context).UserID != user.ID {
		t.Fatal("临时会话未关联预期用户")
	}
	return session
}

func auditPOP3Message(t *testing.T, session *gopop.Session, id int, status int8) {
	t.Helper()
	message := models.Email{Id: id, Subject: "audit-pop3", Size: 100}
	if _, err := db.Instance.Insert(&message); err != nil {
		t.Fatal(err)
	}
	relation := models.UserEmail{UserID: session.Ctx.(*context.Context).UserID, EmailID: id, Status: status}
	if _, err := db.Instance.Insert(&relation); err != nil {
		t.Fatal(err)
	}
}

func TestAuditPOP3MessageNumbersMustStartAtOne(t *testing.T) {
	session := auditPOP3Session(t)
	auditPOP3Message(t, session, 101, 0)
	auditPOP3Message(t, session, 305, 0)
	rows, err := (action{}).List(session, "")
	if err != nil || len(rows) != 2 {
		t.Fatalf("列表前置条件失败: rows=%v err=%v", rows, err)
	}
	_, lookupErr := (action{}).List(session, "1")
	t.Logf("message_count=%d list_message_numbers=%d,%d list_1_error=%v", len(rows), rows[0].Id, rows[1].Id, lookupErr)
	if rows[0].Id != 1 || rows[1].Id != 2 || lookupErr != nil {
		t.Fatal("POP3 消息编号使用全局邮件 ID，未建立会话级 1..N 映射")
	}
}

func TestAuditPOP3MarkedDeletionMustHideMessage(t *testing.T) {
	session := auditPOP3Session(t)
	auditPOP3Message(t, session, 1, 0)
	if err := (action{}).Delete(session, 1); err != nil {
		t.Fatal(err)
	}
	rows, err := (action{}).List(session, "")
	if err != nil {
		t.Fatal(err)
	}
	count, _, err := (action{}).Stat(session)
	if err != nil {
		t.Fatal(err)
	}
	_, uidlErr := (action{}).Uidl(session, "1")
	duplicateErr := (action{}).Delete(session, 1)
	missingErr := (action{}).Delete(session, 999999)
	t.Logf("after_dele_list_count=%d stat_count=%d uidl_error=%v duplicate_dele_error=%v missing_dele_error=%v", len(rows), count, uidlErr, duplicateErr, missingErr)
	if len(rows) != 0 || count != 0 || uidlErr == nil || duplicateErr == nil || missingErr == nil {
		t.Fatal("已标记删除邮件仍出现在会话查询中，重复及不存在的 DELE 也未被拒绝")
	}
}

func TestAuditPOP3STATMustMatchVisibleMailbox(t *testing.T) {
	session := auditPOP3Session(t)
	auditPOP3Message(t, session, 1, 0)
	auditPOP3Message(t, session, 2, 5)
	rows, err := (action{}).List(session, "")
	if err != nil {
		t.Fatal(err)
	}
	count, _, err := (action{}).Stat(session)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("inbox_messages=1 junk_messages=1 list_count=%d stat_count=%d", len(rows), count)
	if count != int64(len(rows)) {
		t.Fatal("STAT 纳入垃圾箱邮件，但 LIST 排除了垃圾箱，邮箱视图不一致")
	}
}

func TestAuditPOP3NegativeTOPMustNotPanic(t *testing.T) {
	session := auditPOP3Session(t)
	auditPOP3Message(t, session, 1, 0)
	if _, err := db.Instance.ID(1).Cols("from_address").Update(&models.Email{FromAddress: "sender@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (action{}).Top(session, 1, 0); err != nil {
		t.Fatalf("正常 TOP 前置条件失败: %v", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Logf("negative_top_panic=%v", recovered)
			t.Error("负数 TOP 行参数进入切片表达式并触发 panic")
		}
	}()
	if _, err := (action{}).Top(session, 1, -100000); err == nil {
		t.Error("负数 TOP 行参数必须明确拒绝")
	}
}
