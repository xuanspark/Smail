package email

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/address"
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

type senderFilterRequest struct {
	ID         int    `json:"id"`
	Email      string `json:"email"`
	FilterType string `json:"filter_type"`
}

type toggleSenderFilterRequest struct {
	Email      string `json:"email"`
	TargetType string `json:"target_type"`
}

// ContactsList 查询当前用户的历史写信收件人列表（多租户严格隔离）
func ContactsList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
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
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
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
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
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

	now := time.Now()
	if reqData.Trusted {
		var existing models.UserTrustedSender
		has, err := db.Instance.Table(&models.UserTrustedSender{}).
			Where("user_id = ? AND LOWER(email) = ?", ctx.UserID, email).
			Get(&existing)
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("check trusted sender failed")
			response.NewErrorResponse(response.ServerError, "操作失败", "").FPrint(w)
			return
		}
		if has {
			if existing.FilterType != models.FilterTypeWhitelist {
				existing.FilterType = models.FilterTypeWhitelist
				existing.UpdatedAt = &now
				_, _ = db.Instance.ID(existing.ID).Cols("filter_type", "updated_at").Update(&existing)
			}
		} else {
			item := models.UserTrustedSender{
				UserID:     ctx.UserID,
				Email:      email,
				FilterType: models.FilterTypeWhitelist,
				CreatedAt:  now,
				UpdatedAt:  &now,
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
			response.NewErrorResponse(response.ServerError, "添加信任发件人失败", "").FPrint(w)
			return
		}
	}

	response.NewSuccessResponse("ok").FPrint(w)
}

// SenderFiltersList 查询当前用户的发件人黑白名单列表（支持分页、搜索、类型筛选与统计）
func SenderFiltersList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}

	query := req.URL.Query()
	filterType := strings.ToLower(strings.TrimSpace(query.Get("filter_type")))
	keyword := strings.ToLower(strings.TrimSpace(query.Get("keyword")))
	page, _ := strconv.Atoi(query.Get("page"))
	if page <= 0 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(query.Get("page_size"))
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	session := db.Instance.Table(&models.UserTrustedSender{}).Where("user_id = ?", ctx.UserID)
	if filterType == models.FilterTypeWhitelist || filterType == models.FilterTypeBlacklist {
		session = session.And("filter_type = ?", filterType)
	}
	if keyword != "" {
		session = session.And("LOWER(email) LIKE ?", "%"+keyword+"%")
	}

	var list []models.UserTrustedSender
	total, err := session.OrderBy("created_at DESC").Limit(pageSize, offset).FindAndCount(&list)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("query sender filters failed")
		response.NewErrorResponse(response.ServerError, "查询过滤名单失败", "").FPrint(w)
		return
	}

	whitelistCount, _ := db.Instance.Table(&models.UserTrustedSender{}).
		Where("user_id = ? AND filter_type = ?", ctx.UserID, models.FilterTypeWhitelist).
		Count(&models.UserTrustedSender{})
	blacklistCount, _ := db.Instance.Table(&models.UserTrustedSender{}).
		Where("user_id = ? AND filter_type = ?", ctx.UserID, models.FilterTypeBlacklist).
		Count(&models.UserTrustedSender{})

	response.NewSuccessResponse(map[string]any{
		"list":            list,
		"total":           total,
		"whitelist_count": whitelistCount,
		"blacklist_count": blacklistCount,
		"page":            page,
		"page_size":       pageSize,
	}).FPrint(w)
}

// AddSenderFilter 添加或更新当前用户的白名单/黑名单
func AddSenderFilter(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
	var reqData senderFilterRequest
	if err := json.NewDecoder(req.Body).Decode(&reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(reqData.Email))
	if email == "" || !address.IsValidEmailAddress(email) {
		response.NewErrorResponse(response.ParamsError, "邮箱格式不正确", "").FPrint(w)
		return
	}

	filterType := strings.ToLower(strings.TrimSpace(reqData.FilterType))
	if filterType == "" {
		filterType = models.FilterTypeWhitelist
	}
	if filterType != models.FilterTypeWhitelist && filterType != models.FilterTypeBlacklist {
		response.NewErrorResponse(response.ParamsError, "名单类别必须为白名单或黑名单", "").FPrint(w)
		return
	}

	now := time.Now()
	var existing models.UserTrustedSender
	has, err := db.Instance.Table(&models.UserTrustedSender{}).
		Where("user_id = ? AND LOWER(email) = ?", ctx.UserID, email).
		Get(&existing)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("check sender filter exists failed")
		response.NewErrorResponse(response.ServerError, "操作失败", "").FPrint(w)
		return
	}

	if has {
		existing.FilterType = filterType
		existing.UpdatedAt = &now
		if _, err := db.Instance.ID(existing.ID).Cols("filter_type", "updated_at").Update(&existing); err != nil {
			log.WithContext(ctx).WithError(err).Error("update sender filter failed")
			response.NewErrorResponse(response.ServerError, "更新过滤名单失败", "").FPrint(w)
			return
		}
	} else {
		item := models.UserTrustedSender{
			UserID:     ctx.UserID,
			Email:      email,
			FilterType: filterType,
			CreatedAt:  now,
			UpdatedAt:  &now,
		}
		if _, err := db.Instance.Insert(&item); err != nil {
			log.WithContext(ctx).WithError(err).Error("insert sender filter failed")
			response.NewErrorResponse(response.ServerError, "添加过滤名单失败", "").FPrint(w)
			return
		}
	}

	response.NewSuccessResponse("ok").FPrint(w)
}

