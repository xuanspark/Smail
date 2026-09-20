package imap_server

import (
	"fmt"
	"log/slog"
	"sort"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/del_email"
	"git.pcux.cn/xuans/Smail/services/list"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/spf13/cast"
)

const maxIMAPExpungeUIDs = 10000

type imapExpungeTarget struct {
	uid      int
	sequence uint32
}

func (s *serverSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	if s == nil || s.ctx == nil || s.ctx.UserID <= 0 {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Expunge failed"}
	}
	// CLOSE 也会调用这里；只读会话正常关闭，但不能真正删除邮件。
	if s.readOnly {
		return nil
	}

	snapshot, err := s.selectedUIDs()
	if err != nil {
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Expunge failed"}
	}
	// 只匹配会话内真实存在的 UID，不展开巨大或稀疏区间。
	maximum, _, err := imapSnapshotBounds(snapshot)
	if err != nil {
		return err
	}
	requestedUIDs := make([]int, 0, len(snapshot))
	for _, uid := range snapshot {
		number, err := imapUID(uid)
		if err != nil {
			return err
		}
		matched := uids == nil
		if uids != nil {
			for _, span := range *uids {
				matched = matched || numberInRange(uint32(number), uint32(span.Start), uint32(span.Stop), maximum)
			}
		}
		if matched {
			requestedUIDs = append(requestedUIDs, uid)
		}
	}
	if len(requestedUIDs) == 0 {
		return nil
	}
	targets, err := markedDeletedMessages(s, requestedUIDs)
	if err != nil {
		slog.Error("query IMAP deleted messages failed", "error", err)
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Expunge failed"}
	}
	if len(targets) == 0 {
		return nil
	}

	uidList := make([]int, 0, len(targets))
	for _, target := range targets {
		uidList = append(uidList, target.uid)
	}
	if err := del_email.DelByUID(s.ctx, uidList); err != nil {
		slog.Error("delete IMAP messages failed", "error", err)
		return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Expunge failed"}
	}

	sort.Slice(targets, func(i, j int) bool {
		return targets[i].sequence > targets[j].sequence
	})
	for _, target := range targets {
		if w != nil {
			if err := w.WriteExpunge(target.sequence); err != nil {
				return err
			}
		}
		s.mailboxMu.Lock()
		for index, uid := range s.mailboxUIDs {
			if uid == target.uid {
				s.mailboxUIDs = append(s.mailboxUIDs[:index], s.mailboxUIDs[index+1:]...)
				break
			}
		}
		s.mailboxMu.Unlock()
	}
	return nil
}

func expandIMAPUIDSet(uids *imap.UIDSet, maxUID int) ([]int, error) {
	if uids == nil {
		return nil, nil
	}
	maximum, err := imapUint32(maxUID)
	if err != nil {
		return nil, err
	}
	result := make([]int, 0)
	seen := make(map[int]struct{})
	appendUID := func(uid imap.UID) error {
		value := cast.ToInt(uint32(uid))
		if value <= 0 {
			return nil
		}
		if _, exists := seen[value]; exists {
			return nil
		}
		if len(result) >= maxIMAPExpungeUIDs {
			return fmt.Errorf("UID range is too large")
		}
		seen[value] = struct{}{}
		result = append(result, value)
		return nil
	}
	for _, uidRange := range *uids {
		start := uidRange.Start
		stop := uidRange.Stop
		if start == 0 {
			if maxUID > 0 {
				if err := appendUID(imap.UID(maximum)); err != nil {
					return nil, err
				}
			}
			continue
		}
		if stop == 0 {
			if maxUID <= 0 {
				continue
			}
			if imap.UID(maximum) < start {
				if err := appendUID(imap.UID(maximum)); err != nil {
					return nil, err
				}
				continue
			}
			stop = imap.UID(maximum)
		}
		if stop < start {
			return nil, fmt.Errorf("invalid UID range")
		}
		span := uint64(stop-start) + 1
		if span > maxIMAPExpungeUIDs {
			return nil, fmt.Errorf("UID range is too large")
		}
		for value := start; ; value++ {
			if err := appendUID(value); err != nil {
				return nil, err
			}
			if value == stop {
				break
			}
		}
	}
	return result, nil
}

func markedDeletedMessages(s *serverSession, requested []int) ([]imapExpungeTarget, error) {
	uids, err := s.selectedUIDs()
	if err != nil {
		return nil, err
	}
	sequences := make(map[int]int, len(uids))
	for index, uid := range uids {
		sequences[uid] = index + 1
	}
	var relations []*response.UserEmailUIDData
	for start := 0; start < len(requested); start += imapUIDQueryBatchSize {
		end := min(start+imapUIDQueryBatchSize, len(requested))
		batch, err := list.GetUEListByUID(s.ctx, s.currentMailbox, 0, 0, requested[start:end])
		if err != nil {
			return nil, err
		}
		relations = append(relations, batch...)
	}
	targets := make([]imapExpungeTarget, 0, len(relations))
	for _, relation := range relations {
		if relation == nil || relation.ID <= 0 || relation.IsDeleted != 1 || sequences[relation.ID] == 0 {
			continue
		}
		sequence, err := imapUint32(sequences[relation.ID])
		if err != nil || sequence == 0 {
			return nil, fmt.Errorf("invalid message sequence number")
		}
		targets = append(targets, imapExpungeTarget{
			uid:      relation.ID,
			sequence: sequence,
		})
	}
	return targets, nil
}
