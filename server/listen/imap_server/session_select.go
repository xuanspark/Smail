package imap_server

import (
	"git.pcux.cn/xuans/Smail/services/group"
	"github.com/emersion/go-imap/v2"
	log "github.com/sirupsen/logrus"
	"strings"
)

func (s *serverSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	if "" == mailbox {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeBad,
			Text: "mailbox not found",
		}
	}

	mailbox = strings.Trim(mailbox, `"`)
	_, data, err := group.GetGroupStatus(s.ctx, mailbox, []string{"MESSAGES", "UNSEEN", "UIDNEXT", "UIDVALIDITY"})
	if err != nil {
		log.WithContext(s.ctx).WithError(err).Error("query selected mailbox status failed")
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Select failed"}
	}
	s.currentMailbox = mailbox

	numMessages, err := imapUint32(data["MESSAGES"])
	if err != nil {
		return nil, err
	}
	uidNext, err := imapUID(data["UIDNEXT"])
	if err != nil {
		return nil, err
	}
	uidValidity, err := imapUint32(data["UIDVALIDITY"])
	if err != nil {
		return nil, err
	}

	ret := &imap.SelectData{
		Flags:          []imap.Flag{imap.FlagSeen},
		PermanentFlags: []imap.Flag{imap.FlagSeen},
		NumMessages:    numMessages,
		UIDNext:        uidNext,
		UIDValidity:    uidValidity,
	}

	return ret, nil

}
