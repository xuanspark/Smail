package detail

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/query"
	"strings"
	"xorm.io/xorm"
)
import . "xorm.io/builder"

func GetEmailDetail(ctx *context.Context, id int, markRead bool) (*response.EmailResponseData, error) {
	if ctx == nil || ctx.UserID <= 0 || id <= 0 || db.Instance == nil {
		return nil, errors.New("Not authorized")
	}
	// 先查是否是本人的邮件
	var ue models.UserEmail
	hasRelation, err := db.Instance.Where("email_id = ? AND user_id = ?", id, ctx.UserID).Get(&ue)
	if err != nil {
		return nil, fmt.Errorf("load email ownership: %w", err)
	}
	if !hasRelation {
		return nil, errors.New("Not authorized")
	}

	//获取邮件内容
	var email response.EmailResponseData
	hasEmail, err := db.Instance.Select("*,1 as is_read").Table("email").Where("id=?", id).Get(&email)
	if err != nil {
		return nil, fmt.Errorf("load email detail: %w", err)
	}
	if !hasEmail {
		return nil, errors.New("Not found")
	}

	email.IsRead = ue.IsRead
	if email.Type == 0 {
		email.Bcc = "[]"
	}

	// 将内容中的cid内容替换成url
	if strings.TrimSpace(email.Attachments) != "" {
		var atts []parsemail.Attachment
		if err := json.Unmarshal([]byte(email.Attachments), &atts); err != nil {
			return nil, fmt.Errorf("decode stored attachments: %w", err)
		}
		for _, att := range atts {
			email.Html = sql.NullString{
				String: strings.ReplaceAll(email.Html.String, fmt.Sprintf("cid:%s", att.ContentID), fmt.Sprintf("/attachments/%d/%s", id, att.ContentID)),
			}
		}
	}

	if markRead && hasRelation && ue.IsRead == 0 {
		ue.IsRead = 1
		affected, err := db.Instance.Where("id=?", ue.ID).Cols("is_read").Update(&ue)
		if err != nil {
			return nil, fmt.Errorf("mark email as read: %w", err)
		}
		if affected != 1 {
			return nil, errors.New("mark email as read failed")
		}
		email.IsRead = 1
	}

	return &email, nil
}

func MakeRead(ctx *context.Context, emailID int, read bool) error {
	var relation models.UserEmail
	found, err := db.Instance.Where("email_id = ? and user_id = ?", emailID, ctx.UserID).Get(&relation)
	if err != nil {
		return fmt.Errorf("load email relation for read update: %w", err)
	}
	if !found || relation.ID == 0 {
		return errors.New("Not authorized")
	}

	desired := int8(0)
	if read {
		desired = 1
	}
	if relation.IsRead == desired {
		return nil
	}

	affected, err := db.Instance.Where("id = ? and user_id = ?", relation.ID, ctx.UserID).Cols("is_read").Update(map[string]interface{}{"is_read": desired})
	if err != nil {
		return fmt.Errorf("update email read state: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("update email read state affected %d rows", affected)
	}
	return nil
}

func MarkEmailsRead(ctx *context.Context, emailIDs []int) error {
	if ctx == nil || ctx.UserID <= 0 || db.Instance == nil {
		return errors.New("Not authorized")
	}

	uniqueIDs := make([]int, 0, len(emailIDs))
	seenIDs := make(map[int]struct{}, len(emailIDs))
	for _, emailID := range emailIDs {
		if emailID <= 0 {
			return errors.New("invalid email id")
		}
		if _, exists := seenIDs[emailID]; exists {
			continue
		}
		seenIDs[emailID] = struct{}{}
		uniqueIDs = append(uniqueIDs, emailID)
	}
	if len(uniqueIDs) == 0 {
		return errors.New("empty email id list")
	}

	placeholders, idArgs, ok := query.IntList(uniqueIDs, 10000)
	if !ok {
		return errors.New("invalid email id list")
	}

	return db.Transaction(db.Instance, func(session *xorm.Session) error {
		var relations []models.UserEmail
		selectArgs := append([]any{ctx.UserID}, idArgs...)
		selectSQL := fmt.Sprintf("SELECT id,email_id,is_read FROM user_email WHERE user_id = ? AND email_id IN (%s)", placeholders)
		if err := session.SQL(selectSQL, selectArgs...).Find(&relations); err != nil {
			return fmt.Errorf("load email ownership for read update: %w", err)
		}

		ownedIDs := make(map[int]struct{}, len(relations))
		for _, relation := range relations {
			ownedIDs[relation.EmailID] = struct{}{}
		}
		for _, emailID := range uniqueIDs {
			if _, owned := ownedIDs[emailID]; !owned {
				return fmt.Errorf("email %d is not owned by current user", emailID)
			}
		}

		updateArgs := append([]any{ctx.UserID}, idArgs...)
		updateSQL := fmt.Sprintf("user_id = ? AND email_id IN (%s) AND is_read <> 1", placeholders)
		if _, err := session.Table("user_email").Where(updateSQL, updateArgs...).Cols("is_read").Update(map[string]any{"is_read": 1}); err != nil {
			return fmt.Errorf("update email read state: %w", err)
		}
		return nil
	})
}

func FindUE(ctx *context.Context, groupName string, req list.ImapListReq, uid bool) ([]models.UserEmail, error) {
	var ue []models.UserEmail
	if uid {
		if len(req.UidList) == 0 || len(req.UidList) > 10000 {
			return nil, fmt.Errorf("invalid UID list")
		}
		for _, value := range req.UidList {
			if value <= 0 {
				return nil, fmt.Errorf("invalid UID")
			}
		}
		if err := db.Instance.Where(Eq{"id": req.UidList, "user_id": ctx.UserID}).Find(&ue); err != nil {
			return nil, fmt.Errorf("query user email by UID: %w", err)
		}
		return ue, nil
	}
	if req.Star < 0 || req.End < 0 || (req.End > 0 && req.Star > req.End) {
		return nil, fmt.Errorf("invalid sequence range")
	}

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

	sqlText := "SELECT id,email_id,is_read FROM (" + inner + ") a WHERE "
	if req.Star > 0 {
		sqlText += "serial_number >= ?"
		params = append(params, req.Star)
		if req.End > 0 {
			sqlText += " and serial_number <= ?"
			params = append(params, req.End)
		}
	} else {
		placeholders, sequenceArgs, ok := query.IntList(req.UidList, 10000)
		if !ok {
			return nil, fmt.Errorf("invalid sequence list")
		}
		sqlText += fmt.Sprintf("serial_number in (%s)", placeholders)
		params = append(params, sequenceArgs...)
	}
	if err := db.Instance.SQL(sqlText, params...).Find(&ue); err != nil {
		return nil, fmt.Errorf("query user email by sequence: %w", err)
	}
	return ue, nil
}
