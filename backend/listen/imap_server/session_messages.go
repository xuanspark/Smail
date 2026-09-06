package imap_server

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/list"
	"github.com/emersion/go-imap/v2"
)

// 分批限制同时适配 SQLite 参数数量和底层 UID 列表校验，不限制邮箱总量。
const imapUIDQueryBatchSize = 500

// selectedUIDs 返回客户端已经获知的序号快照，外部删除不能提前改变命令目标。
func (s *serverSession) selectedUIDs() ([]int, error) {
	s.mailboxMu.Lock()
	defer s.mailboxMu.Unlock()
	if s.mailboxUIDs == nil {
		relations, err := list.GetUEListByUID(s.ctx, s.currentMailbox, 0, 0, nil)
		if err != nil {
			return nil, err
		}
		s.mailboxUIDs = make([]int, 0, len(relations))
		for _, relation := range relations {
			s.mailboxUIDs = append(s.mailboxUIDs, relation.ID)
		}
	}
	return append([]int{}, s.mailboxUIDs...), nil
}

func numberInRange(value, start, stop, maximum uint32) bool {
	if start == 0 {
		start = maximum
	}
	if stop == 0 {
		stop = maximum
	}
	if start > stop {
		start, stop = stop, start
	}
	return value >= start && value <= stop
}

func (s *serverSession) selectedMessages(numbers imap.NumSet) ([]*response.EmailResponseData, error) {
	uids, err := s.selectedUIDs()
	if err != nil {
		return nil, err
	}
	var maximum uint32
	for _, uid := range uids {
		maximum = max(maximum, uint32(uid))
	}
	selected := make([]int, 0)
	sequences := make(map[int]int, len(uids))
	for index, uid := range uids {
		sequences[uid] = index + 1
		matched := false
		switch set := numbers.(type) {
		case imap.SeqSet:
			for _, span := range set {
				matched = matched || numberInRange(uint32(index+1), span.Start, span.Stop, uint32(len(uids)))
			}
		case imap.UIDSet:
			for _, span := range set {
				matched = matched || numberInRange(uint32(uid), uint32(span.Start), uint32(span.Stop), maximum)
			}
		}
		if matched {
			selected = append(selected, uid)
		}
	}
	if len(selected) == 0 {
		return nil, nil
	}
	rows := make([]*response.EmailResponseData, 0, len(selected))
	for start := 0; start < len(selected); start += imapUIDQueryBatchSize {
		end := min(start+imapUIDQueryBatchSize, len(selected))
		batch, err := list.GetEmailListByGroup(s.ctx, s.currentMailbox, list.ImapListReq{UidList: selected[start:end]}, true)
		if err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
	}
	for _, row := range rows {
		row.SerialNumber = sequences[row.UeId]
	}
	return rows, nil
}
