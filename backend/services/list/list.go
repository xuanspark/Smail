package list

import (
	"fmt"
	"strings"

	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/query"
	. "xorm.io/builder"
)

func GetEmailList(ctx *context.Context, tagInfo dto.SearchTag, keyword string, pop3List bool, offset, limit int) (emailList []*response.EmailResponseData, total int64, err error) {
	return getList(ctx, tagInfo, keyword, pop3List, offset, limit)
}

func getList(ctx *context.Context, tagInfo dto.SearchTag, keyword string, pop3List bool, offset, limit int) (emailList []*response.EmailResponseData, total int64, err error) {
	querySQL, queryParams := genSQL(ctx, false, tagInfo, keyword, pop3List, offset, limit)

	if err := db.Instance.SQL(querySQL, queryParams...).Find(&emailList); err != nil {
		return nil, 0, fmt.Errorf("query email list: %w", err)
	}

	totalSQL, totalParams := genSQL(ctx, true, tagInfo, keyword, pop3List, offset, limit)

	if _, err := db.Instance.SQL(totalSQL, totalParams...).Get(&total); err != nil {
		return nil, 0, fmt.Errorf("query email count: %w", err)
	}

	return emailList, total, nil
}

func genSQL(ctx *context.Context, count bool, tagInfo dto.SearchTag, keyword string, pop3List bool, offset, limit int) (string, []any) {
	sqlParams := []any{ctx.UserID}
	sql := "select "

	if count {
		sql += `count(1) from email e left join user_email ue on e.id=ue.email_id where ue.user_id = ? `
	} else if pop3List {
		sql += `e.id,e.size from email e left join user_email ue on e.id=ue.email_id where ue.user_id = ? `
	} else {
		sql += `e.*,ue.is_read from email e left join user_email ue on e.id=ue.email_id where ue.user_id = ? `
	}

	if tagInfo.Status != -1 {
		switch tagInfo.Status {
		case consts.EmailStatusDel:
			sql += " and (ue.status =? or ue.group_id=?) "
			sqlParams = append(sqlParams, tagInfo.Status, models.Deleted)
		case consts.EmailStatusDrafts:
			sql += " and (ue.status =? or ue.group_id=?) "
			sqlParams = append(sqlParams, tagInfo.Status, models.Drafts)
		case consts.EmailStatusJunk:
			sql += " and (ue.status =? or ue.group_id=?) "
			sqlParams = append(sqlParams, tagInfo.Status, models.Junk)
		}

	} else if tagInfo.Status == -1 {
		if tagInfo.Type != 1 {
			sql += " and ue.status = 0"
		} else {
			// 发件箱不展示已删除的邮件
			sql += " and ue.status != 3"
		}
	}

	if tagInfo.Type == consts.EmailTypeReceive {
		sql += " and type =? "
		sqlParams = append(sqlParams, tagInfo.Type)
	} else if tagInfo.Type == consts.EmailTypeSend {
		sql += " and (type =? or ue.group_id=?)"
		sqlParams = append(sqlParams, tagInfo.Type, models.Sent)
	}

	if tagInfo.GroupId != -1 {
		if tagInfo.GroupId > 0 {
			sql += " and ue.group_id=? "
			sqlParams = append(sqlParams, tagInfo.GroupId)
		} else if tagInfo.GroupId == 0 && tagInfo.Status == -1 {
			sql += " and (ue.group_id=? or ue.group_id=0)"
			sqlParams = append(sqlParams, models.INBOX)
		}
	} else {
		sql += " and (ue.group_id=0 or ue.group_id =?) "
		sqlParams = append(sqlParams, models.INBOX)
	}

	if keyword != "" {
		sql += " and (subject like ? or text like ? )"
		sqlParams = append(sqlParams, "%"+keyword+"%", "%"+keyword+"%")
	}

	if limit == 0 {
		limit = 10
	}

	sql += " order by e.id desc"

	if limit < 10000 {
		sql += fmt.Sprintf(" LIMIT %d OFFSET %d ", limit, offset)
	}

	return sql, sqlParams

}

type statRes struct {
	Total int64
	Size  int64
}

