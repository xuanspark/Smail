package imap_server

import (
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

func (s *serverSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	var ue []models.UserEmail
	if err := db.Instance.Table("user_email").Where("user_id=? and `create` >=?", s.ctx.UserID, s.connectTime).Find(&ue); err != nil {
		log.WithContext(s.ctx).WithError(err).Error("query IMAP polling updates failed")
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Poll failed"}
	}

	if len(ue) > 0 {
		if err := w.WriteNumMessages(cast.ToUint32(len(ue))); err != nil {
			log.WithContext(s.ctx).WithError(err).Error("write IMAP polling update failed")
			return err
		}
	}

	return nil
}
