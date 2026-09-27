package outbox

import (
	stdcontext "context"
	"encoding/json"
	"errors"
	"io"
	"net/textproto"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/smtp"
	"xorm.io/xorm"
)

func queueFixture(t *testing.T) (*xorm.Engine, models.Email, []models.OutboundDelivery) {
	t.Helper()
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	engine.SetMaxOpenConns(1)
	t.Cleanup(func() { engine.Close() })
	if err := engine.Sync2(&models.Email{}, &models.UserEmail{}, &models.User{}, &models.OutboundDelivery{}); err != nil {
		t.Fatal(err)
	}
	user := models.User{Account: "sender", Name: "sender", Password: "test", Role: "user"}
	if _, err := engine.Insert(&user); err != nil {
		t.Fatal(err)
	}
	email := models.Email{Type: consts.EmailTypeSend, Status: consts.EmailStatusDeliveryPending, SendUserID: user.ID, FromAddress: "sender@example.com", To: `[{"EmailAddress":"one@example.com"},{"EmailAddress":"two@example.com"}]`, Cc: `[{"EmailAddress":"one@EXAMPLE.com"}]`, MsgID: "stable@example.com"}
	err = db.Transaction(engine, func(session *xorm.Session) error {
		if _, err := session.Insert(&email); err != nil {
			return err
		}
		if _, err := session.Insert(&models.UserEmail{UserID: user.ID, EmailID: email.Id, Status: consts.EmailStatusDeliveryPending}); err != nil {
			return err
		}
		return Enqueue(session, email)
	})
	if err != nil {
		t.Fatal(err)
	}
	var jobs []models.OutboundDelivery
	if err := engine.Asc("id").Find(&jobs); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("duplicate recipient: %v", jobs)
	}
	return engine, email, jobs
}

func smtpQueueFixture(t *testing.T) (*xorm.Engine, []models.OutboundDelivery, *credentials.Service) {
	t.Helper()
	engine, email, jobs := queueFixture(t)
	oldDB, oldConfig := db.Instance, config.Instance
	db.Instance = engine
	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com"}}
	t.Cleanup(func() { db.Instance, config.Instance = oldDB, oldConfig })
	if err := engine.Sync2(&models.ProtocolCredential{}, &models.ClientCredential{}); err != nil {
		t.Fatal(err)
	}
	service, err := credentials.NewService(engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetProtocolEnabled(email.SendUserID, "smtp", true); err != nil {
		t.Fatal(err)
	}
	code, err := service.GenerateProtocolCode(email.SendUserID, "smtp")
	if err != nil {
		t.Fatal(err)
	}
	_, session, err := auth.AuthenticateProtocolSession("smtp", "192.0.2.95:1234", "sender@example.com", code)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := session.QueueAuthorization()
	if err != nil {
		t.Fatal(err)
	}
	submission, err := json.Marshal(Submission{
		EnvelopeFrom: "sender@example.com", Date: "2026-09-27T12:30:00+08:00",
		Trigger: "SMTP", Authorization: authorization,
	})
	if err != nil {
		t.Fatal(err)
	}
	email.Sender = `{"EmailAddress":"sender@example.com"}`
	if _, err := engine.ID(email.Id).Cols("sender").Update(&email); err != nil {
		t.Fatal(err)
	}
	for index := range jobs {
		jobs[index].Submission = string(submission)
		if _, err := engine.ID(jobs[index].ID).Cols("submission").Update(&jobs[index]); err != nil {
			t.Fatal(err)
		}
	}
	return engine, jobs, service
}

func TestSMTPQueueRestoresEnvelopeWithoutRewritingHeaders(t *testing.T) {
	engine, jobs, _ := smtpQueueFixture(t)
	calls := 0
	r := &Runtime{engine: engine, deliver: func(ctx *context.Context, message *parsemail.Email, recipient string) error {
		calls++
		if ctx.UserAccount != "sender" || recipient != jobs[0].Recipient || message.EnvelopeFrom != "sender@example.com" {
			t.Fatal("worker did not restore SMTP identity or envelope")
		}
		if len(message.EnvelopeTo) != 1 || message.EnvelopeTo[0].EmailAddress != recipient || len(message.To) != 2 {
			t.Fatal("envelope recipients leaked into message headers")
		}
		if message.MsgID != "stable@example.com" || message.Date != "2026-09-27T12:30:00+08:00" || message.DeliveryTrigger != "SMTP" {
			t.Fatal("worker lost immutable submission metadata")
		}
		return nil
	}}
	if err := r.process(jobs[0]); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("deliveries = %d, want 1", calls)
	}
}

