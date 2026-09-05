package group

import (
	errors2 "errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/query"
	log "github.com/sirupsen/logrus"
	"strings"
	"unicode"
	"unicode/utf8"
	"xorm.io/builder"
	"xorm.io/xorm"
)

type GroupItem struct {
	Id       int          `json:"id"`
	Label    string       `json:"label"`
	Tag      string       `json:"tag"`
	Children []*GroupItem `json:"children"`
}

const (
	maxGroupDepth      = 100
	maxGroupPathLength = 600
)

func CreateGroup(ctx *context.Context, name string, parentId int) (*models.Group, error) {
	name, err := normalizeGroupName(name)
	if err != nil {
		return nil, err
	}
	// 先查询是否存在
	if parentId < 0 {
		return nil, errors2.New("invalid parent group")
	}
	if parentId == 0 && isReservedGroupName(name) {
		return nil, errors2.New("reserved group name")
	}

	group := models.Group{
		Name:     name,
		ParentId: parentId,
		UserId:   ctx.UserID,
	}
	group.FullPath, err = getLayerName(ctx, &group, true)
	if err != nil {
		return nil, err
	}
	if err := validateGroupPath(group.FullPath); err != nil {
		return nil, err
	}

	var existing models.Group
	found, err := db.Instance.Table("group").Where("full_path = ? and user_id = ?", group.FullPath, ctx.UserID).Get(&existing)
	if err != nil {
		return nil, err
	}
	if found && existing.ID > 0 {
		return &existing, nil
	}

	_, err = db.Instance.Insert(&group)
	return &group, err
}

func Rename(ctx *context.Context, oldPath, newName string) error {
	newName, err := normalizeGroupName(newName)
	if err != nil {
		return err
	}
	oldGroupInfo, err := GetGroupByPath(ctx, oldPath)
	if err != nil {
		return err
	}
	if oldGroupInfo == nil || oldGroupInfo.ID == 0 {
		return errors2.New("group not found")
	}
	if oldGroupInfo.ParentId == 0 && isReservedGroupName(newName) {
		return errors2.New("reserved group name")
	}
	if oldGroupInfo.Name == newName {
		return nil
	}
	oldGroupInfo.Name = newName
	oldGroupInfo.FullPath, err = getLayerName(ctx, oldGroupInfo, true)
	if err != nil {
		return err
	}
	if err := validateGroupPath(oldGroupInfo.FullPath); err != nil {
		return err
	}

	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return errors.Wrap(err)
	}

	var groups []models.Group
	if err := session.Table("group").Where("user_id = ?", ctx.UserID).Find(&groups); err != nil {
		rollbackGroupTransaction(session)
		return errors.Wrap(err)
	}
	updates, err := buildRenameUpdates(oldGroupInfo.ID, newName, oldGroupInfo.FullPath, groups)
	if err != nil {
		rollbackGroupTransaction(session)
		return err
	}

	updatedIDs := make(map[int]struct{}, len(updates))
	updatedPaths := make(map[string]int, len(updates))
	for _, update := range updates {
		if ownerID, exists := updatedPaths[update.FullPath]; exists && ownerID != update.ID {
			rollbackGroupTransaction(session)
			return errors2.New("group path conflict")
		}
		updatedIDs[update.ID] = struct{}{}
		updatedPaths[update.FullPath] = update.ID
	}
	for _, group := range groups {
		if _, updated := updatedIDs[group.ID]; updated {
			continue
		}
		if _, conflict := updatedPaths[group.FullPath]; conflict {
			rollbackGroupTransaction(session)
			return errors2.New("group already exists")
		}
	}

	for _, update := range updates {
		values := map[string]any{"full_path": update.FullPath}
		if update.ID == oldGroupInfo.ID {
			values["name"] = newName
		}
		rows, err := session.Table("group").Where("id = ? and user_id = ?", update.ID, ctx.UserID).Update(values)
		if err != nil {
			rollbackGroupTransaction(session)
			return errors.Wrap(err)
		}
		if rows != 1 {
			rollbackGroupTransaction(session)
			return errors2.New("group update failed")
		}
	}

	if err := session.Commit(); err != nil {
		return errors.Wrap(err)
	}
	return nil
}

