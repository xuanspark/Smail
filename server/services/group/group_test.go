package group

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func initGroupTestDB(t *testing.T) {
	t.Helper()
	config.Instance = &config.Config{
		DbType: config.DBTypeSQLite,
		DbDSN:  filepath.Join(t.TempDir(), "group.db"),
	}
	if err := db.Init(""); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.Instance != nil {
			_ = db.Instance.Close()
		}
	})
}

func insertTestGroup(t *testing.T, userID int, name string) models.Group {
	t.Helper()
	group := models.Group{Name: name, UserId: userID, FullPath: name}
	if _, err := db.Instance.Insert(&group); err != nil {
		t.Fatal(err)
	}
	return group
}

func insertTestUserEmail(t *testing.T, userID, emailID, groupID int) {
	t.Helper()
	if _, err := db.Instance.Insert(&models.UserEmail{UserID: userID, EmailID: emailID, GroupId: groupID}); err != nil {
		t.Fatal(err)
	}
}

func getTestUserEmail(t *testing.T, userID, emailID int) models.UserEmail {
	t.Helper()
	var item models.UserEmail
	has, err := db.Instance.Where("user_id = ? and email_id = ?", userID, emailID).Get(&item)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Fatalf("user_email not found: user=%d email=%d", userID, emailID)
	}
	return item
}

func TestGetGroupStatus(t *testing.T) {
	initGroupTestDB(t)
	db.Instance.ShowSQL(true)
	ctx := &context.Context{
		UserID:      1,
		UserName:    "admin",
		UserAccount: "admin",
	}
	ret, _, err := GetGroupStatus(ctx, "INBOX", []string{"MESSAGES", "UIDNEXT", "UIDVALIDITY", "UNSEEN"})
	if err != nil {
		t.Fatal(err)
	}
	if ret == "" {
		t.Fatal("empty group status")
	}
}

func TestMoveMailToGroupRejectsOtherUserGroup(t *testing.T) {
	initGroupTestDB(t)
	foreignGroup := insertTestGroup(t, 1, "owner-group")
	insertTestUserEmail(t, 2, 101, 0)

	ctx := &context.Context{UserID: 2}
	if err := MoveMailToGroup(ctx, []int{101}, foreignGroup.ID); err == nil {
		t.Fatal("mail was moved into another user's group")
	}
	if got := getTestUserEmail(t, 2, 101).GroupId; got != 0 {
		t.Fatalf("group_id = %d, want 0", got)
	}
}

func TestMoveMailToGroupRejectsPartiallyOwnedBatch(t *testing.T) {
	initGroupTestDB(t)
	target := insertTestGroup(t, 1, "target")
	insertTestUserEmail(t, 1, 111, 0)
	insertTestUserEmail(t, 2, 222, 0)

	err := MoveMailToGroup(&context.Context{UserID: 1}, []int{111, 222}, target.ID)
	if err == nil {
		t.Fatal("partially owned batch was accepted")
	}
	if got := getTestUserEmail(t, 1, 111).GroupId; got != 0 {
		t.Fatalf("owned message was partially moved: group_id = %d", got)
	}
}
func TestMove2DefaultBoxRejectsPartiallyOwnedBatch(t *testing.T) {
	initGroupTestDB(t)
	insertTestUserEmail(t, 1, 311, 71)
	insertTestUserEmail(t, 2, 322, 72)
	if _, err := db.Instance.Table(&models.UserEmail{}).Where("user_id = ? and email_id = ?", 1, 311).Cols("status").Update(map[string]interface{}{"status": 4}); err != nil {
		t.Fatal(err)
	}

	err := Move2DefaultBox(&context.Context{UserID: 1}, []int{311, 322}, "INBOX")
	if err == nil {
		t.Fatal("partially owned default mailbox move was accepted")
	}
	item := getTestUserEmail(t, 1, 311)
	if item.GroupId != 71 || item.Status != 4 {
		t.Fatalf("owned message was partially moved: group_id=%d status=%d", item.GroupId, item.Status)
	}
}

func TestMove2DefaultBoxUpdatesOwnedBatch(t *testing.T) {
	initGroupTestDB(t)
	insertTestUserEmail(t, 1, 411, 81)
	insertTestUserEmail(t, 1, 422, 82)

	if err := Move2DefaultBox(&context.Context{UserID: 1}, []int{411, 411, 422}, "Junk"); err != nil {
		t.Fatal(err)
	}
	for _, emailID := range []int{411, 422} {
		item := getTestUserEmail(t, 1, emailID)
		if item.GroupId != 0 || item.Status != 5 {
			t.Fatalf("email %d: group_id=%d status=%d", emailID, item.GroupId, item.Status)
		}
	}
}