func TestSMTPQueueRejectsStaleAuthorizationBeforeDelivery(t *testing.T) {
	for _, change := range []string{"revoke", "disable", "rotate", "malformed", "sender", "database_failure"} {
		t.Run(change, func(t *testing.T) {
			engine, jobs, service := smtpQueueFixture(t)
			var err error
			switch change {
			case "revoke":
				err = service.RevokeProtocolCode(1, "smtp")
			case "disable":
				err = service.SetProtocolEnabled(1, "smtp", false)
			case "rotate":
				_, err = service.GenerateProtocolCode(1, "smtp")
			case "malformed":
				jobs[0].Submission = "{"
			case "sender":
				var submission Submission
				if err := json.Unmarshal([]byte(jobs[0].Submission), &submission); err != nil {
					t.Fatal(err)
				}
				submission.EnvelopeFrom = "other@example.com"
				var encoded []byte
				encoded, err = json.Marshal(submission)
				jobs[0].Submission = string(encoded)
			case "database_failure":
				_, err = engine.Exec("ALTER TABLE protocol_credential RENAME TO unavailable_credential")
			}
			if err != nil {
				t.Fatal(err)
			}
			r := &Runtime{engine: engine, deliver: func(*context.Context, *parsemail.Email, string) error {
				t.Fatal("worker delivered without current authorization")
				return nil
			}}
			err = r.process(jobs[0])
			if (change == "database_failure") != (err != nil) {
				t.Fatalf("process error = %v", err)
			}
			var stored models.OutboundDelivery
			if found, err := engine.ID(jobs[0].ID).Get(&stored); err != nil || !found {
				t.Fatalf("read queued job: found=%v err=%v", found, err)
			}
			if change == "database_failure" {
				if stored.State != queued || stored.Attempts != 0 {
					t.Fatal("database read failure consumed a delivery attempt")
				}
			} else if stored.State != failed {
				t.Fatalf("revoked submission state = %s", stored.State)
			}
		})
	}
}

func assertStatus(t *testing.T, engine *xorm.Engine, emailID int, status int8) {
	t.Helper()
	var email models.Email
	var relation models.UserEmail
	if ok, err := engine.ID(emailID).Get(&email); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := engine.Where("email_id = ?", emailID).Get(&relation); err != nil || !ok {
		t.Fatal(err)
	}
	if email.Status != status || relation.Status != status {
		t.Fatalf("email=%d relation=%d want=%d", email.Status, relation.Status, status)
	}
}

func TestClaimAndPartialResults(t *testing.T) {
	engine, email, jobs := queueFixture(t)
	first := jobs[0]
	if ok, err := claim(engine, &first); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := claim(engine, &jobs[0]); err != nil || ok {
		t.Fatalf("duplicate claim: %v %v", ok, err)
	}
	if _, err := finish(engine, first, nil); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, engine, email.Id, consts.EmailStatusDeliveryPending)
	second := jobs[1]
	if ok, err := claim(engine, &second); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := finish(engine, second, &textproto.Error{Code: 451, Msg: "later"}); err != nil {
		t.Fatal(err)
	}
	var queuedJob models.OutboundDelivery
	if _, err := engine.ID(second.ID).Get(&queuedJob); err != nil {
		t.Fatal(err)
	}
	if queuedJob.State != queued || queuedJob.NextAttempt <= time.Now().Unix() {
		t.Fatalf("retry state=%+v", queuedJob)
	}
	if ok, err := claim(engine, &queuedJob); err != nil || ok {
		t.Fatal("retry was claimed before backoff elapsed")
	}
	if _, err := engine.ID(queuedJob.ID).Cols("next_attempt").Update(&models.OutboundDelivery{}); err != nil {
		t.Fatal(err)
	}
	if ok, err := claim(engine, &queuedJob); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := finish(engine, queuedJob, &textproto.Error{Code: 550, Msg: "rejected"}); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, engine, email.Id, consts.EmailStatusFail)
	if ok, err := claim(engine, &first); err != nil || ok {
		t.Fatal("accepted job was claimed again")
	}
}

func TestRestartDoesNotResendInterruptedOrAccepted(t *testing.T) {
	engine, email, jobs := queueFixture(t)
	for i := range jobs {
		if ok, err := claim(engine, &jobs[i]); err != nil || !ok {
			t.Fatal(err)
		}
	}
	if _, err := finish(engine, jobs[0], nil); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterrupted(engine); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, engine, email.Id, consts.EmailStatusDeliveryUncertain)
	var stored []models.OutboundDelivery
	if err := engine.Asc("id").Find(&stored); err != nil {
		t.Fatal(err)
	}
	if stored[0].State != accepted || stored[1].State != uncertain {
		t.Fatalf("states=%+v", stored)
	}
	if err := recoverInterrupted(engine); err != nil {
		t.Fatal(err)
	}
}