type groupPathUpdate struct {
	ID       int
	FullPath string
}

type groupPathQueueItem struct {
	ID    int
	Depth int
}

func buildRenameUpdates(rootID int, newName, rootPath string, groups []models.Group) ([]groupPathUpdate, error) {
	if rootID <= 0 {
		return nil, errors2.New("invalid group")
	}
	if err := validateGroupPath(rootPath); err != nil {
		return nil, err
	}

	byID := make(map[int]models.Group, len(groups))
	children := make(map[int][]models.Group)
	for _, group := range groups {
		if group.ID <= 0 {
			return nil, errors2.New("invalid group hierarchy")
		}
		if _, exists := byID[group.ID]; exists {
			return nil, errors2.New("duplicate group id")
		}
		byID[group.ID] = group
		children[group.ParentId] = append(children[group.ParentId], group)
	}
	root, exists := byID[rootID]
	if !exists {
		return nil, errors2.New("group not found")
	}
	if root.ParentId != 0 {
		if _, exists := byID[root.ParentId]; !exists {
			return nil, errors2.New("group parent not found")
		}
	}

	paths := map[int]string{rootID: rootPath}
	visited := make(map[int]struct{}, len(groups))
	queue := []groupPathQueueItem{{ID: rootID, Depth: 1}}
	updates := make([]groupPathUpdate, 0)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.Depth > maxGroupDepth {
			return nil, errors2.New("group hierarchy is too deep")
		}
		if _, exists := visited[current.ID]; exists {
			return nil, errors2.New("group hierarchy cycle detected")
		}
		visited[current.ID] = struct{}{}
		updates = append(updates, groupPathUpdate{ID: current.ID, FullPath: paths[current.ID]})

		for _, child := range children[current.ID] {
			childName, err := normalizeGroupName(child.Name)
			if err != nil || childName != child.Name {
				return nil, errors2.New("invalid group hierarchy")
			}
			childPath := paths[current.ID] + "/" + childName
			if err := validateGroupPath(childPath); err != nil {
				return nil, err
			}
			paths[child.ID] = childPath
			queue = append(queue, groupPathQueueItem{ID: child.ID, Depth: current.Depth + 1})
		}
	}

	if len(updates) == 0 || updates[0].ID != rootID || newName == "" {
		return nil, errors2.New("invalid group rename")
	}
	return updates, nil
}

func GetGroupByName(ctx *context.Context, name string) (*models.Group, error) {
	var groups []models.Group
	if err := db.Instance.Table("group").Where("name = ? and user_id = ?", name, ctx.UserID).Limit(2).Find(&groups); err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return &models.Group{}, nil
	}
	if len(groups) > 1 {
		return nil, errors2.New("group name is ambiguous")
	}
	return &groups[0], nil
}

func GetGroupByPath(ctx *context.Context, path string) (*models.Group, error) {
	group, err := GetGroupByFullPath(ctx, path)
	if err != nil || group.ID > 0 || strings.Contains(path, "/") {
		return group, err
	}
	return GetGroupByName(ctx, path)
}

func GetGroupByFullPath(ctx *context.Context, fullPath string) (*models.Group, error) {
	var group models.Group
	_, err := db.Instance.Table("group").Where("full_path = ? and user_id = ?", fullPath, ctx.UserID).Get(&group)

	return &group, err
}

