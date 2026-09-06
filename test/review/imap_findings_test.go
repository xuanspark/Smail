package imap_server

import (
	"reflect"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-imap/v2"
)

func TestAuditLargeSelectedMailboxMustBatchUIDQueries(t *testing.T) {
	user := models.User{Account: t.Name()}
	if _, err := db.Instance.Insert(&user); err != nil {
		t.Fatal(err)
	}
	message := models.Email{Subject: t.Name()}
	if _, err := db.Instance.Insert(&message); err != nil {
		t.Fatal(err)
	}
	const count = 10001
	uids := make([]int, 0, count)
	deleted := make(map[int]bool)
	// 独立账户及共享邮件内容限制夹具体积，同时保留每个副本的真实 UID。
	for start := 0; start < count; start += 400 {
		batch := make([]models.UserEmail, min(400, count-start))
		for index := range batch {
			batch[index] = models.UserEmail{UserID: user.ID, EmailID: message.Id}
			if position := start + index; position == 0 || position == 500 || position == count-1 {
				batch[index].IsDeleted = 1
			}
		}
		if _, err := db.Instance.Insert(&batch); err != nil {
			t.Fatal(err)
		}
	}
	relations, err := list.GetUEListByUID(&context.Context{UserID: user.ID}, "INBOX", 0, 0, nil)
	if err != nil || len(relations) != count {
		t.Fatalf("大邮箱夹具不完整：count=%d err=%v", len(relations), err)
	}
	for _, relation := range relations {
		uids = append(uids, relation.ID)
		if relation.IsDeleted == 1 {
			deleted[relation.ID] = true
		}
	}
	session := &serverSession{ctx: &context.Context{UserID: user.ID}, currentMailbox: "INBOX", mailboxUIDs: uids}
	for _, numbers := range []imap.NumSet{
		imap.SeqSet{{Start: 1, Stop: 0}},
		imap.UIDSet{{Start: imap.UID(uids[0]), Stop: 0}},
	} {
		rows, err := session.selectedMessages(numbers)
		if err != nil || len(rows) != count {
			t.Fatalf("范围读取必须分批保留全部副本：count=%d err=%v", len(rows), err)
		}
		for index, row := range rows {
			if row.UeId != uids[index] || row.SerialNumber != index+1 {
				t.Fatalf("分批读取改变了 UID 或会话序号：index=%d uid=%d sequence=%d", index, row.UeId, row.SerialNumber)
			}
		}
	}
	if err := session.Expunge(nil, nil); err != nil {
		t.Fatalf("大邮箱 EXPUNGE 不能被查询参数上限拒绝：%v", err)
	}
	if len(session.mailboxUIDs) != count-len(deleted) {
		t.Fatalf("删除后快照数量错误：%d", len(session.mailboxUIDs))
	}
	for _, uid := range session.mailboxUIDs {
		if deleted[uid] {
			t.Fatalf("已清除的 UID 仍留在会话快照：%d", uid)
		}
	}
	remaining, err := db.Instance.Where("user_id = ?", user.ID).Count(&models.UserEmail{})
	if err != nil || remaining != int64(count-len(deleted)) {
		t.Fatalf("大邮箱只能清除标记邮件：remaining=%d err=%v", remaining, err)
	}
}