// Stat 查询邮件总数和大小
func Stat(ctx *context.Context) (int64, int64, error) {
	sql := `select count(1) as total,sum(size) as size from email e left join user_email ue on e.id=ue.email_id where ue.user_id = ? and e.type = 0 and ue.status != 3`
	var ret statRes
	if _, err := db.Instance.SQL(sql, ctx.UserID).Get(&ret); err != nil {
		return 0, 0, fmt.Errorf("query mailbox statistics: %w", err)
	}
	return ret.Total, ret.Size, nil
}

type ImapListReq struct {
	UidList []int
	Star    int
	End     int
}

func buildIMAPMailboxRelationQuery(ctx *context.Context, groupName string) (string, []any, error) {
	if ctx == nil || ctx.UserID <= 0 {
		return "", nil, fmt.Errorf("invalid mailbox user")
	}
	inner := "SELECT id,email_id,is_read,is_deleted,status,ROW_NUMBER() OVER (ORDER BY id) AS serial_number FROM user_email WHERE user_id = ? AND email_id > 0"
	params := []any{ctx.UserID}
	switch groupName {
	case "INBOX":
		inner += " and status = ? and group_id = 0"
		params = append(params, 0)
	case "Sent Messages":
		inner += " and status = ? and group_id = 0"
		params = append(params, 1)
	case "Drafts":
		inner += " and status = ? and group_id = 0"
		params = append(params, 4)
	case "Deleted Messages":
		inner += " and status = ? and group_id = 0"
		params = append(params, 3)
	case "Junk":
		inner += " and status = ? and group_id = 0"
		params = append(params, 5)
	default:
		var mailbox models.Group
		found, err := db.Instance.Table("group").Where("user_id=? and full_path=?", ctx.UserID, groupName).Get(&mailbox)
		if err != nil {
			return "", nil, fmt.Errorf("query mailbox: %w", err)
		}
		if !found || mailbox.ID == 0 {
			return "", nil, fmt.Errorf("mailbox not found")
		}
		inner += " and group_id = ?"
		params = append(params, mailbox.ID)
	}
	return inner, params, nil
}

func GetUEListByUID(ctx *context.Context, groupName string, star, end int, uidList []int) ([]*response.UserEmailUIDData, error) {
	if star < 0 || end < 0 || (end > 0 && star > end) {
		return nil, fmt.Errorf("invalid UID range")
	}
	inner, params, err := buildIMAPMailboxRelationQuery(ctx, groupName)
	if err != nil {
		return nil, err
	}
	filters := make([]string, 0, 3)
	if len(uidList) > 0 {
		placeholders, uidArgs, ok := query.IntList(uidList, 10000)
		if !ok {
			return nil, fmt.Errorf("invalid UID list")
		}
		filters = append(filters, fmt.Sprintf("id in (%s)", placeholders))
		params = append(params, uidArgs...)
	}
	if star > 0 {
		filters = append(filters, "id >= ?")
		params = append(params, star)
	}
	if end > 0 {
		filters = append(filters, "id <= ?")
		params = append(params, end)
	}
	sql := "SELECT * FROM (" + inner + ") mailbox"
	if len(filters) > 0 {
		sql += " WHERE " + strings.Join(filters, " AND ")
	}
	sql += " ORDER BY id"
	var relations []*response.UserEmailUIDData
	if err := db.Instance.SQL(sql, params...).Find(&relations); err != nil {
		return nil, fmt.Errorf("query mailbox UID relations: %w", err)
	}
	return relations, nil
}

func GetMaxUIDByMailbox(ctx *context.Context, groupName string) (int, error) {
	inner, params, err := buildIMAPMailboxRelationQuery(ctx, groupName)
	if err != nil {
		return 0, err
	}
	var maxUID int
	if _, err := db.Instance.SQL("SELECT COALESCE(MAX(id), 0) FROM ("+inner+") mailbox", params...).Get(&maxUID); err != nil {
		return 0, fmt.Errorf("query mailbox maximum UID: %w", err)
	}
	return maxUID, nil
}

