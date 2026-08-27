package attachments

import (
	"encoding/json"
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/context"
	"strings"
)

var ErrAttachmentUnavailable = errors.New("attachment unavailable")

func GetAttachments(ctx *context.Context, emailId int, cid string) (string, []byte, error) {
	if emailId <= 0 || strings.TrimSpace(cid) == "" {
		return "", nil, ErrAttachmentUnavailable
	}

	// 获取邮件内容
	email, err := loadAuthorizedEmail(ctx, emailId)
	if err != nil {
		return "", nil, err
	}

	// 检查权限

	atts, err := decodeAttachments(email.Attachments)
	if err != nil {
		return "", nil, err
	}
	for _, att := range atts {
		if att.ContentID == cid {
			return att.ContentType, att.Content, nil
		}
	}
	return "", nil, ErrAttachmentUnavailable
}

func GetAttachmentsByIndex(ctx *context.Context, emailId int, index int) (string, []byte, error) {
	if emailId <= 0 || index < 0 {
		return "", nil, ErrAttachmentUnavailable
	}

	// 获取邮件内容
	email, err := loadAuthorizedEmail(ctx, emailId)
	if err != nil {
		return "", nil, err
	}

	// 检查权限

	atts, err := decodeAttachments(email.Attachments)
	if err != nil {
		return "", nil, err
	}

	if index < len(atts) {
		return atts[index].Filename, atts[index].Content, nil
	}
	return "", nil, ErrAttachmentUnavailable
}

func loadAuthorizedEmail(ctx *context.Context, emailID int) (models.Email, error) {
	var email models.Email
	has, err := db.Instance.ID(emailID).Get(&email)
	if err != nil {
		return models.Email{}, fmt.Errorf("load attachment email: %w", err)
	}
	if !has || !auth.HasAuth(ctx, &email) {
		return models.Email{}, ErrAttachmentUnavailable
	}
	return email, nil
}

func decodeAttachments(raw string) ([]parsemail.Attachment, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var atts []parsemail.Attachment
	if err := json.Unmarshal([]byte(raw), &atts); err != nil {
		return nil, fmt.Errorf("decode stored attachments: %w", err)
	}
	return atts, nil
}
