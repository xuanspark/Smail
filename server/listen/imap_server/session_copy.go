package imap_server

import (
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

func (s *serverSession) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
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
				log.WithContext(s.ctx).WithError(err).Error("query IMAP messages for copy failed")
				return nil, copyFailedError()
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
				log.WithContext(s.ctx).WithError(err).Error("query IMAP messages for UID copy failed")
				return nil, copyFailedError()
			}
			emailList = append(emailList, res...)
		}
	}

	if len(emailList) == 0 {
		return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Email not found"}
	}

	var (
		uidValidity int
		destUIDs    []int
		err         error
	)
	if group.IsDefaultBox(dest) {
		uidValidity, destUIDs, err = copy2defaultbox(s.ctx, emailList, dest)
	} else {
		uidValidity, destUIDs, err = copy2userbox(s.ctx, emailList, dest)
	}
	if err != nil {
		return nil, err
	}

	data := &imap.CopyData{
		UIDValidity: cast.ToUint32(uidValidity),
		DestUIDs:    imap.UIDSet{},
		SourceUIDs:  imap.UIDSet{},
	}
	for _, uid := range destUIDs {
		value := imap.UID(cast.ToUint32(uid))
		data.DestUIDs = append(data.DestUIDs, imap.UIDRange{Start: value, Stop: value})
	}
	for _, email := range emailList {
		value := imap.UID(cast.ToUint32(email.UeId))
		data.SourceUIDs = append(data.SourceUIDs, imap.UIDRange{Start: value, Stop: value})
	}

	return data, nil
}

func copy2defaultbox(ctx *context.Context, mails []*response.EmailResponseData, dest string) (int, []int, error) {
	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		log.WithContext(ctx).WithError(err).Error("begin default mailbox copy transaction failed")
		return 0, nil, copyFailedError()
	}

	destUIDs := make([]int, 0, len(mails))
	for _, email := range mails {
		newUe := models.UserEmail{
			UserID:  ctx.UserID,
			EmailID: email.Id,
			IsRead:  email.IsRead,
			GroupId: 0,
		}
		switch dest {
		case "Deleted Messages":
			newUe.Status = consts.EmailStatusDel
		case "INBOX":
			newUe.Status = consts.EmailStatusWait
		case "Sent Messages":
			newUe.Status = consts.EmailStatusSent
		case "Drafts":
			newUe.Status = consts.EmailStatusDrafts
		case "Junk":
			newUe.Status = consts.EmailStatusJunk
		default:
			_ = session.Rollback()
			return 0, nil, copyFailedError()
		}
		if _, err := session.Insert(&newUe); err != nil {
			_ = session.Rollback()
			log.WithContext(ctx).WithError(err).Error("insert default mailbox copy failed")
			return 0, nil, copyFailedError()
		}
		destUIDs = append(destUIDs, newUe.ID)
	}
	if err := session.Commit(); err != nil {
		log.WithContext(ctx).WithError(err).Error("commit default mailbox copy failed")
		return 0, nil, copyFailedError()
	}

	return models.GroupNameToCode[dest], destUIDs, nil
}

func copy2userbox(ctx *context.Context, mails []*response.EmailResponseData, dest string) (int, []int, error) {
	groupInfo, err := group.GetGroupByFullPath(ctx, dest)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("resolve user mailbox copy destination failed")
		return 0, nil, copyFailedError()
	}
	if groupInfo == nil || groupInfo.ID == 0 {
		return 0, nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Mailbox not found"}
	}

	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		log.WithContext(ctx).WithError(err).Error("begin user mailbox copy transaction failed")
		return 0, nil, copyFailedError()
	}

	destUIDs := make([]int, 0, len(mails))
	for _, email := range mails {
		newUe := models.UserEmail{
			UserID:  ctx.UserID,
			EmailID: email.Id,
			IsRead:  email.IsRead,
			GroupId: groupInfo.ID,
			Status:  email.Status,
		}
		if _, err := session.Insert(&newUe); err != nil {
			_ = session.Rollback()
			log.WithContext(ctx).WithError(err).Error("insert user mailbox copy failed")
			return 0, nil, copyFailedError()
		}
		destUIDs = append(destUIDs, newUe.ID)
	}
	if err := session.Commit(); err != nil {
		log.WithContext(ctx).WithError(err).Error("commit user mailbox copy failed")
		return 0, nil, copyFailedError()
	}

	return groupInfo.ID, destUIDs, nil
}

func copyFailedError() error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Copy failed"}
}
