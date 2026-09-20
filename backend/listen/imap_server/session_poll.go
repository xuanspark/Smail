package imap_server

import (
	"git.pcux.cn/xuans/Smail/services/list"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
)

func (s *serverSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	return s.poll(w, allowExpunge, false)
}

func (s *serverSession) poll(w *imapserver.UpdateWriter, allowExpunge, forceExists bool) error {
	if s.authorization != nil && s.authorization.Check() != nil {
		return protocolAuthorizationError()
	}
	s.mailboxMu.Lock()
	defer s.mailboxMu.Unlock()
	if s.currentMailbox == "" {
		return nil
	}
	if _, _, err := imapSnapshotBounds(s.mailboxUIDs); err != nil {
		return err
	}
	relations, err := list.GetUEListByUID(s.ctx, s.currentMailbox, 0, 0, nil)
	if err != nil {
		log.WithContext(s.ctx).WithError(err).Error("query IMAP polling updates failed")
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Poll failed"}
	}

	present := make(map[int]bool, len(relations))
	nextUIDs := make([]int, 0, len(relations))
	for _, relation := range relations {
		present[relation.ID] = true
		nextUIDs = append(nextUIDs, relation.ID)
	}
	_, count, err := imapSnapshotBounds(nextUIDs)
	if err != nil {
		return err
	}
	// 不允许 EXPUNGE 的命令后保留旧序号，等到可通知时再同步整个变化。
	for _, uid := range s.mailboxUIDs {
		if !present[uid] && !allowExpunge {
			return nil
		}
	}
	for index := len(s.mailboxUIDs) - 1; index >= 0; index-- {
		if !present[s.mailboxUIDs[index]] {
			sequence, err := imapUint32(index + 1)
			if err != nil {
				return err
			}
			if err := w.WriteExpunge(sequence); err != nil {
				return err
			}
			s.mailboxUIDs = append(s.mailboxUIDs[:index], s.mailboxUIDs[index+1:]...)
		}
	}
	if forceExists || len(relations) != len(s.mailboxUIDs) {
		if err := w.WriteNumMessages(count); err != nil {
			return err
		}
	}
	s.mailboxUIDs = nextUIDs
	return nil
}
