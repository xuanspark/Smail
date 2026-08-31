package imap_server

import (
	"fmt"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/del_email"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/array"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

func (s *serverSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	if s == nil || s.ctx == nil || s.ctx.UserID <= 0 || flags == nil {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
	}

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
				log.WithContext(s.ctx).WithError(err).Error("query IMAP messages for store failed")
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
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
				log.WithContext(s.ctx).WithError(err).Error("query IMAP messages for UID store failed")
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
			}
			emailList = append(emailList, res...)
		}
	}

	seenRequested := array.InArray(imap.FlagSeen, flags.Flags)
	deletedRequested := array.InArray(imap.FlagDeleted, flags.Flags)
	updateReadState := false
	readState := false
	switch flags.Op {
	case imap.StoreFlagsSet:
		updateReadState = true
		readState = seenRequested
	case imap.StoreFlagsAdd:
		updateReadState = seenRequested
		readState = true
	case imap.StoreFlagsDel:
		updateReadState = seenRequested
		readState = false
	default:
		return &imap.Error{Type: imap.StatusResponseTypeBad, Text: "Unsupported STORE operation"}
	}

	if updateReadState {
		for _, data := range emailList {
			if err := updateReadByUID(s.ctx.UserID, data.UeId, readState); err != nil {
				log.WithContext(s.ctx).WithError(err).Error("update IMAP message read state failed")
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
			}
			data.IsRead = 0
			if readState {
				data.IsRead = 1
			}
		}
	}

	if deletedRequested && flags.Op == imap.StoreFlagsAdd {
		for _, data := range emailList {
			s.deleteUidList = append(s.deleteUidList, data.UeId)
		}
	}

	if len(s.deleteUidList) > 0 {
		err := del_email.DelByUID(s.ctx, s.deleteUidList)
		s.deleteUidList = []int{}
		if err != nil {
			log.WithContext(s.ctx).WithError(err).Error("delete IMAP message failed")
			return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
		}
	}

	if !flags.Silent && w != nil {
		for _, data := range emailList {
			sequence, err := imapUint32(data.SerialNumber)
			if err != nil || sequence == 0 {
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
			}
			message := w.CreateMessage(sequence)
			if data.IsRead == 1 {
				message.WriteFlags([]imap.Flag{imap.FlagSeen})
			} else {
				message.WriteFlags(nil)
			}
			if err := message.Close(); err != nil {
				return err
			}
		}
	}

	return nil
}

func updateReadByUID(userID, relationID int, read bool) error {
	if userID <= 0 || relationID <= 0 {
		return fmt.Errorf("invalid mailbox relation")
	}
	value := int8(0)
	if read {
		value = 1
	}
	affected, err := db.Instance.Where("id = ? and user_id = ?", relationID, userID).
		Cols("is_read").Update(&models.UserEmail{IsRead: value})
	if err != nil {
		return fmt.Errorf("update mailbox relation read state: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("mailbox relation not found")
	}
	return nil
}