func TestAuditSnapshotSearchAndExpungeBoundaries(t *testing.T) {
	first, last := auditIMAPMessage(t), auditIMAPMessage(t)
	ctx := &context.Context{UserID: first.UserID}
	snapshot := []int{first.ID, last.ID}
	starUID := imap.UIDSet{{Start: 0, Stop: 0}}
	starSequence := imap.SeqSet{{Start: 0, Stop: 0}}
	for _, criteria := range []*imap.SearchCriteria{
		{UID: []imap.UIDSet{starUID}},
		{SeqNum: []imap.SeqSet{starSequence}},
	} {
		rows, err := list.SearchEmails(ctx, "INBOX", criteria, snapshot)
		if err != nil || len(rows) != 1 || rows[0].ID != last.ID {
			t.Fatalf("通配应选择快照最后一封：rows=%v err=%v", rows, err)
		}
	}
	rows, err := list.SearchEmails(ctx, "INBOX", &imap.SearchCriteria{
		UID: []imap.UIDSet{imap.UIDSetNum(imap.UID(first.ID))}, SeqNum: []imap.SeqSet{starSequence},
	}, snapshot)
	if err != nil || len(rows) != 0 {
		t.Fatalf("UID 筛选不能缩小序号通配上界：rows=%v err=%v", rows, err)
	}
	session := &serverSession{ctx: ctx, currentMailbox: "INBOX", mailboxUIDs: append([]int{}, snapshot...)}
	if _, err := db.Instance.ID(last.ID).Delete(&models.UserEmail{}); err != nil {
		t.Fatal(err)
	}
	for _, criteria := range []*imap.SearchCriteria{
		{UID: []imap.UIDSet{starUID}}, {SeqNum: []imap.SeqSet{starSequence}},
		{Or: [][2]imap.SearchCriteria{{{UID: []imap.UIDSet{starUID}}, {SeqNum: []imap.SeqSet{starSequence}}}}},
	} {
		rows, err := list.SearchEmails(ctx, "INBOX", criteria, snapshot)
		if err != nil || len(rows) != 0 {
			t.Fatalf("未通知删除不能把通配目标前移：rows=%v err=%v", rows, err)
		}
	}
	rows, err = list.SearchEmails(ctx, "INBOX", &imap.SearchCriteria{Not: []imap.SearchCriteria{{UID: []imap.UIDSet{starUID}}}}, snapshot)
	if err != nil || len(rows) != 1 || rows[0].ID != first.ID {
		t.Fatalf("NOT 必须使用同一快照：rows=%v err=%v", rows, err)
	}
	selected, err := session.selectedMessages(imap.SeqSetNum(2))
	if err != nil || len(selected) != 0 {
		t.Fatalf("FETCH/STORE 共用目标解析不能改变已删除序号：rows=%v err=%v", selected, err)
	}
	if _, err := db.Instance.ID(first.ID).Cols("is_deleted").Update(&models.UserEmail{IsDeleted: 1}); err != nil {
		t.Fatal(err)
	}
	empty := imap.UIDSet{}
	for _, set := range []*imap.UIDSet{&empty, &starUID} {
		if err := session.Expunge(nil, set); err != nil {
			t.Fatal(err)
		}
		if found, err := db.Instance.ID(first.ID).Exist(&models.UserEmail{}); err != nil || !found {
			t.Fatalf("空集合或尾部通配不得删除其他邮件：found=%v err=%v", found, err)
		}
	}
	// 极大反向区间按快照过滤，不能按数值展开或错误拒绝。
	wide := imap.UIDSet{{Start: imap.UID(^uint32(0)), Stop: 1}}
	if err := session.Expunge(nil, &wide); err != nil {
		t.Fatal(err)
	}
	if found, err := db.Instance.ID(first.ID).Exist(&models.UserEmail{}); err != nil || found {
		t.Fatalf("反向范围应删除匹配标记：found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(session.mailboxUIDs, []int{last.ID}) {
		t.Fatalf("只移除已经通知删除的序号：%v", session.mailboxUIDs)
	}
}

// 使用现有 TestMain 创建的临时 SQLite 和随机本地 TLS 端口，不连接开发服务。
func TestAuditCopyThenMoveMustKeepMailboxIdentity(t *testing.T) {
	var user models.User
	if found, err := db.Instance.Where("account=?", "testCase").Get(&user); err != nil || !found {
		t.Fatalf("临时测试账户不可用: found=%v err=%v", found, err)
	}
	message := models.Email{Subject: "audit-copy-move"}
	if _, err := db.Instance.Insert(&message); err != nil {
		t.Fatal(err)
	}
	relation := models.UserEmail{UserID: user.ID, EmailID: message.Id, Status: 0}
	if _, err := db.Instance.Insert(&relation); err != nil {
		t.Fatal(err)
	}
	if _, err := clientLogin.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	uid := imap.UIDSetNum(imap.UID(relation.ID))
	if _, err := clientLogin.Copy(uid, "Junk").Wait(); err != nil {
		t.Fatalf("复制前置条件失败: %v", err)
	}
	count, err := db.Instance.Where("user_id=? AND email_id=?", user.ID, message.Id).Count(&models.UserEmail{})
	if err != nil || count != 2 {
		t.Fatalf("未生成预期副本: count=%d err=%v", count, err)
	}
	// 当前服务器未声明 MOVE 扩展，客户端会回退 COPY/STORE/EXPUNGE。
	// 另外验证网页移动所用的服务，避免把回退成功误判为原生 MOVE 已通过。
	webMoveErr := group.Move2DefaultBox(&context.Context{UserID: user.ID}, []int{message.Id}, "Deleted Messages")
	t.Logf("web_move_after_imap_copy_error=%v", webMoveErr)
	if webMoveErr != nil {
		t.Errorf("IMAP 复制后网页移动不应把自己的邮件判断为不属于自己: %v", webMoveErr)
	}
	// 网页移动已经移走原 UID；另建独立样本验证客户端回退，不能重复移动旧 UID。
	moveRelation := auditIMAPMessage(t)
	if _, err := clientLogin.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	_, err = clientLogin.Move(imap.UIDSetNum(imap.UID(moveRelation.ID)), "Deleted Messages").Wait()
	t.Logf("copy_relation_count=%d move_error=%v", count, err)
	if err != nil {
		t.Fatalf("同一用户复制邮件后，移动原文件夹中的 UID 不应失败: %v", err)
	}
}

func auditIMAPMessage(t *testing.T) models.UserEmail {
	t.Helper()
	var user models.User
	if found, err := db.Instance.Where("account=?", "testCase").Get(&user); err != nil || !found {
		t.Fatalf("测试账户不可用: found=%v err=%v", found, err)
	}
	message := models.Email{Subject: t.Name()}
	if _, err := db.Instance.Insert(&message); err != nil {
		t.Fatal(err)
	}
	relation := models.UserEmail{UserID: user.ID, EmailID: message.Id, Status: 0}
	if _, err := db.Instance.Insert(&relation); err != nil {
		t.Fatal(err)
	}
	return relation
}

func TestAuditExamineMustNotPersistStore(t *testing.T) {
	relation := auditIMAPMessage(t)
	if _, err := clientLogin.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		t.Fatal(err)
	}
	err := clientLogin.Store(imap.UIDSetNum(imap.UID(relation.ID)), &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagSeen}, Silent: true,
	}, nil).Close()
	var updated models.UserEmail
	if found, lookupErr := db.Instance.ID(relation.ID).Get(&updated); lookupErr != nil || !found {
		t.Fatalf("读取测试邮件失败: found=%v err=%v", found, lookupErr)
	}
	t.Logf("readonly=true store_error=%v persisted_is_read=%d", err, updated.IsRead)
	if updated.IsRead != relation.IsRead {
		t.Fatal("EXAMINE 只读打开后，STORE 仍持久化修改已读状态")
	}
}

