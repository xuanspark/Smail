package imap_server

import (
	"git.pcux.cn/xuans/Smail/services/group"
	"github.com/emersion/go-imap/v2"
	log "github.com/sirupsen/logrus"
	"strings"
)

func (s *serverSession) Rename(mailbox, newName string) error {
	if group.IsDefaultBox(mailbox) {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "This mailbox does not support rename.",
		}
	}

	newGroupPath := strings.Split(newName, "/")

	newGroupName := newGroupPath[len(newGroupPath)-1]

	err := group.Rename(s.ctx, mailbox, newGroupName)

	if err != nil {
		log.WithContext(s.ctx).WithError(err).Error("rename IMAP mailbox failed")
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "Rename failed",
		}
	}
	return nil
}