func TestMove2DefaultBoxRejectsInvalidMailbox(t *testing.T) {
	initGroupTestDB(t)
	insertTestUserEmail(t, 1, 511, 91)

	if err := Move2DefaultBox(&context.Context{UserID: 1}, []int{511}, "Unknown"); err == nil {
		t.Fatal("invalid default mailbox was accepted")
	}
	if got := getTestUserEmail(t, 1, 511).GroupId; got != 91 {
		t.Fatalf("message changed after invalid mailbox request: group_id=%d", got)
	}
}

func TestDelGroupKeepsOtherUsersMail(t *testing.T) {
	initGroupTestDB(t)
	ownedGroup := insertTestGroup(t, 1, "owned-group")
	insertTestUserEmail(t, 1, 201, ownedGroup.ID)
	insertTestUserEmail(t, 2, 202, ownedGroup.ID)

	deleted, err := DelGroup(&context.Context{UserID: 1}, ownedGroup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("owned group was not deleted")
	}
	if got := getTestUserEmail(t, 1, 201).GroupId; got != 0 {
		t.Fatalf("owner group_id = %d, want 0", got)
	}
	if got := getTestUserEmail(t, 2, 202).GroupId; got != ownedGroup.ID {
		t.Fatalf("other user group_id = %d, want %d", got, ownedGroup.ID)
	}
}

func TestDelGroupRollsBackWhenMailResetFails(t *testing.T) {
	initGroupTestDB(t)
	ownedGroup := insertTestGroup(t, 1, "rollback-group")
	insertTestUserEmail(t, 1, 301, ownedGroup.ID)

	triggerSQL := fmt.Sprintf("create trigger fail_group_reset before update of group_id on user_email when old.group_id = %d begin select raise(abort, 'forced reset failure'); end", ownedGroup.ID)
	if _, err := db.Instance.Exec(triggerSQL); err != nil {
		t.Fatal(err)
	}

	deleted, err := DelGroup(&context.Context{UserID: 1}, ownedGroup.ID)
	if err == nil || deleted {
		t.Fatalf("DelGroup() = (%v, %v), want rollback error", deleted, err)
	}

	var count int64
	if count, err = db.Instance.Table("group").Where("id = ? and user_id = ?", ownedGroup.ID, 1).Count(); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("group count = %d, want 1 after rollback", count)
	}
	if got := getTestUserEmail(t, 1, 301).GroupId; got != ownedGroup.ID {
		t.Fatalf("group_id = %d, want %d after rollback", got, ownedGroup.ID)
	}
}

func insertTestChildGroup(t *testing.T, userID int, name string, parentID int, fullPath string) models.Group {
	t.Helper()
	group := models.Group{Name: name, ParentId: parentID, UserId: userID, FullPath: fullPath}
	if _, err := db.Instance.Insert(&group); err != nil {
		t.Fatal(err)
	}
	return group
}

func TestCreateGroupRejectsOtherUserParent(t *testing.T) {
	initGroupTestDB(t)
	foreignParent := insertTestGroup(t, 1, "foreign")

	created, err := CreateGroup(&context.Context{UserID: 2}, "child", foreignParent.ID)
	if err == nil || created != nil {
		t.Fatalf("CreateGroup() = (%v, %v), want ownership error", created, err)
	}

	count, err := db.Instance.Table("group").Where("user_id = ? and name = ?", 2, "child").Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("created group count = %d, want 0", count)
	}
}

func TestCreateGroupRejectsMissingParent(t *testing.T) {
	initGroupTestDB(t)

	created, err := CreateGroup(&context.Context{UserID: 1}, "child", 999999)
	if err == nil || created != nil {
		t.Fatalf("CreateGroup() = (%v, %v), want missing parent error", created, err)
	}
}

