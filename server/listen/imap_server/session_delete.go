package imap_server

import (
	"git.pcux.cn/xuans/Smail/services/group"
	"github.com/emersion/go-imap/v2"
	log "github.com/sirupsen/logrus"
)

func (s *serverSession) Delete(mailbox string) error {
	groupInfo, err := group.GetGroupByPath(s.ctx, mailbox)
	if err != nil {
		log.WithContext(s.ctx).WithError(err).Error("query IMAP mailbox for deletion failed")
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Delete failed",
		}
	}
	if groupInfo.ID == 0 {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Mailbox not found",
		}
	}
	_, err = group.DelGroup(s.ctx, groupInfo.ID)
	if err != nil {
		log.WithContext(s.ctx).WithError(err).Error("delete IMAP mailbox failed")
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Delete failed",
		}
	}

	return nil
}
