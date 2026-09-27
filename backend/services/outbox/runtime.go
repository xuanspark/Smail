package outbox

import (
	stdcontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/dbmigration"
	"git.pcux.cn/xuans/Smail/services/notification"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/send"
	"git.pcux.cn/xuans/Smail/utils/smtp"
	"git.pcux.cn/xuans/Smail/utils/validation"
	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
)

type Runtime struct {
	engine  *xorm.Engine
	stop    chan struct{}
	done    chan struct{}
	once    sync.Once
	deliver func(*context.Context, *parsemail.Email, string) error
	pending *pendingResult
}

type pendingResult struct {
	job     models.OutboundDelivery
	ctx     *context.Context
	message *parsemail.Email
}

func Start(engine *xorm.Engine) (*Runtime, error) {
	if err := recoverInterrupted(engine); err != nil {
		return nil, err
	}
	r := &Runtime{engine: engine, stop: make(chan struct{}), done: make(chan struct{}), deliver: send.SendRecipientTriggered}
	go r.loop()
	return r, nil
}

func (r *Runtime) Stop(ctx stdcontext.Context) error {
	if r == nil {
		return nil
	}
	r.once.Do(func() { close(r.stop) })
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) loop() {
	defer close(r.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		default:
		}
		if err := r.tick(); err != nil {
			log.WithError(err).Error("处理持久投递队列失败")
		}
		select {
		case <-r.stop:
			return
		case <-ticker.C:
		}
	}
}

func (r *Runtime) tick() error {
	// 单工作器最多保留一个未落库结果，恢复前不领取新任务，避免内存积压和重复投递。
	if r.pending != nil {
		return dbmigration.WhileWritable(r.flushResult)
	}
	var jobs []models.OutboundDelivery
	if err := r.engine.Where("state = ? AND next_attempt <= ?", queued, time.Now().Unix()).Asc("id").Limit(20).Find(&jobs); err != nil {
		return err
	}
	for _, job := range jobs {
		select {
		case <-r.stop:
			return nil
		default:
		}
		if err := r.process(job); err != nil {
			return fmt.Errorf("投递任务 %d: %w", job.ID, err)
		}
	}
	return nil
}

func (r *Runtime) process(job models.OutboundDelivery) error {
	// 迁移必须等待 SMTP 及结果落库结束，冻结源库后不再领取任务。
	return dbmigration.WhileWritable(func() error { return r.processWritable(job) })
}

func (r *Runtime) processWritable(job models.OutboundDelivery) error {
	if r.pending != nil {
		return r.flushResult()
	}
	// 读取失败时尚未开始投递，保留排队状态供数据库恢复后继续处理。
	var stored models.Email
	emailFound, err := r.engine.ID(job.EmailID).Get(&stored)
	if err != nil {
		return err
	}
	var user models.User
	userFound := false
	if emailFound {
		userFound, err = r.engine.ID(stored.SendUserID).Get(&user)
		if err != nil {
			return err
		}
	}
	var submission Submission
	authorized := true
	if job.Submission != "" {
		authorized = userFound && json.Unmarshal([]byte(job.Submission), &submission) == nil
		if authorized {
			authorized, err = submission.Authorization.Check(r.engine, user)
			if err != nil {
				return err
			}
		}
	}
	owned, err := claim(r.engine, &job)
	if err != nil || !owned {
		return err
	}
	ctx := &context.Context{Context: stdcontext.Background()}
	var message *parsemail.Email
	deliveryErr := func() (result error) {
		defer func() {
			if value := recover(); value != nil {
				result = &smtp.DeliveryUncertainError{Cause: fmt.Errorf("投递任务异常: %v", value)}
			}
		}()
		if !emailFound {
			return errors.New("待投递邮件已不存在")
		}
		if !userFound || user.Disabled != 0 {
			return errors.New("发件账号不可用，已停止投递")
		}
		ctx.UserID, ctx.UserAccount, ctx.UserName, ctx.Role = user.ID, user.Account, user.Name, user.EffectiveRole()
		message, err = parsemail.NewEmailFromModel(stored)
		if err != nil {
			return err
		}
		if job.Submission != "" {
			if !authorized || !authorizedQueuedSender(submission, message, user) {
				return notification.ErrDenied
			}
			message.EnvelopeFrom = submission.EnvelopeFrom
			if submission.Date != "" {
				message.Date = submission.Date
			}
			message.EnvelopeTo = []*parsemail.User{{EmailAddress: job.Recipient}}
			message.DeliveryTrigger = submission.Trigger
		}
		return r.deliver(ctx, message, job.Recipient)
	}()
	// 固定首次结果及退避时间，后续只重试落库，绝不再次调用 SMTP。
	r.pending = &pendingResult{job: deliveryResult(job, deliveryErr), ctx: ctx, message: message}
	return r.flushResult()
}

func authorizedQueuedSender(submission Submission, message *parsemail.Email, user models.User) bool {
	if message == nil || message.From == nil || message.Sender == nil || submission.Trigger == "" {
		return false
	}
	domains := config.CurrentMailDomains()
	if submission.Authorization.Kind == "notification" {
		sender := validation.NotificationAddress(domains.Primary)
		return sender != "" && strings.EqualFold(submission.EnvelopeFrom, sender) &&
			strings.EqualFold(message.From.EmailAddress, sender) && strings.EqualFold(message.Sender.EmailAddress, sender)
	}
	allowAlias := user.EffectiveRole() == access.RoleSuperAdmin
	return validation.IsAuthorizedSender(message.From.EmailAddress, user.Account, allowAlias, domains.Accepted) &&
		validation.IsAuthorizedSender(message.Sender.EmailAddress, user.Account, allowAlias, domains.Accepted) &&
		(submission.EnvelopeFrom == "" || validation.IsAuthorizedSender(submission.EnvelopeFrom, user.Account, allowAlias, domains.Accepted))
}

func (r *Runtime) flushResult() error {
	result := r.pending
	if result == nil {
		return nil
	}
	terminal, err := persistResult(r.engine, result.job)
	if err != nil {
		return fmt.Errorf("投递任务 %d 结果写入失败，已暂停领取任务并等待数据库恢复: %w", result.job.ID, err)
	}
	r.pending = nil
	if terminal && result.message != nil {
		r.notify(result.ctx, result.message, result.job.EmailID)
	}
	return nil
}

func (r *Runtime) notify(ctx *context.Context, message *parsemail.Email, emailID int) {
	var jobs []models.OutboundDelivery
	if err := r.engine.Where("email_id = ?", emailID).Find(&jobs); err != nil {
		log.WithError(err).Error("读取投递结果用于插件通知失败")
		return
	}
	failures := make(map[string]error)
	for _, job := range jobs {
		if job.State != accepted {
			failures[job.Recipient] = errors.New(job.LastError)
		}
	}
	for _, hook := range hooks.Snapshot() {
		if hook == nil {
			continue
		}
		func() {
			defer func() {
				if recover() != nil {
					log.Error("投递完成插件发生异常")
				}
			}()
			hook.SendAfter(ctx, message, failures)
		}()
	}
}
