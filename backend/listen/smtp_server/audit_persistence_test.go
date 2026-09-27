package smtp_server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	listservice "git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/services/outbox"
	"git.pcux.cn/xuans/Smail/utils/context"
	smtp "github.com/emersion/go-smtp"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newAuthorizedAuditSession(t *testing.T, engine *xorm.Engine) (*Session, string) {
	t.Helper()
	user := models.User{ID: 1, Account: "sender", Password: "test-hash", Role: "user", SessionVersion: 1, SecurityVersion: 1}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetProtocolEnabled(user.ID, "smtp", true); err != nil {
		t.Fatal(err)
	}
	code, err := service.GenerateProtocolCode(user.ID, "smtp")
	if err != nil {
		t.Fatal(err)
	}
	_, grant, err := auth.AuthenticateProtocolSession("smtp", "192.0.2.93:1234", "sender@example.com", code)
	if err != nil {
		t.Fatal(err)
	}
	session := newAuditTestSession()
	session.authorization = grant
	session.From = "sender@example.com"
	session.To = []string{"recipient@example.net"}
	return session, code
}

func auditQueue(t *testing.T, engine *xorm.Engine) []models.OutboundDelivery {
	t.Helper()
	var jobs []models.OutboundDelivery
	if err := engine.Asc("id").Find(&jobs); err != nil {
		t.Fatal(err)
	}
	return jobs
}

func TestSMTPSubmissionQueuesOnlyAcceptedEnvelope(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	session, code := newAuthorizedAuditSession(t, engine)
	session.From = "sender@EXAMPLE.com"
	session.To = []string{"envelope@example.net", "envelope@EXAMPLE.net"}
	email := newAuditTestEmail("envelope-queue@example.com")
	email.Date = "2026-09-27T12:30:00+08:00"
	if err := session.deliverOutgoingEmail(email, "SMTP"); err != nil {
		t.Fatal(err)
	}
	jobs := auditQueue(t, engine)
	if len(jobs) != 1 || jobs[0].Recipient != "envelope@example.net" || jobs[0].State != "queued" || jobs[0].Attempts != 0 {
		t.Fatalf("queued envelope jobs = %+v", jobs)
	}
	var submission outbox.Submission
	if err := json.Unmarshal([]byte(jobs[0].Submission), &submission); err != nil {
		t.Fatal(err)
	}
	if submission.EnvelopeFrom != session.From || submission.Date != email.Date || submission.Authorization.Kind != "smtp" || submission.Trigger != "SMTP" {
		t.Fatal("SMTP envelope or authorization metadata was not preserved")
	}
	if strings.Contains(jobs[0].Submission, code) || strings.Contains(jobs[0].Submission, "test-hash") {
		t.Fatal("submission persisted reusable credentials")
	}
	if email.To[0].EmailAddress != "recipient@example.net" || email.MessageId <= 0 {
		t.Fatal("queue changed message headers or lost stored identity")
	}
	var saved models.Email
	if found, err := engine.ID(jobs[0].EmailID).Get(&saved); err != nil || !found {
		t.Fatalf("saved message: found=%v err=%v", found, err)
	}
	expectedDate, err := time.Parse(time.RFC3339, email.Date)
	if err != nil || !saved.SendDate.Equal(expectedDate) {
		t.Fatalf("stored message date = %v, want %v", saved.SendDate, expectedDate)
	}
	assertAuditCounts(t, engine, 1, 1)
	assertAuditStatuses(t, engine, consts.EmailStatusDeliveryPending)
}

func TestSMTPQueueInsertFailureRollsBackWholeSubmission(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	session, _ := newAuthorizedAuditSession(t, engine)
	if _, err := engine.Exec(`CREATE TRIGGER fail_queue_insert BEFORE INSERT ON outbound_delivery
		BEGIN SELECT RAISE(ABORT, 'forced queue insert failure'); END`); err != nil {
		t.Fatal(err)
	}
	email := newAuditTestEmail("queue-failure@example.com")
	assertSMTPCode(t, session.deliverOutgoingEmail(email, "SMTP"), 451)
	assertAuditCounts(t, engine, 0, 0)
	if len(auditQueue(t, engine)) != 0 || email.MessageId != 0 {
		t.Fatal("failed submission left partial durable state")
	}
}