func getEmailListByUidList(ctx *context.Context, groupName string, req ImapListReq) ([]*response.EmailResponseData, error) {
	if req.Star < 0 || req.End < 0 || (req.End > 0 && req.Star > req.End) {
		return nil, fmt.Errorf("invalid UID range")
	}
	inner, params, err := buildIMAPMailboxRelationQuery(ctx, groupName)
	if err != nil {
		return nil, err
	}
	sql := "SELECT * FROM (" + inner + ") mailbox WHERE "
	if req.Star == 0 && req.End == 0 && len(req.UidList) == 0 {
		maxUID, err := GetMaxUIDByMailbox(ctx, groupName)
		if err != nil {
			return nil, err
		}
		if maxUID == 0 {
			return []*response.EmailResponseData{}, nil
		}
		sql += "id = ?"
		params = append(params, maxUID)
		sql += " ORDER BY id"
		var relations []*response.UserEmailUIDData
		if err := db.Instance.SQL(sql, params...).Find(&relations); err != nil {
			return nil, fmt.Errorf("query mailbox wildcard UID: %w", err)
		}
		return hydrateIMAPEmailResponses(relations)
	}
	if req.Star > 0 {
		sql += "id >= ?"
		params = append(params, req.Star)
		if req.End > 0 {
			sql += " and id <= ?"
			params = append(params, req.End)
		}
	} else {
		placeholders, uidArgs, ok := query.IntList(req.UidList, 10000)
		if !ok {
			return nil, fmt.Errorf("invalid UID list")
		}
		sql += fmt.Sprintf("id in (%s)", placeholders)
		params = append(params, uidArgs...)
	}
	sql += " ORDER BY id"
	var relations []*response.UserEmailUIDData
	if err := db.Instance.SQL(sql, params...).Find(&relations); err != nil {
		return nil, fmt.Errorf("query mailbox UID list: %w", err)
	}
	return hydrateIMAPEmailResponses(relations)
}

func GetEmailListByGroup(ctx *context.Context, groupName string, req ImapListReq, uid bool) ([]*response.EmailResponseData, error) {
	if len(req.UidList) == 0 && req.Star == 0 && req.End == 0 && !uid {
		return nil, nil
	}
	if uid {
		return getEmailListByUidList(ctx, groupName, req)
	}
	if req.Star < 0 || req.End < 0 || (req.End > 0 && req.Star > req.End) {
		return nil, fmt.Errorf("invalid sequence range")
	}
	inner, params, err := buildIMAPMailboxRelationQuery(ctx, groupName)
	if err != nil {
		return nil, err
	}
	sql := "SELECT * FROM (" + inner + ") mailbox WHERE "
	if req.Star > 0 {
		sql += "serial_number >= ?"
		params = append(params, req.Star)
		if req.End > 0 {
			sql += " and serial_number <= ?"
			params = append(params, req.End)
		}
	} else {
		placeholders, sequenceArgs, ok := query.IntList(req.UidList, 10000)
		if !ok {
			return nil, fmt.Errorf("invalid sequence list")
		}
		sql += fmt.Sprintf("serial_number in (%s)", placeholders)
		params = append(params, sequenceArgs...)
	}
	sql += " ORDER BY serial_number"
	var relations []*response.UserEmailUIDData
	if err := db.Instance.SQL(sql, params...).Find(&relations); err != nil {
		return nil, fmt.Errorf("query mailbox sequence list: %w", err)
	}
	return hydrateIMAPEmailResponses(relations)
}

func hydrateIMAPEmailResponses(relations []*response.UserEmailUIDData) ([]*response.EmailResponseData, error) {
	result := make([]*response.EmailResponseData, 0, len(relations))
	if len(relations) == 0 {
		return result, nil
	}
	emailIDs := make([]int, 0, len(relations))
	for _, relation := range relations {
		if relation == nil || relation.EmailID <= 0 {
			continue
		}
		emailIDs = append(emailIDs, relation.EmailID)
	}
	if len(emailIDs) == 0 {
		return result, nil
	}
	var stored []*response.EmailResponseData
	if err := db.Instance.Table("email").Select("*").Where(Eq{"id": emailIDs}).Find(&stored); err != nil {
		return nil, fmt.Errorf("query mailbox messages: %w", err)
	}
	messageByID := make(map[int]*response.EmailResponseData, len(stored))
	for _, message := range stored {
		if message != nil {
			messageByID[message.Id] = message
		}
	}
	for _, relation := range relations {
		message := messageByID[relation.EmailID]
		if message == nil {
			continue
		}
		copyMessage := *message
		copyMessage.IsRead = relation.IsRead
		copyMessage.IsDeleted = relation.IsDeleted
		copyMessage.SerialNumber = relation.SerialNumber
		copyMessage.UeId = relation.ID
		result = append(result, &copyMessage)
	}
	return result, nil
}
