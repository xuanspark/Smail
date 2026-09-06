package imap_server

import (
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"sync"
	"time"
)

type idleConnection struct {
	writer  *imapserver.UpdateWriter
	mailbox string
	session *serverSession
}

type idleConnections struct {
	mu      sync.Mutex
	writers map[string]idleConnection
}

var userConnects sync.Map
var userConnectsMu sync.Mutex

func (s *serverSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	userId := s.ctx.UserID
	logId := cast.ToString(s.ctx.GetValue(context.LogID))

	userConnectsMu.Lock()
	connectsAny, ok := userConnects.Load(userId)
	if !ok {
		connectsAny = &idleConnections{writers: map[string]idleConnection{}}
		userConnects.Store(userId, connectsAny)
	}
	connects := connectsAny.(*idleConnections)

	connects.mu.Lock()
	connects.writers[logId] = idleConnection{writer: w, mailbox: s.currentMailbox, session: s}
	connects.mu.Unlock()
	userConnectsMu.Unlock()

	defer func() {
		userConnectsMu.Lock()
		connects.mu.Lock()
		delete(connects.writers, logId)
		if len(connects.writers) == 0 {
			userConnects.Delete(userId)
		}
		connects.mu.Unlock()
		userConnectsMu.Unlock()
	}()
	if err := s.Poll(w, true); err != nil {
		return err
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-ticker.C:
			if err := s.Poll(w, true); err != nil {
				return err
			}
		}
	}
}

func IdleNotice(ctx *context.Context, userId int, email *models.Email) error {
	if userId == 0 || email == nil || email.Id == 0 {
		return nil
	}

	userConnectsMu.Lock()
	connectsAny, ok := userConnects.Load(userId)
	if !ok {
		userConnectsMu.Unlock()
		return nil
	}

	connects := connectsAny.(*idleConnections)
	connects.mu.Lock()
	userConnectsMu.Unlock()
	defer connects.mu.Unlock()

	for _, connect := range connects.writers {
		if connect.session != nil {
			// 同步变化和显式总数在同一锁内只写一次，避免投递时重复通知。
			if err := connect.session.poll(connect.writer, true, true); err != nil {
				return err
			}
			continue
		}
		numMessages, err := idleNumMessages(ctx, userId, connect.mailbox)
		if err != nil {
			return err
		}
		if err := connect.writer.WriteNumMessages(numMessages); err != nil {
			log.WithContext(ctx).WithError(err).Error("write IMAP IDLE update failed")
			return err
		}
	}
	return nil
}

func idleNumMessages(ctx *context.Context, userId int, mailbox string) (uint32, error) {
	if mailbox == "" {
		mailbox = "INBOX"
	}
	noticeCtx := *ctx
	noticeCtx.UserID = userId
	_, data, err := group.GetGroupStatus(&noticeCtx, mailbox, []string{"MESSAGES"})
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("query IMAP IDLE mailbox status failed")
		return 0, err
	}
	return cast.ToUint32(data["MESSAGES"]), nil
}