func DelGroup(ctx *context.Context, groupId int) (bool, error) {
	if groupId <= 0 {
		return false, errors2.New("invalid group")
	}
	var root models.Group
	found, err := db.Instance.Table("group").Where("id = ? and user_id = ?", groupId, ctx.UserID).Get(&root)
	if err != nil {
		return false, errors.Wrap(err)
	}
	if !found {
		return false, nil
	}

	allGroupIds, err := getAllChildIDs(ctx, groupId)
	if err != nil {
		return false, err
	}
	allGroupIds = append(allGroupIds, groupId)
	placeholders, groupArgs, ok := query.IntList(allGroupIds, 10000)
	if !ok {
		return false, errors2.New("invalid group list")
	}

	// 开启一个事务
	trans := db.Instance.NewSession()
	defer trans.Close()
	if err := trans.Begin(); err != nil {
		return false, errors.Wrap(err)
	}

	deleteArgs := append(append([]any{}, groupArgs...), ctx.UserID)
	deleteExecArgs := append([]any{db.WithContext(ctx, fmt.Sprintf("delete from `group` where id in (%s) and user_id =?", placeholders))}, deleteArgs...)
	res, err := trans.Exec(deleteExecArgs...)
	if err != nil {
		rollbackGroupTransaction(trans)
		return false, errors.Wrap(err)
	}
	num, err := res.RowsAffected()
	if err != nil {
		rollbackGroupTransaction(trans)
		return false, errors.Wrap(err)
	}

	updateArgs := append(append([]any{}, groupArgs...), ctx.UserID)
	updateExecArgs := append([]any{db.WithContext(ctx, fmt.Sprintf("update user_email set group_id=0 where group_id in (%s) and user_id=?", placeholders))}, updateArgs...)
	_, err = trans.Exec(updateExecArgs...)
	if err != nil {
		rollbackGroupTransaction(trans)
		return false, errors.Wrap(err)
	}

	if err := trans.Commit(); err != nil {
		return false, errors.Wrap(err)
	}

	return num > 0, nil
}

func rollbackGroupTransaction(session *xorm.Session) {
	if err := session.Rollback(); err != nil {
		log.WithError(err).Error("rollback group transaction failed")
	}
}

type id struct {
	Id int `db:"id"`
}

func getAllChildIDs(ctx *context.Context, rootID int) ([]int, error) {
	visited := map[int]struct{}{rootID: {}}
	queue := []int{rootID}
	ret := make([]int, 0)

	for len(queue) > 0 {
		currentID := queue[0]
		queue = queue[1:]

		var ids []id
		if err := db.Instance.Table("group").Where("parent_id=? and user_id=?", currentID, ctx.UserID).Find(&ids); err != nil {
			return nil, errors.Wrap(err)
		}
		for _, item := range ids {
			if item.Id <= 0 {
				return nil, errors2.New("invalid group hierarchy")
			}
			if _, exists := visited[item.Id]; exists {
				return nil, errors2.New("group hierarchy cycle detected")
			}
			visited[item.Id] = struct{}{}
			ret = append(ret, item.Id)
			queue = append(queue, item.Id)
		}
	}

	return ret, nil
}

// GetGroupInfoList 获取全部的分组
func GetGroupInfoList(ctx *context.Context) ([]*GroupItem, error) {
	return buildChildren(ctx, 0, make(map[int]struct{}), 0)
}

