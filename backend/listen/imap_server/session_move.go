package imap_server

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

func (s *serverSession) Move(w *imapserver.MoveWriter, numSet imap.NumSet, dest string) error {
	var emailList []*response.EmailResponseData

	switch numSet.(type) {
	case imap.SeqSet:
		seqSet := numSet.(imap.SeqSet)
		for _, seq := range seqSet {
			res, err := list.GetEmailListByGroup(s.ctx, s.currentMailbox, list.ImapListReq{
				Star: cast.ToInt(seq.Start),
				End:  cast.ToInt(seq.Stop),
			}, false)
			if err != nil {
				log.WithContext(s.ctx).WithError(err).Error("query IMAP messages for move failed")
				return moveFailedError()
			}
			emailList = append(emailList, res...)
		}
	case imap.UIDSet:
		uidSet := numSet.(imap.UIDSet)
		for _, uid := range uidSet {
			res, err := list.GetEmailListByGroup(s.ctx, s.currentMailbox, list.ImapListReq{
				Star: cast.ToInt(uint32(uid.Start)),
				End:  cast.ToInt(uint32(uid.Stop)),
			}, true)
			if err != nil {
				log.WithContext(s.ctx).WithError(err).Error("query IMAP messages for UID move failed")
				return moveFailedError()
			}
			emailList = append(emailList, res...)
		}
	}

	relationIDs := make([]int, 0, len(emailList))
	for _, email := range emailList {
		if email.UeId > 0 {
			relationIDs = append(relationIDs, email.UeId)
		}
	}

	if len(relationIDs) == 0 {
		return nil
	}

	if group.IsDefaultBox(dest) {
		return move2defaultbox(s.ctx, relationIDs, dest)
	}
	return move2userbox(s.ctx, relationIDs, dest)
}

func move2defaultbox(ctx *context.Context, relationIDs []int, dest string) error {
	if err := group.MoveRelationsToDefaultBox(ctx, relationIDs, dest); err != nil {
		log.WithContext(ctx).WithError(err).Error("move messages to default mailbox failed")
		return moveFailedError()
	}
	return nil
}

func move2userbox(ctx *context.Context, relationIDs []int, dest string) error {
	groupInfo, err := group.GetGroupByFullPath(ctx, dest)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("resolve IMAP move destination failed")
		return moveFailedError()
	}
	if groupInfo == nil || groupInfo.ID == 0 {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Mailbox not found"}
	}
	if err := group.MoveRelationsToGroup(ctx, relationIDs, groupInfo.ID); err != nil {
		log.WithContext(ctx).Error("move messages to user mailbox failed")
		return moveFailedError()
	}
	return nil
}

func moveFailedError() error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Move failed"}
}