func TestAuditCopiedMessagesMustKeepDistinctUIDs(t *testing.T) {
	relation := auditIMAPMessage(t)
	if _, err := clientLogin.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if _, err := clientLogin.Copy(imap.UIDSetNum(imap.UID(relation.ID)), "INBOX").Wait(); err != nil {
		t.Fatal(err)
	}
	var relations []models.UserEmail
	if err := db.Instance.Where("user_id=? AND email_id=?", relation.UserID, relation.EmailID).Find(&relations); err != nil || len(relations) != 2 {
		t.Fatalf("复制前置条件失败: count=%d err=%v", len(relations), err)
	}
	for _, stored := range relations {
		t.Logf("stored_uid=%d status=%d group=%d", stored.ID, stored.Status, stored.GroupId)
	}
	rows, lookupErr := list.GetEmailListByGroup(&context.Context{UserID: relation.UserID}, "INBOX", list.ImapListReq{
		UidList: []int{relations[0].ID, relations[1].ID},
	}, true)
	if lookupErr != nil || len(rows) != 2 {
		t.Fatalf("直接查询副本失败: count=%d err=%v", len(rows), lookupErr)
	}
	t.Logf("hydrated_uids=%d,%d same_pointer=%v", rows[0].UeId, rows[1].UeId, rows[0] == rows[1])
	if rows[0] == rows[1] || rows[0].UeId == rows[1].UeId {
		t.Error("不同邮箱关系复用了同一个可变响应对象，UID 被后一个副本覆盖")
	}
	uids := imap.UIDSetNum(imap.UID(relations[0].ID), imap.UID(relations[1].ID))
	messages, err := clientLogin.Fetch(uids, &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil || len(messages) != 2 {
		t.Fatalf("读取两个副本失败: count=%d err=%v", len(messages), err)
	}
	t.Logf("stored_uids=%d,%d fetched_uids=%d,%d", relations[0].ID, relations[1].ID, messages[0].UID, messages[1].UID)
	if messages[0].UID == messages[1].UID {
		t.Fatal("不同副本在实际 IMAP FETCH 响应中使用了同一个 UID")
	}
}

func TestAuditListPercentMustIncludeInbox(t *testing.T) {
	mailboxes, err := clientLogin.List("", "%", nil).Collect()
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(mailboxes))
	found := false
	for _, mailbox := range mailboxes {
		names = append(names, mailbox.Mailbox)
		found = found || mailbox.Mailbox == "INBOX"
	}
	t.Logf("top_level_mailboxes=%v", names)
	if !found {
		t.Fatal("LIST 顶层通配符遗漏 INBOX 和默认文件夹")
	}
}