func TestUncertainDATAIsNeverRetried(t *testing.T) {
	engine, _, jobs := queueFixture(t)
	job := jobs[0]
	if ok, err := claim(engine, &job); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := finish(engine, job, &smtp.DeliveryUncertainError{Cause: io.EOF}); err != nil {
		t.Fatal(err)
	}
	if ok, err := claim(engine, &job); err != nil || ok {
		t.Fatal("uncertain job was claimed again")
	}
}

func TestResultFailureRollsBackAndNeverRedelivers(t *testing.T) {
	engine, email, jobs := queueFixture(t)
	if _, err := engine.Exec("CREATE TRIGGER reject_result BEFORE UPDATE ON user_email BEGIN SELECT RAISE(ABORT, 'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r := &Runtime{engine: engine, deliver: func(_ *context.Context, e *parsemail.Email, recipient string) error {
		calls.Add(1)
		if e.MsgID != "stable@example.com" || len(e.To) != 2 || recipient != "one@example.com" {
			t.Fatal("envelope changed message headers")
		}
		return nil
	}}
	if err := r.process(jobs[0]); err == nil {
		t.Fatal("result failure was ignored")
	}
	if err := r.tick(); err == nil {
		t.Fatal("result retry failure was ignored")
	}
	if calls.Load() != 1 {
		t.Fatalf("SMTP called %d times", calls.Load())
	}
	assertStatus(t, engine, email.Id, consts.EmailStatusDeliveryPending)
	var job models.OutboundDelivery
	if _, err := engine.ID(jobs[0].ID).Get(&job); err != nil {
		t.Fatal(err)
	}
	if job.State != running {
		t.Fatalf("state=%s", job.State)
	}
	if _, err := engine.Exec("DROP TRIGGER reject_result"); err != nil {
		t.Fatal(err)
	}
	if err := r.tick(); err != nil {
		t.Fatal(err)
	}
	if r.pending != nil || calls.Load() != 1 {
		t.Fatal("result was not recovered without another SMTP attempt")
	}
	var persisted models.OutboundDelivery
	if found, err := engine.ID(job.ID).Get(&persisted); err != nil || !found || persisted.State != accepted {
		t.Fatalf("result not persisted: %+v %v", persisted, err)
	}
}

func TestPersistResultAfterLostCommitResponse(t *testing.T) {
	for _, deliveryErr := range []error{nil, &textproto.Error{Code: 451, Msg: "later"}, &smtp.DeliveryUncertainError{Cause: io.EOF}} {
		engine, _, jobs := queueFixture(t)
		job := jobs[0]
		if ok, err := claim(engine, &job); err != nil || !ok {
			t.Fatal(err)
		}
		result := deliveryResult(job, deliveryErr)
		if _, err := persistResult(engine, result); err != nil {
			t.Fatal(err)
		}
		if _, err := persistResult(engine, result); err != nil {
			t.Fatalf("same result must be idempotent: %v", err)
		}
		result.Attempts++
		if _, err := persistResult(engine, result); err == nil {
			t.Fatal("different attempt must not overwrite persisted result")
		}
	}
}

func TestPendingResultStopsNewClaimsAndSurvivesRestartSafely(t *testing.T) {
	engine, _, jobs := queueFixture(t)
	if _, err := engine.Exec("CREATE TRIGGER reject_result BEFORE UPDATE ON user_email BEGIN SELECT RAISE(ABORT, 'injected failure'); END"); err != nil {
		t.Fatal(err)
	}
	var calls int
	r := &Runtime{engine: engine, deliver: func(_ *context.Context, _ *parsemail.Email, _ string) error {
		calls++
		return nil
	}}
	for i := 0; i < 4; i++ {
		if err := r.tick(); err == nil {
			t.Fatal("persistence failure ignored")
		}
	}
	var second models.OutboundDelivery
	if _, err := engine.ID(jobs[1].ID).Get(&second); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || second.State != queued || second.Attempts != 0 {
		t.Fatalf("new delivery started during outage: calls=%d job=%+v", calls, second)
	}
	if _, err := engine.Exec("DROP TRIGGER reject_result"); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterrupted(engine); err != nil {
		t.Fatal(err)
	}
	var first models.OutboundDelivery
	if _, err := engine.ID(jobs[0].ID).Get(&first); err != nil || first.State != uncertain {
		t.Fatalf("lost in-memory result must remain uncertain: %+v %v", first, err)
	}
	if _, err := persistResult(engine, r.pending.job); err == nil {
		t.Fatal("stale runtime overwrote restart recovery")
	}
}

func TestQueuedJobsSurviveRestartAndDrain(t *testing.T) {
	engine, email, _ := queueFixture(t)
	if err := recoverInterrupted(engine); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r := &Runtime{engine: engine, stop: make(chan struct{}), done: make(chan struct{}), deliver: func(_ *context.Context, _ *parsemail.Email, _ string) error { calls.Add(1); return nil }}
	go r.loop()
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d", calls.Load())
	}
	assertStatus(t, engine, email.Id, consts.EmailStatusSent)
}

