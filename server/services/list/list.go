package list

import (
	"fmt"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/query"
)
import . "xorm.io/builder"

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

func GetUEListByUID(ctx *context.Context, groupName string, star, end int, uidList []int) ([]*response.UserEmailUIDData, error) {
	var ue []*response.UserEmailUIDData
	sql := "SELECT id,email_id, is_read, ROW_NUMBER() OVER (ORDER BY id) AS serial_number FROM `user_email` WHERE user_id = ? "

	params := []any{ctx.UserID}

	if len(uidList) > 0 {
		placeholders, uidArgs, ok := query.IntList(uidList, 10000)
		if !ok {
			return nil, fmt.Errorf("invalid UID list")
		}
		sql += fmt.Sprintf(" and id in (%s)", placeholders)
		params = append(params, uidArgs...)
	}
	if star > 0 {
		sql += " and id >=?"
		params = append(params, star)
	}
	if end > 0 {
		sql += " and id <=?"
		params = append(params, end)
	}

	switch groupName {
	case "INBOX":
		sql += " and status =? and group_id = 0"
		params = append(params, 0)
	case "Sent Messages":
		sql += " and status =? and group_id = 0"
		params = append(params, 1)
	case "Drafts":
		sql += " and status =? and group_id = 0"
		params = append(params, 4)
	case "Deleted Messages":
		sql += " and status =? and group_id = 0"
		params = append(params, 3)
	case "Junk":
		sql += " and status =? and group_id = 0"
		params = append(params, 5)
	default:
		var group models.Group
		found, err := db.Instance.Table("group").Where("user_id=? and full_path=?", ctx.UserID, groupName).Get(&group)
		if err != nil {
			return nil, err
		}
		if !found || group.ID == 0 {
			return nil, nil
		}

		sql += " and group_id = ?"
		params = append(params, group.ID)
	}

	if err := db.Instance.SQL(sql, params...).Find(&ue); err != nil {
		return nil, err
	}
	return ue, nil
}

func getEmailListByUidList(ctx *context.Context, groupName string, req ImapListReq) ([]*response.EmailResponseData, error) {
	var ret []*response.EmailResponseData
	var ue []*response.UserEmailUIDData
	if req.Star < 0 || req.End < 0 || (req.End > 0 && req.Star > req.End) {
		return nil, fmt.Errorf("invalid UID range")
	}

	buildQuery := func(filterColumn string, baseParams []any) (string, []any, bool) {
		sql := "SELECT id,email_id,is_read,ROW_NUMBER() OVER (ORDER BY id) AS serial_number FROM `user_email` WHERE user_id = ?"
		params := append([]any{}, baseParams...)
		if filterColumn != "" {
			sql += " and " + filterColumn + " = ?"
			if filterColumn == "status" {
				sql += " and group_id = 0"
			}
		}
		if req.Star > 0 {
			sql += " and id >= ?"
			params = append(params, req.Star)
			if req.End > 0 {
				sql += " and id <= ?"
				params = append(params, req.End)
			}
			return sql, params, true
		}
		placeholders, uidArgs, ok := query.IntList(req.UidList, 10000)
		if !ok {
			return "", nil, false
		}
		sql += fmt.Sprintf(" and id in (%s)", placeholders)
		params = append(params, uidArgs...)
		return sql, params, true
	}

	var sql string
	var params []any
	var ok bool
	switch groupName {
	case "INBOX":
		sql, params, ok = buildQuery("status", []any{ctx.UserID, 0})
	case "Sent Messages":
		sql, params, ok = buildQuery("status", []any{ctx.UserID, 1})
	case "Drafts":
		sql, params, ok = buildQuery("status", []any{ctx.UserID, 4})
	case "Deleted Messages":
		sql, params, ok = buildQuery("status", []any{ctx.UserID, 3})
	case "Junk":
		sql, params, ok = buildQuery("status", []any{ctx.UserID, 5})
	default:
		var group models.Group
		found, err := db.Instance.Table("group").Where("user_id=? and full_path=?", ctx.UserID, groupName).Get(&group)
		if err != nil {
			return nil, fmt.Errorf("query mailbox: %w", err)
		}
		if !found || group.ID == 0 {
			return nil, fmt.Errorf("mailbox not found")
		}
		sql, params, ok = buildQuery("group_id", []any{ctx.UserID, group.ID})
	}
	if !ok {
		return nil, fmt.Errorf("invalid UID list")
	}
	if err := db.Instance.SQL(sql, params...).Find(&ue); err != nil {
		return nil, fmt.Errorf("query mailbox UID list: %w", err)
	}

	ueMap := map[int]*response.UserEmailUIDData{}
	var emailIds []int
	for _, email := range ue {
		ueMap[email.EmailID] = email
		emailIds = append(emailIds, email.EmailID)
	}
	if len(emailIds) == 0 {
		return ret, nil
	}
	if err := db.Instance.Table("email").Select("*").Where(Eq{"id": emailIds}).Find(&ret); err != nil {
		return nil, fmt.Errorf("query mailbox messages: %w", err)
	}
	for i, data := range ret {
		item := ueMap[data.Id]
		if item == nil {
			continue
		}
		ret[i].IsRead = item.IsRead
		ret[i].SerialNumber = item.SerialNumber
		ret[i].UeId = item.ID
	}
	return ret, nil
}

func GetEmailListByGroup(ctx *context.Context, groupName string, req ImapListReq, uid bool) ([]*response.EmailResponseData, error) {
	if len(req.UidList) == 0 && req.Star == 0 && req.End == 0 {
		return nil, nil
	}
	if uid {
		return getEmailListByUidList(ctx, groupName, req)
	}
	if req.Star < 0 || req.End < 0 || (req.End > 0 && req.Star > req.End) {
		return nil, fmt.Errorf("invalid sequence range")
	}

	var ret []*response.EmailResponseData
	var ue []*response.UserEmailUIDData
	inner := "SELECT id,email_id,is_read,ROW_NUMBER() OVER (ORDER BY id) AS serial_number FROM `user_email` WHERE user_id = ?"
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
		var group models.Group
		found, err := db.Instance.Table("group").Where("user_id=? and full_path=?", ctx.UserID, groupName).Get(&group)
		if err != nil {
			return nil, fmt.Errorf("query mailbox: %w", err)
		}
		if !found || group.ID == 0 {
			return nil, fmt.Errorf("mailbox not found")
		}
		inner += " and group_id = ?"
		params = append(params, group.ID)
	}

	sql := "SELECT * FROM (" + inner + ") a WHERE "
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
	if err := db.Instance.SQL(sql, params...).Find(&ue); err != nil {
		return nil, fmt.Errorf("query mailbox sequence list: %w", err)
	}

	ueMap := map[int]*response.UserEmailUIDData{}
	var emailIds []int
	for _, email := range ue {
		ueMap[email.EmailID] = email
		emailIds = append(emailIds, email.EmailID)
	}
	if len(emailIds) == 0 {
		return ret, nil
	}
	if err := db.Instance.Table("email").Select("*").Where(Eq{"id": emailIds}).Find(&ret); err != nil {
		return nil, fmt.Errorf("query mailbox messages: %w", err)
	}
	for i, data := range ret {
		item := ueMap[data.Id]
		if item == nil {
			continue
		}
		ret[i].IsRead = item.IsRead
		ret[i].SerialNumber = item.SerialNumber
		ret[i].UeId = item.ID
	}
	return ret, nil
}
