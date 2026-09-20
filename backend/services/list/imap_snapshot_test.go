package list

import (
	"math"
	"strconv"
	"testing"
)

func TestIMAPSnapshotCountRejectsInvalidUIDBeforeDatabaseRead(t *testing.T) {
	for _, snapshot := range [][]int{{0}, {-1}, {1, 0}} {
		if _, err := SearchEmails(nil, "INBOX", nil, snapshot); err == nil {
			t.Fatal("非法快照必须在查询数据库之前拒绝")
		}
	}
	if count, err := imapSnapshotCount(nil); err != nil || count != 0 {
		t.Fatalf("空快照验证失败：%d %v", count, err)
	}
	if strconv.IntSize >= 64 {
		limit := uint64(math.MaxUint32)
		if count, err := imapSnapshotCount([]int{int(limit)}); err != nil || count != 1 {
			t.Fatalf("最大合法 UID 被拒绝：%d %v", count, err)
		}
		if _, err := SearchEmails(nil, "INBOX", nil, []int{int(limit + 1)}); err == nil {
			t.Fatal("越界 UID 不应被截断为搜索结果")
		}
	}
}
