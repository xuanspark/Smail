package imap_server

import (
	"git.pcux.cn/xuans/Smail/services/group"
	"github.com/emersion/go-imap/v2"
	log "github.com/sirupsen/logrus"
	"strings"
)

func (s *serverSession) Create(mailbox string, options *imap.CreateOptions) error {
	groupPath := strings.Split(mailbox, "/")

	var parentId int
	for _, path := range groupPath {
		newGroup, err := group.CreateGroup(s.ctx, path, parentId)
		if err != nil {
			log.WithContext(s.ctx).WithError(err).Error("create IMAP mailbox failed")
			return &imap.Error{
				Type: imap.StatusResponseTypeNo,
				Text: "Create failed",
			}
		}
		parentId = newGroup.ID
	}

	return nil
}
