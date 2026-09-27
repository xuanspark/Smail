package outbox

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/utils/send"
	"git.pcux.cn/xuans/Smail/utils/smtp"
	"xorm.io/xorm"
)

const (
	queued           = "queued"
	running          = "running"
	accepted         = "accepted"
	failed           = "failed"
	uncertain        = "uncertain"
	maxAttempts      = 5
	uncertainMessage = "投递结果不确定，已暂停自动重试，请先核对收件结果"
)

// Enqueue 必须与邮件及用户关联在同一事务内保存，不保存验证码或恢复令牌。
func Enqueue(session *xorm.Session, email models.Email) error {
	message, err := parsemail.NewEmailFromModel(email)
	if err != nil {
		return err
	}
	return enqueueRecipients(session, email, message, "")
}

type Submission struct {
	EnvelopeFrom  string                   `json:"envelope_from"`
	Date          string                   `json:"date"`
	Trigger       string                   `json:"trigger"`
	Authorization auth.QueuedAuthorization `json:"authorization"`
}

func EnqueueSMTP(session *xorm.Session, email models.Email, recipients []string, submission Submission) error {
	if submission.Authorization.Kind == "" || submission.Authorization.ID <= 0 || submission.Authorization.UserID != email.SendUserID || submission.Trigger == "" {
		return errors.New("SMTP submission authorization is missing")
	}
	encoded, err := json.Marshal(submission)
	if err != nil {
		return err
	}
	message := &parsemail.Email{To: make([]*parsemail.User, len(recipients))}
	for index, address := range recipients {
		message.To[index] = &parsemail.User{EmailAddress: address}
	}
	return enqueueRecipients(session, email, message, string(encoded))
}

func enqueueRecipients(session *xorm.Session, email models.Email, message *parsemail.Email, submission string) error {
	seen := make(map[string]bool)
	for _, user := range append(append(message.To, message.Cc...), message.Bcc...) {
		if user == nil || user.EmailAddress == "" {
			return errors.New("队列收件人不能为空")
		}
		// 本地部分保留大小写，仅合并完全相同的 SMTP 信封地址。
		address := user.EmailAddress
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || len(address) > 320 {
			return errors.New("invalid queue recipient")
		}
		if at := strings.LastIndexByte(address, '@'); at >= 0 {
			address = address[:at+1] + strings.ToLower(address[at+1:])
		}
		if seen[address] {
			continue
		}
		seen[address] = true
		job := models.OutboundDelivery{EmailID: email.Id, Recipient: address, State: queued, Submission: submission}
		if _, err := session.Insert(&job); err != nil {
			return err
		}
	}
	if len(seen) == 0 {
		return errors.New("队列必须包含收件人")
	}
	return nil
}

func summarize(jobs []models.OutboundDelivery) (int8, string, bool) {
	counts := make(map[string]int)
	reasons := make(map[string]bool)
	for _, job := range jobs {
		counts[job.State]++
		if job.LastError != "" {
			reasons[job.LastError] = true
		}
	}
	if counts[queued]+counts[running] > 0 {
		return consts.EmailStatusDeliveryPending, "", false
	}
	details := make([]string, 0, len(reasons))
	for reason := range reasons {
		details = append(details, reason)
	}
	sort.Strings(details)
	suffix := ""
	if len(details) > 0 {
		suffix = "；原因：" + strings.Join(details, "；")
	}
	if counts[uncertain] > 0 {
		return consts.EmailStatusDeliveryUncertain, fmt.Sprintf("%s；已接收 %d，结果不确定 %d，失败 %d", uncertainMessage, counts[accepted], counts[uncertain], counts[failed]) + suffix, true
	}
	if len(jobs) > 0 && counts[accepted] == len(jobs) {
		return consts.EmailStatusSent, "", true
	}
	return consts.EmailStatusFail, fmt.Sprintf("投递未全部完成；已接收 %d，失败 %d。已接收的收件人不会自动重投", counts[accepted], counts[failed]) + suffix, true
}

func syncStatus(session *xorm.Session, emailID int) (bool, error) {
	var jobs []models.OutboundDelivery
	if err := session.Where("email_id = ?", emailID).Find(&jobs); err != nil {
		return false, err
	}
	status, message, terminal := summarize(jobs)
	if _, err := session.ID(emailID).Cols("status", "error").Update(&models.Email{Status: status, Error: sql.NullString{String: message, Valid: true}}); err != nil {
		return false, err
	}
	// 不改动用户已放入回收站的关联状态。
	_, err := session.Where("email_id = ? AND status <> ?", emailID, consts.EmailStatusDel).Cols("status").Update(&models.UserEmail{Status: status})
	return terminal, err
}

