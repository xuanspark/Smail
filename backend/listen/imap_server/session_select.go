package imap_server

import (
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/services/list"
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
	readOnly := options != nil && options.ReadOnly
	_, data, err := group.GetGroupStatus(s.ctx, mailbox, []string{"MESSAGES", "UNSEEN", "UIDNEXT", "UIDVALIDITY"})
	if err != nil {
		log.WithContext(s.ctx).WithError(err).Error("query selected mailbox status failed")
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Select failed"}
	}
	relations, err := list.GetUEListByUID(s.ctx, mailbox, 0, 0, nil)
	if err != nil {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Select failed"}
	}
	numMessages, err := imapUint32(len(relations))
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
	s.mailboxMu.Lock()
	s.currentMailbox = mailbox
	s.readOnly = readOnly
	s.mailboxUIDs = make([]int, 0, len(relations))
	for _, relation := range relations {
		s.mailboxUIDs = append(s.mailboxUIDs, relation.ID)
	}
	s.mailboxMu.Unlock()

	ret := &imap.SelectData{
		Flags:          []imap.Flag{imap.FlagSeen, imap.FlagDeleted},
		PermanentFlags: []imap.Flag{imap.FlagSeen, imap.FlagDeleted},
		NumMessages:    numMessages,
		UIDNext:        uidNext,
		UIDValidity:    uidValidity,
	}
	if readOnly {
		ret.PermanentFlags = nil
	}

	return ret, nil

}
