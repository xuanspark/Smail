package imap_server

import (
	"fmt"
	"math"

	"github.com/emersion/go-imap/v2"
	"github.com/spf13/cast"
)

func imapUint32(value int) (uint32, error) {
	if value < 0 || uint64(value) > math.MaxUint32 {
		return 0, fmt.Errorf("IMAP number out of range")
	}
	return cast.ToUint32(value), nil
}

func imapUID(value int) (imap.UID, error) {
	number, err := imapUint32(value)
	if err != nil || number == 0 {
		return 0, fmt.Errorf("IMAP UID out of range")
	}
	return imap.UID(number), nil
}

// 在发送更新、选择邮件或删除数据之前校验整个会话快照，禁止截断后指向另一封邮件。
func imapSnapshotBounds(uids []int) (uint32, uint32, error) {
	count, err := imapUint32(len(uids))
	if err != nil {
		return 0, 0, err
	}
	var maximum uint32
	for _, value := range uids {
		uid, err := imapUID(value)
		if err != nil {
			return 0, 0, err
		}
		maximum = max(maximum, uint32(uid))
	}
	return maximum, count, nil
}