func TestSMTPQueueBusyFailureDoesNotAcceptSubmission(t *testing.T) {
	engine, dsn := newAuditTestEngine(t)
	session, _ := newAuthorizedAuditSession(t, engine)
	blocker := beginAuditReadTransaction(t, dsn)
	assertSMTPCode(t, session.deliverOutgoingEmail(newAuditTestEmail("busy@example.com"), "SMTP"), 451)
	assertAuditCounts(t, engine, 0, 0)
	if len(auditQueue(t, engine)) != 0 {
		t.Fatal("failed transaction left delivery tasks")
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := session.deliverOutgoingEmail(newAuditTestEmail("busy@example.com"), "SMTP"); err != nil {
		t.Fatal(err)
	}
	assertAuditCounts(t, engine, 1, 1)
	assertAuditStatuses(t, engine, consts.EmailStatusDeliveryPending)
}

func TestSMTPQueueRetriesInitialTransactionBeforeAccepting(t *testing.T) {
	engine, dsn := newAuditTestEngine(t)
	session, _ := newAuthorizedAuditSession(t, engine)
	blocker := beginAuditReadTransaction(t, dsn)
	released := make(chan struct{})
	go func() {
		time.Sleep(35 * time.Millisecond)
		_ = blocker.Rollback()
		close(released)
	}()
	err := session.deliverOutgoingEmail(newAuditTestEmail("busy-retry@example.com"), "SMTP")
	<-released
	if err != nil {
		t.Fatal(err)
	}
	assertAuditCounts(t, engine, 1, 1)
	if len(auditQueue(t, engine)) != 1 {
		t.Fatal("transaction retry duplicated delivery tasks")
	}
}

func TestSMTPQueueRequiresCurrentAuthorization(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(fmt.Sprintf("revoke=%t", revoke), func(t *testing.T) {
			engine, _ := newAuditTestEngine(t)
			session := newAuditTestSession()
			if revoke {
				session, _ = newAuthorizedAuditSession(t, engine)
				if _, err := engine.Where("user_id = ?", 1).Cols("enabled").Update(&models.ProtocolCredential{Enabled: false}); err != nil {
					t.Fatal(err)
				}
			}
			assertSMTPCode(t, session.deliverOutgoingEmail(newAuditTestEmail("revoked@example.com"), "SMTP"), 535)
			assertAuditCounts(t, engine, 0, 0)
			if len(auditQueue(t, engine)) != 0 {
				t.Fatal("unauthorized submission was queued")
			}
		})
	}
}

func TestSameMessageIDCreatesIndependentQueuedSubmissions(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	session, _ := newAuthorizedAuditSession(t, engine)
	for i := 0; i < 2; i++ {
		email := newAuditTestEmail("reused-message-id@example.com")
		email.Text = []byte(fmt.Sprintf("body %d", i))
		if err := session.deliverOutgoingEmail(email, "SMTP"); err != nil {
			t.Fatal(err)
		}
	}
	assertAuditCounts(t, engine, 2, 2)
	assertMessageIDCount(t, engine, "reused-message-id@example.com", 2)
	if len(auditQueue(t, engine)) != 2 {
		t.Fatal("independent submissions were merged by Message-ID")
	}
}

func TestSMTPAcceptanceDoesNotInvokeDeliveryCompletionHooks(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	session, _ := newAuthorizedAuditSession(t, engine)
	hook := &queuedAuditHook{authenticationCaptureHook: &authenticationCaptureHook{}}
	hooks.HookList = map[string]framework.EmailHook{"queued": hook}
	if err := session.deliverOutgoingEmail(newAuditTestEmail("deferred-hook@example.com"), "SMTP"); err != nil {
		t.Fatal(err)
	}
	if hook.called {
		t.Fatal("SendAfter was called before the queue delivered the message")
	}
	assertAuditStatuses(t, engine, consts.EmailStatusDeliveryPending)
}

type queuedAuditHook struct {
	*authenticationCaptureHook
	called bool
}

func (h *queuedAuditHook) SendAfter(*context.Context, *parsemail.Email, map[string]error) {
	h.called = true
}