// DeleteSenderFilter 删除发件人黑白名单条目（支持ID或邮箱地址删除）
func DeleteSenderFilter(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
	var reqData senderFilterRequest
	if req.Body != nil {
		_ = json.NewDecoder(req.Body).Decode(&reqData)
	}
	email := strings.ToLower(strings.TrimSpace(reqData.Email))
	if reqData.ID <= 0 && email == "" {
		if idVal, err := strconv.Atoi(req.URL.Query().Get("id")); err == nil && idVal > 0 {
			reqData.ID = idVal
		}
		if qEmail := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("email"))); qEmail != "" {
			email = qEmail
		}
	}

	session := db.Instance.Table(&models.UserTrustedSender{}).Where("user_id = ?", ctx.UserID)
	if reqData.ID > 0 {
		session = session.And("id = ?", reqData.ID)
	} else if email != "" {
		session = session.And("LOWER(email) = ?", email)
	} else {
		response.NewErrorResponse(response.ParamsError, "请指定要删除的记录ID或发件人邮箱", "").FPrint(w)
		return
	}

	if _, err := session.Delete(&models.UserTrustedSender{}); err != nil {
		log.WithContext(ctx).WithError(err).Error("delete sender filter failed")
		response.NewErrorResponse(response.ServerError, "操作失败", "").FPrint(w)
		return
	}

	response.NewSuccessResponse("ok").FPrint(w)
}

// ToggleSenderFilter 快速切换发件人的白名单/黑名单状态
func ToggleSenderFilter(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
	var reqData toggleSenderFilterRequest
	if err := json.NewDecoder(req.Body).Decode(&reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	email := strings.ToLower(strings.TrimSpace(reqData.Email))
	if email == "" || !address.IsValidEmailAddress(email) {
		response.NewErrorResponse(response.ParamsError, "邮箱格式不正确", "").FPrint(w)
		return
	}

	now := time.Now()
	var existing models.UserTrustedSender
	has, err := db.Instance.Table(&models.UserTrustedSender{}).
		Where("user_id = ? AND LOWER(email) = ?", ctx.UserID, email).
		Get(&existing)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("query sender filter for toggle failed")
		response.NewErrorResponse(response.ServerError, "操作失败", "").FPrint(w)
		return
	}

	targetType := strings.ToLower(strings.TrimSpace(reqData.TargetType))
	if !has {
		if targetType != models.FilterTypeWhitelist && targetType != models.FilterTypeBlacklist {
			targetType = models.FilterTypeBlacklist
		}
		item := models.UserTrustedSender{
			UserID:     ctx.UserID,
			Email:      email,
			FilterType: targetType,
			CreatedAt:  now,
			UpdatedAt:  &now,
		}
		if _, err := db.Instance.Insert(&item); err != nil {
			log.WithContext(ctx).WithError(err).Error("insert toggled sender filter failed")
			response.NewErrorResponse(response.ServerError, "操作失败", "").FPrint(w)
			return
		}
	} else {
		if targetType == "" {
			if existing.FilterType == models.FilterTypeBlacklist {
				targetType = models.FilterTypeWhitelist
			} else {
				targetType = models.FilterTypeBlacklist
			}
		}
		existing.FilterType = targetType
		existing.UpdatedAt = &now
		if _, err := db.Instance.ID(existing.ID).Cols("filter_type", "updated_at").Update(&existing); err != nil {
			log.WithContext(ctx).WithError(err).Error("update toggled sender filter failed")
			response.NewErrorResponse(response.ServerError, "操作失败", "").FPrint(w)
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