func TestRetryLimitAndSafeFailureDetails(t *testing.T) {
	engine, _, jobs := queueFixture(t)
	job := jobs[0]
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if _, err := engine.ID(job.ID).Cols("next_attempt").Update(&models.OutboundDelivery{}); err != nil {
			t.Fatal(err)
		}
		if ok, err := claim(engine, &job); err != nil || !ok {
			t.Fatalf("claim %d: %v", attempt, err)
		}
		before := time.Now().Unix()
		if _, err := finish(engine, job, &textproto.Error{Code: 451, Msg: "sensitive-remote-response"}); err != nil {
			t.Fatal(err)
		}
		var refreshed models.OutboundDelivery
		if found, err := engine.ID(job.ID).Get(&refreshed); err != nil || !found {
			t.Fatal(err)
		}
		job = refreshed
		if strings.Contains(job.LastError, "sensitive") || !strings.Contains(job.LastError, "451") {
			t.Fatalf("unsafe or missing failure details: %s", job.LastError)
		}
		if attempt < maxAttempts {
			minimum := before + int64(60*(1<<(attempt-1)))
			if job.State != queued || job.NextAttempt < minimum || job.NextAttempt > minimum+2 {
				t.Fatalf("unexpected backoff: %+v", job)
			}
		} else if job.State != failed || job.NextAttempt != 0 {
			t.Fatalf("retry limit ignored: %+v", job)
		}
	}
	status, message, terminal := summarize([]models.OutboundDelivery{job})
	if status != consts.EmailStatusFail || !terminal || !strings.Contains(message, "451") {
		t.Fatalf("failure summary: %d %s %v", status, message, terminal)
	}
}

func TestLegacyPendingIsUncertainWithoutEnqueue(t *testing.T) {
	engine, _, _ := queueFixture(t)
	email := models.Email{Type: consts.EmailTypeSend, Status: consts.EmailStatusDeliveryPending}
	if _, err := engine.Insert(&email); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&models.UserEmail{UserID: 1, EmailID: email.Id, Status: consts.EmailStatusDeliveryPending}); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterrupted(engine); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, engine, email.Id, consts.EmailStatusDeliveryUncertain)
	count, err := engine.Where("email_id = ?", email.Id).Count(&models.OutboundDelivery{})
	if err != nil || count != 0 {
		t.Fatalf("legacy message enqueued: %d %v", count, err)
	}
}

func TestReadFailureDoesNotClaimOrDeliver(t *testing.T) {
	engine, _, jobs := queueFixture(t)
	if _, err := engine.Exec("ALTER TABLE email RENAME TO unavailable_email"); err != nil {
		t.Fatal(err)
	}
	r := &Runtime{engine: engine, deliver: func(_ *context.Context, _ *parsemail.Email, _ string) error {
		t.Fatal("SMTP called despite read failure")
		return nil
	}}
	if err := r.process(jobs[0]); err == nil {
		t.Fatal("read failure ignored")
	}
	var job models.OutboundDelivery
	if _, err := engine.ID(jobs[0].ID).Get(&job); err != nil {
		t.Fatal(err)
	}
	if job.State != queued || job.Attempts != 0 {
		t.Fatalf("read failure consumed attempt: %+v", job)
	}
}

func TestStopTimeoutDoesNotStartAnotherDelivery(t *testing.T) {
	engine, _, _ := queueFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r := &Runtime{engine: engine, stop: make(chan struct{}), done: make(chan struct{}), deliver: func(_ *context.Context, _ *parsemail.Email, _ string) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return nil
	}}
	go r.loop()
	<-entered
	ctx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 20*time.Millisecond)
	defer cancel()
	err := r.Stop(ctx)
	close(release)
	if !errors.Is(err, stdcontext.DeadlineExceeded) {
		t.Fatalf("stop did not time out: %v", err)
	}
	if err := r.Stop(stdcontext.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("delivery started after stop: %d", calls.Load())
	}
}

func TestDeliveryDoesNotRestoreTrashedRelation(t *testing.T) {
	engine, email, jobs := queueFixture(t)
	if _, err := engine.Where("email_id = ?", email.Id).Cols("status").Update(&models.UserEmail{Status: consts.EmailStatusDel}); err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		if ok, err := claim(engine, &job); err != nil || !ok {
			t.Fatal(err)
		}
		if _, err := finish(engine, job, nil); err != nil {
			t.Fatal(err)
		}
	}
	var relation models.UserEmail
	if _, err := engine.Where("email_id = ?", email.Id).Get(&relation); err != nil {
		t.Fatal(err)
	}
	if relation.Status != consts.EmailStatusDel {
		t.Fatal("delivery restored trashed message")
	}
}