func TestOutgoingAuditRelationInsertRollsBackEmail(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	if _, err := engine.Exec(`CREATE TRIGGER fail_user_email_insert
		BEFORE INSERT ON user_email
		BEGIN
			SELECT RAISE(ABORT, 'forced user_email insert failure');
		END`); err != nil {
		t.Fatal(err)
	}

	email := newAuditTestEmail("relation-failure@example.com")
	_, _, err := saveEmail(newAuditTestSession().Ctx, email.Size, email, 1, int(consts.EmailTypeSend), nil, true, true)
	if err == nil {
		t.Fatal("saveEmail() error = nil, want relation insert failure")
	}
	if email.MessageId != 0 {
		t.Fatalf("email.MessageId = %d, want 0 after rollback", email.MessageId)
	}
	assertAuditCounts(t, engine, 0, 0)
}

func TestIncomingAuditRelationInsertRollsBackEmail(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	recipient := &models.User{Account: "recipient", Name: "Recipient"}
	if _, err := engine.Insert(recipient); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Exec(`CREATE TRIGGER fail_incoming_user_email_insert
		BEFORE INSERT ON user_email
		BEGIN
			SELECT RAISE(ABORT, 'forced incoming user_email insert failure');
		END`); err != nil {
		t.Fatal(err)
	}

	from := &parsemail.User{Name: "External", EmailAddress: "external@example.net"}
	email := &parsemail.Email{
		From:   from,
		Sender: from,
		To:     []*parsemail.User{{EmailAddress: "recipient@example.com"}},
		Text:   []byte("body"),
		Size:   4,
		MsgID:  "incoming-relation-failure@example.net",
	}
	_, _, err := saveEmail(&context.Context{}, email.Size, email, 0, int(consts.EmailTypeReceive), []string{"recipient@example.com"}, true, true)
	if err == nil {
		t.Fatal("saveEmail() error = nil, want incoming relation insert failure")
	}
	assertAuditCounts(t, engine, 0, 0)
}

func TestOutgoingPendingAuditMailboxVisibility(t *testing.T) {
	engine, _ := newAuditTestEngine(t)
	session := newAuditTestSession()
	email := newAuditTestEmail("pending-visibility@example.com")
	_, saved, err := saveEmail(session.Ctx, email.Size, email, session.Ctx.UserID, int(consts.EmailTypeSend), nil, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != consts.EmailStatusDeliveryPending {
		t.Fatalf("saved status = %d, want %d", saved.Status, consts.EmailStatusDeliveryPending)
	}
	assertAuditStatuses(t, engine, consts.EmailStatusDeliveryPending)

	outgoing, _, err := listservice.GetEmailList(session.Ctx, dto.SearchTag{
		Type:    consts.EmailTypeSend,
		Status:  -1,
		GroupId: -1,
	}, "", false, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range outgoing {
		if item.Id == saved.Id {
			found = true
			if item.Status != consts.EmailStatusDeliveryPending || !item.Error.Valid || item.Error.String != deliveryPendingError {
				t.Fatalf("outgoing pending audit = status:%d error:%#v", item.Status, item.Error)
			}
		}
	}
	if !found {
		t.Fatalf("pending audit email %d is missing from web outgoing list", saved.Id)
	}

	for _, mailbox := range []string{"INBOX", "Sent Messages"} {
		items, err := listservice.GetUEListByUID(session.Ctx, mailbox, 0, 0, nil)
		if err != nil {
			t.Fatalf("query IMAP mailbox %q: %v", mailbox, err)
		}
		for _, item := range items {
			if item.EmailID == saved.Id {
				t.Fatalf("pending audit email %d unexpectedly appears in IMAP mailbox %q", saved.Id, mailbox)
			}
		}
	}
}

func newAuditTestEngine(t *testing.T) (*xorm.Engine, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "audit.db") + "?_pragma=busy_timeout(20)"
	engine, err := xorm.NewEngine("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	engine.SetMaxIdleConns(1)
	if err = engine.Sync2(&models.User{}, &models.Email{}, &models.UserEmail{}, &models.OutboundDelivery{}, &models.ProtocolCredential{}, &models.ClientCredential{}); err != nil {
		engine.Close()
		t.Fatal(err)
	}
	if _, err = engine.Insert(&models.Email{Subject: "lock seed", MsgID: "lock-seed@example.com"}); err != nil {
		engine.Close()
		t.Fatal(err)
	}
	if _, err = engine.Insert(&models.UserEmail{UserID: 99, EmailID: 1}); err != nil {
		engine.Close()
		t.Fatal(err)
	}

	oldDB := db.Instance
	oldConfig := config.Instance
	oldHooks := hooks.HookList
	db.Instance = engine
	config.Instance = &config.Config{DbType: config.DBTypeSQLite, Domain: "example.com", Domains: []string{"example.com"}}
	hooks.HookList = nil
	t.Cleanup(func() {
		db.Instance = oldDB
		config.Instance = oldConfig
		hooks.HookList = oldHooks
		_ = engine.Close()
	})
	return engine, dsn
}

func beginAuditReadTransaction(t *testing.T, dsn string) *sql.Tx {
	t.Helper()
	blocker, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = blocker.Close() })
	tx, err := blocker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM email").Scan(&count); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	return tx
}

