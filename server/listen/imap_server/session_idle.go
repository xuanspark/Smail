package imap_server

import (
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"sync"
)

type idleConnection struct {
	writer  *imapserver.UpdateWriter
	mailbox string
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
	connects.writers[logId] = idleConnection{writer: w, mailbox: s.currentMailbox}
	connects.mu.Unlock()
	userConnectsMu.Unlock()

	go func() {
		<-stop

		userConnectsMu.Lock()
		connects.mu.Lock()
		delete(connects.writers, logId)
		if len(connects.writers) == 0 {
			userConnects.Delete(userId)
		}
		connects.mu.Unlock()
		userConnectsMu.Unlock()
	}()

	return nil
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
	idleConnects := make([]idleConnection, 0, len(connects.writers))
	for _, connect := range connects.writers {
		idleConnects = append(idleConnects, connect)
	}
	connects.mu.Unlock()
	userConnectsMu.Unlock()

	for _, connect := range idleConnects {
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