func TestCreateGroupAllowsSameLeafUnderDifferentParents(t *testing.T) {
	initGroupTestDB(t)
	ctx := &context.Context{UserID: 1}

	parentOne, err := CreateGroup(ctx, "parentone", 0)
	if err != nil {
		t.Fatal(err)
	}
	parentTwo, err := CreateGroup(ctx, "parenttwo", 0)
	if err != nil {
		t.Fatal(err)
	}
	first, err := CreateGroup(ctx, "leaf", parentOne.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CreateGroup(ctx, "leaf", parentTwo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("group IDs are equal: %d", first.ID)
	}
	if first.FullPath != "parentone/leaf" || second.FullPath != "parenttwo/leaf" {
		t.Fatalf("unexpected paths: %q and %q", first.FullPath, second.FullPath)
	}
	if _, err := GetGroupByName(ctx, "leaf"); err == nil {
		t.Fatal("ambiguous leaf lookup succeeded")
	}
}

func TestCreateGroupRejectsHierarchyCycle(t *testing.T) {
	initGroupTestDB(t)
	first := insertTestGroup(t, 1, "first")
	second := insertTestChildGroup(t, 1, "second", first.ID, "first/second")
	if _, err := db.Instance.Table("group").ID(first.ID).Cols("parent_id").Update(&models.Group{ParentId: second.ID}); err != nil {
		t.Fatal(err)
	}

	created, err := CreateGroup(&context.Context{UserID: 1}, "child", first.ID)
	if err == nil || created != nil {
		t.Fatalf("CreateGroup() = (%v, %v), want hierarchy cycle error", created, err)
	}
}

func TestDelGroupRejectsHierarchyCycle(t *testing.T) {
	initGroupTestDB(t)
	first := insertTestGroup(t, 1, "first")
	second := insertTestChildGroup(t, 1, "second", first.ID, "first/second")
	if _, err := db.Instance.Table("group").ID(first.ID).Cols("parent_id").Update(&models.Group{ParentId: second.ID}); err != nil {
		t.Fatal(err)
	}

	deleted, err := DelGroup(&context.Context{UserID: 1}, first.ID)
	if err == nil || deleted {
		t.Fatalf("DelGroup() = (%v, %v), want hierarchy cycle error", deleted, err)
	}
	count, err := db.Instance.Table("group").Where("user_id = ?", 1).Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("group count = %d, want 2", count)
	}
}

func TestDelGroupPropagatesChildQueryError(t *testing.T) {
	initGroupTestDB(t)
	group := insertTestGroup(t, 1, "queryfail")
	if _, err := db.Instance.Exec(`ALTER TABLE "group" RENAME TO group_backup`); err != nil {
		t.Fatal(err)
	}

	deleted, err := DelGroup(&context.Context{UserID: 1}, group.ID)
	if err == nil || deleted {
		t.Fatalf("DelGroup() = (%v, %v), want query error", deleted, err)
	}
}

func TestGetGroupStatusUsesFullPath(t *testing.T) {
	initGroupTestDB(t)
	first := insertTestChildGroup(t, 1, "leaf", 0, "first/leaf")
	second := insertTestChildGroup(t, 1, "leaf", 0, "second/leaf")
	insertTestUserEmail(t, 1, 401, first.ID)
	insertTestUserEmail(t, 1, 402, second.ID)

	_, status, err := GetGroupStatus(&context.Context{UserID: 1}, "first/leaf", []string{"MESSAGES", "UIDVALIDITY"})
	if err != nil {
		t.Fatal(err)
	}
	if status["MESSAGES"] != 1 || status["UIDVALIDITY"] != first.ID {
		t.Fatalf("status = %#v, want group %d", status, first.ID)
	}
}

func TestGetGroupStatusDefaultMailboxExcludesGroupedMail(t *testing.T) {
	initGroupTestDB(t)
	group := insertTestGroup(t, 1, "custom")
	insertTestUserEmail(t, 1, 501, 0)
	insertTestUserEmail(t, 1, 502, group.ID)

	_, status, err := GetGroupStatus(&context.Context{UserID: 1}, "INBOX", []string{"MESSAGES"})
	if err != nil {
		t.Fatal(err)
	}
	if status["MESSAGES"] != 1 {
		t.Fatalf("status = %#v, want one inbox message", status)
	}
}

func TestCreateGroupRejectsReservedRootName(t *testing.T) {
	initGroupTestDB(t)

	created, err := CreateGroup(&context.Context{UserID: 1}, "inbox", 0)
	if err == nil || created != nil {
		t.Fatalf("CreateGroup() = (%v, %v), want reserved name error", created, err)
	}
}

func TestGetGroupInfoListPropagatesQueryError(t *testing.T) {
	initGroupTestDB(t)
	if _, err := db.Instance.Exec(`ALTER TABLE "group" RENAME TO group_backup`); err != nil {
		t.Fatal(err)
	}

	items, err := GetGroupInfoList(&context.Context{UserID: 1})
	if err == nil || items != nil {
		t.Fatalf("GetGroupInfoList() = (%v, %v), want query error", items, err)
	}
}

func TestGetGroupListPropagatesQueryError(t *testing.T) {
	initGroupTestDB(t)
	if _, err := db.Instance.Exec(`ALTER TABLE "group" RENAME TO group_backup`); err != nil {
		t.Fatal(err)
	}

	items, err := GetGroupList(&context.Context{UserID: 1})
	if err == nil || items != nil {
		t.Fatalf("GetGroupList() = (%v, %v), want query error", items, err)
	}
}

