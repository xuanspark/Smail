package imap_server

import (
	"bytes"
	"math"
	"reflect"
	"strconv"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2"
)

func TestIMAPSnapshotBounds(t *testing.T) {
	for _, test := range []struct {
		name    string
		uids    []int
		maximum uint32
		invalid bool
	}{
		{name: "empty"},
		{name: "valid", uids: []int{7, 2, 9}, maximum: 9},
		{name: "zero", uids: []int{7, 0}, invalid: true},
		{name: "negative", uids: []int{7, -1}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			maximum, count, err := imapSnapshotBounds(test.uids)
			if test.invalid {
				if err == nil || maximum != 0 || count != 0 {
					t.Fatal("非法快照必须完整拒绝")
				}
				return
			}
			if err != nil || maximum != test.maximum || int(count) != len(test.uids) {
				t.Fatalf("快照边界错误：%d %d %v", maximum, count, err)
			}
		})
	}
}

func TestIMAPRejectsOverflowBeforeProtocolSideEffects(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("此用例验证大于协议上限的六十四位数据库标识")
	}
	limit := uint64(math.MaxUint32)
	if maximum, count, err := imapSnapshotBounds([]int{int(limit)}); err != nil || maximum != math.MaxUint32 || count != 1 {
		t.Fatalf("最大合法 UID 被拒绝：%d %d %v", maximum, count, err)
	}
	for _, uid := range []int{0, -1, int(limit + 1), int(limit + 2)} {
		snapshot := []int{1, uid}
		session := &serverSession{ctx: &context.Context{UserID: 1}, currentMailbox: "INBOX", mailboxUIDs: append([]int{}, snapshot...)}
		if _, err := session.selectedMessages(imap.UIDSetNum(1)); err == nil {
			t.Fatal("邮件选择未拒绝非法快照")
		}
		if err := session.poll(nil, true, true); err == nil {
			t.Fatal("轮询未在发送更新前拒绝非法快照")
		}
		if err := session.Expunge(nil, nil); err == nil {
			t.Fatal("删除未在写数据库前拒绝非法快照")
		}
		if !reflect.DeepEqual(session.mailboxUIDs, snapshot) {
			t.Fatal("拒绝非法快照时修改了会话状态")
		}
	}
	set := imap.UIDSet{{Start: 0, Stop: 0}}
	if _, err := expandIMAPUIDSet(&set, int(limit+1)); err == nil {
		t.Fatal("动态范围上界发生截断")
	}
}

func TestAppendUIDOverflowRollsBackMessageAndRelation(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("此用例验证六十四位数据库自增值超过协议上限")
	}
	rows, err := db.Instance.QueryString("SELECT seq FROM sqlite_sequence WHERE name = 'user_email'")
	if err != nil || len(rows) != 1 {
		t.Fatalf("读取测试自增序列失败：%v", err)
	}
	previous := rows[0]["seq"]
	defer func() {
		if _, err := db.Instance.Exec("UPDATE sqlite_sequence SET seq = ? WHERE name = 'user_email'", previous); err != nil {
			t.Errorf("恢复测试序列失败：%v", err)
		}
	}()
	if _, err := db.Instance.Exec("UPDATE sqlite_sequence SET seq = ? WHERE name = 'user_email'", int64(math.MaxUint32)); err != nil {
		t.Fatal(err)
	}
	beforeMessages, err := db.Instance.Count(new(models.Email))
	if err != nil {
		t.Fatal(err)
	}
	beforeRelations, err := db.Instance.Count(new(models.UserEmail))
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("From: sender@test.domain\r\nTo: testCase@test.domain\r\nSubject: boundary\r\nMessage-ID: <uid-overflow@test.domain>\r\n\r\nbody\r\n")
	session := &serverSession{ctx: &context.Context{UserID: clientLoginUserID(t)}, status: AUTHORIZED}
	if data, err := session.Append("INBOX", bytes.NewReader(message), nil); err == nil || data != nil {
		t.Fatal("越界 APPEND 不应提交或返回 UID")
	}
	afterMessages, err := db.Instance.Count(new(models.Email))
	if err != nil {
		t.Fatal(err)
	}
	afterRelations, err := db.Instance.Count(new(models.UserEmail))
	if err != nil {
		t.Fatal(err)
	}
	if beforeMessages != afterMessages || beforeRelations != afterRelations {
		t.Fatal("失败 APPEND 留下了邮件或关联记录")
	}
}