// MoveMailToGroup 将某封邮件移动到某个分组中
func MoveMailToGroup(ctx *context.Context, mailIDs []int, groupID int) error {
	if groupID <= 0 {
		return errors2.New("invalid message move request")
	}
	uniqueIDs, err := normalizeMoveMessageIDs(mailIDs)
	if err != nil {
		return err
	}

	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin message move transaction: %w", err)
	}

	var target models.Group
	found, err := session.Table("group").Where("id = ? and user_id = ?", groupID, ctx.UserID).Get(&target)
	if err != nil {
		rollbackGroupTransaction(session)
		return fmt.Errorf("load move destination: %w", err)
	}
	if !found || target.ID == 0 {
		rollbackGroupTransaction(session)
		return errors2.New("move destination not found")
	}

	var relations []models.UserEmail
	if err := session.Table(&models.UserEmail{}).Where(builder.Eq{
		"email_id": uniqueIDs,
		"user_id":  ctx.UserID,
	}).Find(&relations); err != nil {
		rollbackGroupTransaction(session)
		return fmt.Errorf("load message ownership: %w", err)
	}
	ownedIDs := make(map[int]struct{}, len(relations))
	for _, relation := range relations {
		ownedIDs[relation.EmailID] = struct{}{}
	}
	if len(ownedIDs) != len(uniqueIDs) {
		rollbackGroupTransaction(session)
		return errors2.New("one or more messages are not owned by the user")
	}

	expectedChanges := 0
	for _, relation := range relations {
		if relation.GroupId != groupID {
			expectedChanges++
		}
	}
	if expectedChanges > 0 {
		affected, err := session.Table(&models.UserEmail{}).Where(builder.Eq{
			"email_id": uniqueIDs,
			"user_id":  ctx.UserID,
		}).Cols("group_id").Update(map[string]interface{}{"group_id": groupID})
		if err != nil {
			rollbackGroupTransaction(session)
			return fmt.Errorf("move messages: %w", err)
		}
		if affected != int64(expectedChanges) {
			rollbackGroupTransaction(session)
			return fmt.Errorf("move messages affected %d rows, expected %d", affected, expectedChanges)
		}
	}

	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit message move transaction: %w", err)
	}
	return nil
}

// MoveRelationsToGroup 按当前用户的邮箱关系 ID 移动邮件，避免同一封邮件的多个关系互相影响。
func MoveRelationsToGroup(ctx *context.Context, relationIDs []int, groupID int) error {
	if groupID <= 0 {
		return errors2.New("invalid message move request")
	}
	uniqueIDs, err := normalizeMoveMessageIDs(relationIDs)
	if err != nil {
		return err
	}
	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin relation move transaction: %w", err)
	}
	var target models.Group
	found, err := session.Table("group").Where("id = ? and user_id = ?", groupID, ctx.UserID).Get(&target)
	if err != nil || !found {
		rollbackGroupTransaction(session)
		if err != nil {
			return fmt.Errorf("load move destination: %w", err)
		}
		return errors2.New("move destination not found")
	}
	var relations []models.UserEmail
	if err := session.Table(&models.UserEmail{}).Where(builder.Eq{"id": uniqueIDs, "user_id": ctx.UserID}).And("email_id > 0").Find(&relations); err != nil {
		rollbackGroupTransaction(session)
		return fmt.Errorf("load relation ownership: %w", err)
	}
	if len(relations) != len(uniqueIDs) {
		rollbackGroupTransaction(session)
		return errors2.New("one or more messages are not owned by the user")
	}
	if _, err := session.Table(&models.UserEmail{}).Where(builder.Eq{"id": uniqueIDs, "user_id": ctx.UserID}).Cols("group_id").Update(map[string]interface{}{"group_id": groupID}); err != nil {
		rollbackGroupTransaction(session)
		return fmt.Errorf("move relations: %w", err)
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit relation move transaction: %w", err)
	}
	return nil
}
func buildChildren(ctx *context.Context, parentId int, visited map[int]struct{}, depth int) ([]*GroupItem, error) {
	if depth >= maxGroupDepth {
		return nil, errors2.New("group hierarchy is too deep")
	}
	var ret []*GroupItem
	var rootGroup []*models.Group
	if err := db.Instance.Table("group").Where("parent_id=? and user_id=?", parentId, ctx.UserID).Find(&rootGroup); err != nil {
		return nil, errors.Wrap(err)
	}

	for _, group := range rootGroup {
		if group == nil || group.ID <= 0 {
			return nil, errors2.New("invalid group hierarchy")
		}
		if _, exists := visited[group.ID]; exists {
			return nil, errors2.New("group hierarchy cycle detected")
		}
		visited[group.ID] = struct{}{}
		children, err := buildChildren(ctx, group.ID, visited, depth+1)
		if err != nil {
			return nil, err
		}
		ret = append(ret, &GroupItem{
			Id:       group.ID,
			Label:    group.Name,
			Tag:      dto.SearchTag{GroupId: group.ID, Status: -1, Type: -1}.ToString(),
			Children: children,
		})
	}

	return ret, nil
}