func TestAuditUIDNextMustNotDecrease(t *testing.T) {
	relation := auditIMAPMessage(t)
	before, err := clientLogin.Status("INBOX", &imap.StatusOptions{UIDNext: true, UIDValidity: true}).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Move2DefaultBox(&context.Context{UserID: relation.UserID}, []int{relation.EmailID}, "Junk"); err != nil {
		t.Fatal(err)
	}
	after, err := clientLogin.Status("INBOX", &imap.StatusOptions{UIDNext: true, UIDValidity: true}).Wait()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uidnext_before=%d uidnext_after=%d validity_before=%d validity_after=%d", before.UIDNext, after.UIDNext, before.UIDValidity, after.UIDValidity)
	if after.UIDValidity == before.UIDValidity && after.UIDNext < before.UIDNext {
		t.Fatal("同一 UIDVALIDITY 下，移走最大 UID 邮件导致 UIDNEXT 倒退")
	}
}

func TestAuditFetchWildcardUIDMustReturnLastMessage(t *testing.T) {
	relation := auditIMAPMessage(t)
	if _, err := clientLogin.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	messages, err := clientLogin.Fetch(imap.UIDSetNum(0), &imap.FetchOptions{UID: true}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	// go-imap/v2 beta.5 的动态 UID 集合 Contains(*) 固定返回 false，
	// 因而客户端会丢弃合法的动态 UID 响应；静态 UID 用于验证响应解析本身。
	if len(messages) != 0 {
		t.Fatalf("动态 UID 客户端结果异常: count=%d", len(messages))
	}
	staticMessages, err := clientLogin.Fetch(imap.UIDSetNum(imap.UID(relation.ID)), &imap.FetchOptions{UID: true}).Collect()
	if err != nil || len(staticMessages) != 1 || staticMessages[0].UID != imap.UID(relation.ID) {
		t.Fatalf("静态 UID FETCH 未正确解析服务端响应: count=%d err=%v", len(staticMessages), err)
	}
}

func TestAuditInboxCountMustExcludeLoginBindings(t *testing.T) {
	relation := auditIMAPMessage(t)
	bindings, err := db.Instance.Where("user_id=? AND email_id=0 AND group_id=0 AND status=0", relation.UserID).Count(&models.UserEmail{})
	if err != nil || bindings < 1 {
		t.Fatalf("临时登录邮箱绑定前置条件未成立: count=%d err=%v", bindings, err)
	}
	want, err := db.Instance.Where("user_id=? AND email_id>0 AND group_id=0 AND status=0", relation.UserID).Count(&models.UserEmail{})
	if err != nil {
		t.Fatal(err)
	}
	status, err := clientLogin.Status("INBOX", &imap.StatusOptions{NumMessages: true}).Wait()
	if err != nil || status.NumMessages == nil {
		t.Fatalf("查询邮箱数量失败: status=%v err=%v", status, err)
	}
	t.Logf("login_bindings=%d real_messages=%d status_messages=%d", bindings, want, *status.NumMessages)
	if int64(*status.NumMessages) != want {
		t.Fatal("登录邮箱绑定被当作邮件计入 INBOX，状态数量与可读取邮件不一致")
	}
}

func TestAuditSearchKeysMustIntersect(t *testing.T) {
	first := auditIMAPMessage(t)
	second := auditIMAPMessage(t)
	third := auditIMAPMessage(t)
	if _, err := clientLogin.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	uidCriterion := func(ids ...int) imap.SearchCriteria {
		var set imap.UIDSet
		for _, id := range ids {
			set.AddNum(imap.UID(id))
		}
		return imap.SearchCriteria{UID: []imap.UIDSet{set}}
	}
	cases := map[string]imap.SearchCriteria{
		"multiple_or": {Or: [][2]imap.SearchCriteria{
			{uidCriterion(first.ID), uidCriterion(second.ID)},
			{uidCriterion(second.ID), uidCriterion(third.ID)},
		}},
		"multiple_uid": {UID: []imap.UIDSet{
			imap.UIDSetNum(imap.UID(first.ID), imap.UID(second.ID)),
			imap.UIDSetNum(imap.UID(second.ID), imap.UID(third.ID)),
		}},
	}
	for name, criteria := range cases {
		t.Run(name, func(t *testing.T) {
			result, err := clientLogin.UIDSearch(&criteria, nil).Wait()
			if err != nil {
				t.Fatal(err)
			}
			uids := result.AllUIDs()
			t.Logf("returned=%v expected_only=%d", uids, second.ID)
			if len(uids) != 1 || uids[0] != imap.UID(second.ID) {
				t.Fatal("同一个 SEARCH 的多个键应取交集，而不是并集")
			}
		})
	}
}

func TestAuditSearchDeletedMustMatchStoredFlag(t *testing.T) {
	relation := auditIMAPMessage(t)
	if _, err := clientLogin.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	uid := imap.UIDSetNum(imap.UID(relation.ID))
	if err := clientLogin.Store(uid, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true,
	}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	messages, err := clientLogin.Fetch(uid, &imap.FetchOptions{Flags: true, UID: true}).Collect()
	if err != nil || len(messages) != 1 {
		t.Fatalf("读取标记前置条件失败: count=%d err=%v", len(messages), err)
	}
	deleted := false
	for _, flag := range messages[0].Flags {
		deleted = deleted || flag == imap.FlagDeleted
	}
	if !deleted {
		t.Fatal("STORE 后 FETCH 没有返回删除标记，不能继续归因 SEARCH")
	}
	result, err := clientLogin.UIDSearch(&imap.SearchCriteria{
		UID: []imap.UIDSet{uid}, Flag: []imap.Flag{imap.FlagDeleted},
	}, nil).Wait()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fetch_deleted=%t search_uids=%v expected_uid=%d", deleted, result.AllUIDs(), relation.ID)
	if len(result.AllUIDs()) != 1 || result.AllUIDs()[0] != imap.UID(relation.ID) {
		t.Fatal("已通过 STORE 设置且 FETCH 确认的删除标记未被 SEARCH 识别")
	}
}