func newAuditTestSession() *Session {
	return &Session{Ctx: &context.Context{UserID: 1, UserAccount: "sender", IsAdmin: true}}
}

func newAuditTestEmail(messageID string) *parsemail.Email {
	from := &parsemail.User{Name: "Sender", EmailAddress: "sender@example.com"}
	return &parsemail.Email{
		From:    from,
		Sender:  from,
		To:      []*parsemail.User{{EmailAddress: "recipient@example.net"}},
		Subject: "audit reliability",
		Text:    []byte("body"),
		Size:    4,
		MsgID:   messageID,
	}
}

func successfulAuditSender(*context.Context, *parsemail.Email) (error, map[string]error) {
	return nil, nil
}

func assertSMTPCode(t *testing.T, err error, want int) {
	t.Helper()
	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) {
		t.Fatalf("error = %v (%T), want *smtp.SMTPError", err, err)
	}
	if smtpErr.Code != want {
		t.Fatalf("SMTP code = %d, want %d", smtpErr.Code, want)
	}
}

func assertAuditCounts(t *testing.T, engine *xorm.Engine, wantEmail, wantUserEmail int) {
	t.Helper()
	var emailCount, userEmailCount int
	if _, err := engine.SQL("SELECT count(*) FROM email WHERE id != 1").Get(&emailCount); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SQL("SELECT count(*) FROM user_email WHERE email_id != 1").Get(&userEmailCount); err != nil {
		t.Fatal(err)
	}
	if emailCount != wantEmail || userEmailCount != wantUserEmail {
		t.Fatalf("audit counts = email:%d user_email:%d, want email:%d user_email:%d", emailCount, userEmailCount, wantEmail, wantUserEmail)
	}
}

func assertAuditStatuses(t *testing.T, engine *xorm.Engine, want int8) {
	t.Helper()
	var emailStatus, userEmailStatus int8
	if _, err := engine.SQL("SELECT status FROM email WHERE id != 1 ORDER BY id DESC LIMIT 1").Get(&emailStatus); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.SQL("SELECT status FROM user_email WHERE email_id != 1 ORDER BY email_id DESC LIMIT 1").Get(&userEmailStatus); err != nil {
		t.Fatal(err)
	}
	if emailStatus != want || userEmailStatus != want {
		t.Fatalf("audit statuses = email:%d user_email:%d, want %d", emailStatus, userEmailStatus, want)
	}
}

func assertAuditError(t *testing.T, engine *xorm.Engine, want string, wantValid bool) {
	t.Helper()
	var got sql.NullString
	if _, err := engine.SQL("SELECT error FROM email WHERE id != 1 ORDER BY id DESC LIMIT 1").Get(&got); err != nil {
		t.Fatal(err)
	}
	if got.String != want || got.Valid != wantValid {
		t.Fatalf("audit error = %#v, want string %q valid %t", got, want, wantValid)
	}
}

func assertMessageIDCount(t *testing.T, engine *xorm.Engine, messageID string, want int) {
	t.Helper()
	var count int
	if _, err := engine.SQL("SELECT count(*) FROM email WHERE msg_id=?", messageID).Get(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("rows for Message-ID %q = %d, want %d", messageID, count, want)
	}
}