func TestGetGroupInfoListRejectsExcessiveDepth(t *testing.T) {
	initGroupTestDB(t)
	parentID := 0
	fullPath := ""
	for index := 0; index <= maxGroupDepth; index++ {
		name := fmt.Sprintf("g%d", index)
		if fullPath == "" {
			fullPath = name
		} else {
			fullPath += "/" + name
		}
		group := insertTestChildGroup(t, 1, name, parentID, fullPath)
		parentID = group.ID
	}

	items, err := GetGroupInfoList(&context.Context{UserID: 1})
	if err == nil || items != nil {
		t.Fatalf("GetGroupInfoList() = (%v, %v), want depth error", items, err)
	}
}

func TestRenameUpdatesDescendantPaths(t *testing.T) {
	initGroupTestDB(t)
	root := insertTestGroup(t, 1, "oldroot")
	child := insertTestChildGroup(t, 1, "child", root.ID, "oldroot/child")
	grandchild := insertTestChildGroup(t, 1, "leaf", child.ID, "oldroot/child/leaf")
	foreign := insertTestChildGroup(t, 2, "child", 0, "oldroot/child")

	if err := Rename(&context.Context{UserID: 1}, "oldroot", "newroot"); err != nil {
		t.Fatal(err)
	}

	wants := map[int]string{
		root.ID:       "newroot",
		child.ID:      "newroot/child",
		grandchild.ID: "newroot/child/leaf",
	}
	for id, want := range wants {
		var group models.Group
		found, err := db.Instance.Table("group").Where("id = ? and user_id = ?", id, 1).Get(&group)
		if err != nil {
			t.Fatal(err)
		}
		if !found || group.FullPath != want {
			t.Fatalf("group %d path = %q, want %q", id, group.FullPath, want)
		}
	}

	var foreignAfter models.Group
	found, err := db.Instance.Table("group").Where("id = ? and user_id = ?", foreign.ID, 2).Get(&foreignAfter)
	if err != nil {
		t.Fatal(err)
	}
	if !found || foreignAfter.FullPath != foreign.FullPath {
		t.Fatalf("foreign group path = %q, want %q", foreignAfter.FullPath, foreign.FullPath)
	}
}

func TestRenameRejectsDescendantPathConflict(t *testing.T) {
	initGroupTestDB(t)
	root := insertTestGroup(t, 1, "oldroot")
	child := insertTestChildGroup(t, 1, "child", root.ID, "oldroot/child")
	insertTestChildGroup(t, 1, "unrelated", 0, "newroot/child")

	if err := Rename(&context.Context{UserID: 1}, "oldroot", "newroot"); err == nil {
		t.Fatal("rename succeeded despite descendant path conflict")
	}

	var rootAfter models.Group
	found, err := db.Instance.Table("group").Where("id = ? and user_id = ?", root.ID, 1).Get(&rootAfter)
	if err != nil {
		t.Fatal(err)
	}
	if !found || rootAfter.Name != "oldroot" || rootAfter.FullPath != "oldroot" {
		t.Fatalf("root changed after rejected rename: %#v", rootAfter)
	}
	var childAfter models.Group
	found, err = db.Instance.Table("group").Where("id = ? and user_id = ?", child.ID, 1).Get(&childAfter)
	if err != nil {
		t.Fatal(err)
	}
	if !found || childAfter.FullPath != "oldroot/child" {
		t.Fatalf("child changed after rejected rename: %#v", childAfter)
	}
}

func TestGetGroupStatusPropagatesQueryError(t *testing.T) {
	initGroupTestDB(t)
	if _, err := db.Instance.Exec(`ALTER TABLE user_email RENAME TO user_email_backup`); err != nil {
		t.Fatal(err)
	}

	statusText, status, err := GetGroupStatus(&context.Context{UserID: 1}, "INBOX", []string{"MESSAGES"})
	if err == nil || statusText != "" || status != nil {
		t.Fatalf("GetGroupStatus() = (%q, %#v, %v), want query error", statusText, status, err)
	}
}

func TestGetGroupStatusRejectsMissingMailbox(t *testing.T) {
	initGroupTestDB(t)
	statusText, status, err := GetGroupStatus(&context.Context{UserID: 1}, "missing", []string{"MESSAGES"})
	if err == nil || statusText != "" || status != nil {
		t.Fatalf("GetGroupStatus() = (%q, %#v, %v), want missing mailbox error", statusText, status, err)
	}
}

func TestCreateGroupRejectsExcessivePathLength(t *testing.T) {
	initGroupTestDB(t)
	ctx := &context.Context{UserID: 1}
	parentID := 0
	for index := 0; index < maxGroupDepth; index++ {
		name := strings.Repeat("a", 9) + fmt.Sprintf("%d", index%10)
		created, err := CreateGroup(ctx, name, parentID)
		if err != nil {
			if index < 54 {
				t.Fatalf("path rejected too early at depth %d: %v", index+1, err)
			}
			return
		}
		parentID = created.ID
	}
	t.Fatal("excessive group path was accepted")
}