func GetGroupList(ctx *context.Context) ([]*models.Group, error) {
	var ret []*models.Group
	if err := db.Instance.Table("group").Where("user_id=?", ctx.UserID).Find(&ret); err != nil {
		return nil, errors.Wrap(err)
	}
	return ret, nil
}

func hasChildren(ctx *context.Context, id int) bool {
	var parent []*models.Group
	if err := db.Instance.Table("group").Where("parent_id=? and user_id=?", id, ctx.UserID).Find(&parent); err != nil {
		log.WithContext(ctx).WithError(err).Error("query child groups failed")
		return false
	}
	return len(parent) > 0
}

func getLayerName(ctx *context.Context, item *models.Group, allPath bool) (string, error) {
	return getLayerNameWithVisited(ctx, item, allPath, make(map[int]struct{}), 0)
}

func getLayerNameWithVisited(ctx *context.Context, item *models.Group, allPath bool, visited map[int]struct{}, depth int) (string, error) {
	if depth >= maxGroupDepth {
		return "", errors2.New("group hierarchy is too deep")
	}
	if item == nil || item.UserId != ctx.UserID {
		return "", errors2.New("invalid group")
	}
	if item.ID > 0 {
		if _, exists := visited[item.ID]; exists {
			return "", errors2.New("group hierarchy cycle detected")
		}
		visited[item.ID] = struct{}{}
	}
	if item.ParentId == 0 {
		return item.Name, nil
	}
	var parent models.Group
	found, err := db.Instance.Table("group").Where("id=? and user_id=?", item.ParentId, ctx.UserID).Get(&parent)
	if err != nil {
		return "", err
	}
	if !found || parent.ID == 0 {
		return "", errors2.New("group parent not found")
	}
	parentName, err := getLayerNameWithVisited(ctx, &parent, allPath, visited, depth+1)
	if err != nil {
		return "", err
	}
	if allPath {
		return parentName + "/" + item.Name, nil
	}
	return parentName, nil
}

func normalizeGroupName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors2.New("invalid group name")
	}
	if utf8.RuneCountInString(name) > 10 {
		return "", errors2.New("group name is too long")
	}
	return name, nil
}

func validateGroupPath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return errors2.New("invalid group path")
	}
	if utf8.RuneCountInString(path) > maxGroupPathLength {
		return errors2.New("group path is too long")
	}
	return nil
}

func IsDefaultBox(box string) bool {
	return array.InArray(box, []string{"INBOX", "Sent Messages", "Drafts", "Deleted Messages", "Junk"})
}

func isReservedGroupName(name string) bool {
	for _, reserved := range []string{"INBOX", "Sent Messages", "Drafts", "Deleted Messages", "Junk"} {
		if strings.EqualFold(name, reserved) {
			return true
		}
	}
	return false
}

