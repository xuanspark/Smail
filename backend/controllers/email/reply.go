package email

import (
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/validation"
)

func applyReplyThread(ctx *context.Context, input sendRequest, message *parsemail.Email) error {
	id := input.ReplyEmailID
	if message == nil {
		return validation.ErrInvalidEmail
	}
	message.InReplyTo, message.ThreadReferences = "", ""
	if id == 0 {
		return nil
	}
	if id < 0 || ctx == nil || ctx.UserID <= 0 || db.Instance == nil || message == nil {
		return validation.ErrInvalidEmail
	}
	if input.DraftID > 0 {
		session := db.Instance.NewSession()
		draft, err := loadOwnedDraft(session, ctx.UserID, input.DraftID, input.DraftVersion)
		session.Close()
		if err != nil {
			return err
		}
		if draft.ReplyEmailID == id {
			message.InReplyTo = parsemail.NormalizeThreadHeader(draft.InReplyTo)
			message.ThreadReferences = parsemail.NormalizeThreadHeader(draft.ThreadReferences)
			return nil
		}
	}
	var original models.Email
	found, err := db.Instance.ID(id).
		Where("status <> ?", consts.EmailStatusDrafts).
		And("EXISTS (SELECT 1 FROM user_email WHERE email_id = ? AND user_id = ? AND is_deleted = 0)", id, ctx.UserID).
		Get(&original)
	if err != nil {
		return err
	}
	if !found {
		return validation.ErrInvalidEmail
	}
	message.InReplyTo, message.ThreadReferences = parsemail.ReplyThread(original.MsgID, original.ThreadReferences, original.InReplyTo)
	return nil
}