// recoverInterrupted 只在独占运行实例启动时执行，在途记录绝不重新排队。
func recoverInterrupted(engine *xorm.Engine) error {
	return db.Transaction(engine, func(session *xorm.Session) error {
		// 旧版本未记录 SMTP 尝试边界，只标记待核对，不能当作未发送任务导入。
		var legacy []models.Email
		if err := session.Where("type = ? AND status = ? AND id NOT IN (SELECT email_id FROM outbound_delivery)", consts.EmailTypeSend, consts.EmailStatusDeliveryPending).Find(&legacy); err != nil {
			return err
		}
		for _, email := range legacy {
			if _, err := session.ID(email.Id).Cols("status", "error").Update(&models.Email{Status: consts.EmailStatusDeliveryUncertain, Error: sql.NullString{String: uncertainMessage, Valid: true}}); err != nil {
				return err
			}
			if _, err := session.Where("email_id = ? AND status = ?", email.Id, consts.EmailStatusDeliveryPending).Cols("status").Update(&models.UserEmail{Status: consts.EmailStatusDeliveryUncertain}); err != nil {
				return err
			}
		}
		var interrupted []models.OutboundDelivery
		if err := session.Where("state = ?", running).Find(&interrupted); err != nil {
			return err
		}
		if _, err := session.Where("state = ?", running).Cols("state", "last_error").Update(&models.OutboundDelivery{State: uncertain, LastError: uncertainMessage}); err != nil {
			return err
		}
		seen := make(map[int]bool)
		for _, job := range interrupted {
			if !seen[job.EmailID] {
				if _, err := syncStatus(session, job.EmailID); err != nil {
					return err
				}
				seen[job.EmailID] = true
			}
		}
		return nil
	})
}

func claim(engine *xorm.Engine, job *models.OutboundDelivery) (bool, error) {
	update := *job
	update.State = running
	update.Attempts++
	count, err := engine.ID(job.ID).Where("state = ? AND attempts = ? AND next_attempt <= ?", queued, job.Attempts, time.Now().Unix()).Cols("state", "attempts").Update(&update)
	if err == nil && count == 1 {
		*job = update
	}
	return count == 1, err
}

func finish(engine *xorm.Engine, job models.OutboundDelivery, deliveryErr error) (bool, error) {
	return persistResult(engine, deliveryResult(job, deliveryErr))
}

func deliveryResult(job models.OutboundDelivery, deliveryErr error) models.OutboundDelivery {
	job.State, job.LastError, job.NextAttempt = accepted, "", 0
	if deliveryErr != nil {
		// 不保存远端响应原文，避免凭据或恶意超长内容进入数据库和页面。
		job.State, job.LastError = failed, logstore.MailFailureReason(deliveryErr)
		if smtp.IsDeliveryUncertain(deliveryErr) {
			job.State = uncertain
			job.LastError = uncertainMessage
		} else if send.Retryable(deliveryErr) && job.Attempts < maxAttempts {
			job.State = queued
			job.NextAttempt = time.Now().Add(time.Minute * time.Duration(1<<(job.Attempts-1))).Unix()
		}
	}
	return job
}

func persistResult(engine *xorm.Engine, job models.OutboundDelivery) (bool, error) {
	terminal := false
	err := db.Transaction(engine, func(session *xorm.Session) error {
		count, err := session.ID(job.ID).Where("state = ? AND attempts = ?", running, job.Attempts).Cols("state", "last_error", "next_attempt").Update(&job)
		if err != nil {
			return err
		}
		if count != 1 {
			// 提交响应丢失时，确认同一次尝试的相同结果已落库，不覆盖后续尝试。
			var stored models.OutboundDelivery
			found, readErr := session.ID(job.ID).Get(&stored)
			if readErr != nil {
				return readErr
			}
			if !found || stored.EmailID != job.EmailID || stored.Recipient != job.Recipient ||
				stored.Attempts != job.Attempts || stored.State != job.State ||
				stored.LastError != job.LastError || stored.NextAttempt != job.NextAttempt {
				return errors.New("投递任务状态已发生变化")
			}
		}
		terminal, err = syncStatus(session, job.EmailID)
		return err
	})
	return terminal, err
}
