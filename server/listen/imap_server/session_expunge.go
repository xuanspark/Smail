package imap_server

import (
	"fmt"
	"log/slog"
	"sort"

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

	maxUID := 0
	var err error
	if uids != nil && uids.Dynamic() {
		maxUID, err = list.GetMaxUIDByMailbox(s.ctx, s.currentMailbox)
		if err != nil {
			slog.Error("query IMAP maximum UID failed", "error", err)
			return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "Expunge failed"}
		}
	}
	requestedUIDs, err := expandIMAPUIDSet(uids, maxUID)
	if err != nil {
		return &imap.Error{Type: imap.StatusResponseTypeBad, Text: err.Error()}
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
	}
	return nil
}

func expandIMAPUIDSet(uids *imap.UIDSet, maxUID int) ([]int, error) {
	if uids == nil {
		return nil, nil
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
				if err := appendUID(imap.UID(maxUID)); err != nil {
					return nil, err
				}
			}
			continue
		}
		if stop == 0 {
			if maxUID <= 0 {
				continue
			}
			if imap.UID(maxUID) < start {
				if err := appendUID(imap.UID(maxUID)); err != nil {
					return nil, err
				}
				continue
			}
			stop = imap.UID(maxUID)
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
	relations, err := list.GetUEListByUID(s.ctx, s.currentMailbox, 0, 0, requested)
	if err != nil {
		return nil, err
	}
	targets := make([]imapExpungeTarget, 0, len(relations))
	for _, relation := range relations {
		if relation == nil || relation.ID <= 0 || relation.IsDeleted != 1 {
			continue
		}
		sequence, err := imapUint32(relation.SerialNumber)
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
