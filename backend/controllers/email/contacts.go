package email

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
)

type deleteContactRequest struct {
	Email string `json:"email"`
}

type trustSenderRequest struct {
	Email   string `json:"email"`
	Trusted bool   `json:"trusted"`
}

// ContactsList 查询当前用户的历史写信收件人列表（多租户严格隔离）
func ContactsList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var contacts []models.UserContact
	err := db.Instance.Table(&models.UserContact{}).
		Where("user_id = ?", ctx.UserID).
		OrderBy("last_used_at DESC").
		Limit(100).
		Find(&contacts)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("query user contacts failed")
		response.NewErrorResponse(response.ServerError, "获取联系人失败", "").FPrint(w)
		return
	}

	response.NewSuccessResponse(contacts).FPrint(w)
}

// DeleteContact 删除当前用户的某项历史联系人记录
func DeleteContact(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData deleteContactRequest
	if err := json.NewDecoder(req.Body).Decode(&reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(reqData.Email))
	if email == "" {
		response.NewErrorResponse(response.ParamsError, "联系人邮箱不能为空", "").FPrint(w)
		return
	}

	_, err := db.Instance.Table(&models.UserContact{}).
		Where("user_id = ? AND LOWER(email) = ?", ctx.UserID, email).
		Delete(&models.UserContact{})
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("delete user contact failed")
		response.NewErrorResponse(response.ServerError, "删除联系人失败", "").FPrint(w)
		return
	}

	response.NewSuccessResponse("ok").FPrint(w)
}

// TrustSender 将发件人添加至信任白名单，或从中移除
func TrustSender(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData trustSenderRequest
	if err := json.NewDecoder(req.Body).Decode(&reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(reqData.Email))
	if email == "" {
		response.NewErrorResponse(response.ParamsError, "发件人邮箱不能为空", "").FPrint(w)
		return
	}

	if reqData.Trusted {
		var exists bool
		has, err := db.Instance.Table(&models.UserTrustedSender{}).
			Where("user_id = ? AND LOWER(email) = ?", ctx.UserID, email).
			Exist(&models.UserTrustedSender{})
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("check trusted sender failed")
			response.NewErrorResponse(response.ServerError, "操作失败", "").FPrint(w)
			return
		}
		exists = has
		if !exists {
			item := models.UserTrustedSender{
				UserID:    ctx.UserID,
				Email:     email,
				CreatedAt: time.Now(),
			}
			if _, err := db.Instance.Insert(&item); err != nil {
				log.WithContext(ctx).WithError(err).Error("insert trusted sender failed")
				response.NewErrorResponse(response.ServerError, "添加信任发件人失败", "").FPrint(w)
				return
			}
		}
	} else {
		if _, err := db.Instance.Table(&models.UserTrustedSender{}).
			Where("user_id = ? AND LOWER(email) = ?", ctx.UserID, email).
			Delete(&models.UserTrustedSender{}); err != nil {
			log.WithContext(ctx).WithError(err).Error("remove trusted sender failed")
			response.NewErrorResponse(response.ServerError, "取消信任发件人失败", "").FPrint(w)
			return
		}
	}

	response.NewSuccessResponse("ok").FPrint(w)
}

// RecordUserContacts 异步批量记录发件成功后的收件人地址
func RecordUserContacts(userID int, recipients []models.UserContact) {
	if userID <= 0 || len(recipients) == 0 {
		return
	}
	now := time.Now()
	_ = db.Transaction(db.Instance, func(session *xorm.Session) error {
		for _, r := range recipients {
			email := strings.ToLower(strings.TrimSpace(r.Email))
			if email == "" {
				continue
			}
			var existing models.UserContact
			has, err := session.Table(&models.UserContact{}).
				Where("user_id = ? AND LOWER(email) = ?", userID, email).
				Get(&existing)
			if err != nil {
				return err
			}
			if has {
				existing.LastUsedAt = now
				if r.Name != "" {
					existing.Name = r.Name
				}
				if _, err := session.ID(existing.ID).Cols("name", "last_used_at").Update(&existing); err != nil {
					return err
				}
			} else {
				item := models.UserContact{
					UserID:     userID,
					Email:      email,
					Name:       r.Name,
					LastUsedAt: now,
				}
				if _, err := session.Insert(&item); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