func GetGroupStatus(ctx *context.Context, groupName string, params []string) (string, map[string]int, error) {
	retMap := make(map[string]int, len(params))
	parts := make([]string, 0, len(params))

	if !IsDefaultBox(groupName) {
		var group models.Group
		found, err := db.Instance.Table("group").Where("user_id=? and full_path=?", ctx.UserID, groupName).Get(&group)
		if err != nil {
			return "", nil, errors.Wrap(err)
		}
		if !found || group.ID == 0 {
			return "", nil, errors2.New("group not found")
		}
		for _, param := range params {
			var value int
			var err error
			switch param {
			case "MESSAGES":
				value, err = readGroupValue(db.Instance.Table("user_email").Select("count(1)").Where("group_id=? and user_id=? and email_id > 0", group.ID, ctx.UserID), "query mailbox message count failed")
			case "UIDNEXT":
				value, err = readGroupValue(db.Instance.Table("user_email").Select("id").Where("group_id=? and user_id=?", group.ID, ctx.UserID).OrderBy("id desc"), "query mailbox next UID failed")
				value++
			case "UIDVALIDITY":
				value = group.ID
			case "UNSEEN":
				value, err = readGroupValue(db.Instance.Table("user_email").Select("count(1)").Where("group_id=? and user_id=? and email_id > 0 and is_read=0", group.ID, ctx.UserID), "query mailbox unseen count failed")
			default:
				continue
			}
			if err != nil {
				return "", nil, err
			}
			retMap[param] = value
			parts = append(parts, fmt.Sprintf("%s %d", param, value))
		}
		return formatGroupStatus(parts), retMap, nil
	}

	for _, param := range params {
		var value int
		var err error
		switch param {
		case "MESSAGES":
			value, err = getGroupNum(ctx, groupName, false)
		case "UIDNEXT":
			value, err = getNextUID(ctx, groupName)
		case "UIDVALIDITY":
			value = models.GroupNameToCode[groupName]
		case "UNSEEN":
			value, err = getGroupNum(ctx, groupName, true)
		default:
			continue
		}
		if err != nil {
			return "", nil, err
		}
		retMap[param] = value
		parts = append(parts, fmt.Sprintf("%s %d", param, value))
	}
	return formatGroupStatus(parts), retMap, nil
}

func formatGroupStatus(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " ") + ")"
}

func readGroupValue(querySession *xorm.Session, operation string) (int, error) {
	var value int
	if _, err := querySession.Get(&value); err != nil {
		return 0, fmt.Errorf("%s: %w", operation, err)
	}
	return value, nil
}

func getNextUID(ctx *context.Context, groupName string) (int, error) {
	if !IsDefaultBox(groupName) {
		return 0, errors2.New("invalid default mailbox")
	}
	querySession := db.Instance.Table("user_email").Select("id").Where("user_id=? and email_id > 0", ctx.UserID).OrderBy("id desc")
	value, err := readGroupValue(querySession, "query next mailbox UID failed")
	if err != nil {
		return 0, err
	}
	return value + 1, nil
}

func getGroupNum(ctx *context.Context, groupName string, mustUnread bool) (int, error) {
	var querySession *xorm.Session
	switch groupName {
	case "INBOX":
		if mustUnread {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=0 and email_id > 0 and is_read=0", ctx.UserID)
		} else {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=0 and email_id > 0", ctx.UserID)
		}
	case "Sent Messages":
		if mustUnread {
			return 0, nil
		}
		querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=1 and email_id > 0", ctx.UserID)
	case "Drafts":
		if mustUnread {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=4 and email_id > 0 and is_read=0", ctx.UserID)
		} else {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=4 and email_id > 0", ctx.UserID)
		}
	case "Deleted Messages":
		if mustUnread {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=3 and email_id > 0 and is_read=0", ctx.UserID)
		} else {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=3 and email_id > 0", ctx.UserID)
		}
	case "Junk":
		if mustUnread {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=5 and email_id > 0 and is_read=0", ctx.UserID)
		} else {
			querySession = db.Instance.Table("user_email").Select("count(1)").Where("user_id=? and group_id=0 and status=5 and email_id > 0", ctx.UserID)
		}
	default:
		return 0, errors2.New("invalid default mailbox")
	}
	return readGroupValue(querySession, "query mailbox count failed")
}

