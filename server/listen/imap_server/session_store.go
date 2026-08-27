package imap_server

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/del_email"
	"git.pcux.cn/xuans/Smail/services/detail"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/array"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

func (s *serverSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {

	if flags.Op == imap.StoreFlagsSet {
		return nil
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

	if array.InArray(imap.FlagSeen, flags.Flags) && flags.Op == imap.StoreFlagsAdd {
		for _, data := range emailList {
			if err := detail.MakeRead(s.ctx, data.Id, flags.Op == imap.StoreFlagsAdd); err != nil {
				log.WithContext(s.ctx).WithError(err).Error("update IMAP message read state failed")
				return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
			}
		}
	}

	if array.InArray(imap.FlagDeleted, flags.Flags) && flags.Op == imap.StoreFlagsAdd {
		for _, data := range emailList {
			s.deleteUidList = append(s.deleteUidList, data.UeId)
		}
	}

	err := del_email.DelByUID(s.ctx, s.deleteUidList)
	s.deleteUidList = []int{}

	if err != nil {
		log.WithContext(s.ctx).WithError(err).Error("delete IMAP message failed")
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Store failed"}
	}

	return nil
}