func normalizeMoveMessageIDs(mailIDs []int) ([]int, error) {
	if len(mailIDs) == 0 {
		return nil, errors2.New("empty message ID list")
	}
	uniqueIDs := make([]int, 0, len(mailIDs))
	seen := make(map[int]struct{}, len(mailIDs))
	for _, id := range mailIDs {
		if id <= 0 {
			return nil, errors2.New("invalid message ID")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) > 10000 {
		return nil, errors2.New("too many message IDs")
	}
	return uniqueIDs, nil
}

func Move2DefaultBox(ctx *context.Context, mailIDs []int, groupName string) error {
	var status int8
	switch groupName {
	case "INBOX":
		status = consts.EmailTypeReceive
	case "Sent Messages":
		status = consts.EmailStatusSent
	case "Drafts":
		status = consts.EmailStatusDrafts
	case "Deleted Messages":
		status = consts.EmailStatusDel
	case "Junk":
		status = consts.EmailStatusJunk
	default:
		return errors2.New("invalid default mailbox")
	}

	uniqueIDs, err := normalizeMoveMessageIDs(mailIDs)
	if err != nil {
		return err
	}
	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin default mailbox move transaction: %w", err)
	}

	var relations []models.UserEmail
	if err := session.Table(&models.UserEmail{}).Where(builder.Eq{
		"email_id": uniqueIDs,
		"user_id":  ctx.UserID,
	}).Find(&relations); err != nil {
		rollbackGroupTransaction(session)
		return fmt.Errorf("load default mailbox move ownership: %w", err)
	}
	if len(relations) != len(uniqueIDs) {
		rollbackGroupTransaction(session)
		return errors2.New("one or more messages are not owned by the user")
	}

	expectedChanges := 0
	for _, relation := range relations {
		if relation.Status != status || relation.GroupId != 0 {
			expectedChanges++
		}
	}
	if expectedChanges > 0 {
		affected, err := session.Table(&models.UserEmail{}).Where(builder.Eq{
			"email_id": uniqueIDs,
			"user_id":  ctx.UserID,
		}).Cols("status", "group_id").Update(map[string]interface{}{
			"status":   status,
			"group_id": 0,
		})
		if err != nil {
			rollbackGroupTransaction(session)
			return fmt.Errorf("move messages to default mailbox: %w", err)
		}
		if affected != int64(expectedChanges) {
			rollbackGroupTransaction(session)
			return fmt.Errorf("default mailbox move affected %d rows, expected %d", affected, expectedChanges)
		}
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit default mailbox move transaction: %w", err)
	}
	return nil
}

// MoveRelationsToDefaultBox 按当前用户的邮箱关系 ID 移动邮件到系统邮箱。
func MoveRelationsToDefaultBox(ctx *context.Context, relationIDs []int, groupName string) error {
	var status int8
	switch groupName {
	case "INBOX":
		status = consts.EmailTypeReceive
	case "Sent Messages":
		status = consts.EmailStatusSent
	case "Drafts":
		status = consts.EmailStatusDrafts
	case "Deleted Messages":
		status = consts.EmailStatusDel
	case "Junk":
		status = consts.EmailStatusJunk
	default:
		return errors2.New("invalid default mailbox")
	}
	uniqueIDs, err := normalizeMoveMessageIDs(relationIDs)
	if err != nil {
		return err
	}
	session := db.Instance.NewSession()
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin relation default mailbox move transaction: %w", err)
	}
	var relations []models.UserEmail
	if err := session.Table(&models.UserEmail{}).Where(builder.Eq{"id": uniqueIDs, "user_id": ctx.UserID}).And("email_id > 0").Find(&relations); err != nil {
		rollbackGroupTransaction(session)
		return fmt.Errorf("load relation default mailbox ownership: %w", err)
	}
	if len(relations) != len(uniqueIDs) {
		rollbackGroupTransaction(session)
		return errors2.New("one or more messages are not owned by the user")
	}
	if _, err := session.Table(&models.UserEmail{}).Where(builder.Eq{"id": uniqueIDs, "user_id": ctx.UserID}).Cols("status", "group_id").Update(map[string]interface{}{"status": status, "group_id": 0}); err != nil {
		rollbackGroupTransaction(session)
		return fmt.Errorf("move relations to default mailbox: %w", err)
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit relation default mailbox move transaction: %w", err)
	}
	return nil
}
