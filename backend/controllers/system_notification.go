package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/accountgroup"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/services/notification"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/validation"
)

type notificationRequest struct {
	UserID      int   `json:"user_id"`
	Version     int   `json:"version"`
	Enabled     *bool `json:"enabled"`
	MinuteLimit int   `json:"minute_limit"`
	DayLimit    int   `json:"day_limit"`
}

func loadNotificationUser(ctx *context.Context, w http.ResponseWriter, userID int, write bool) (*models.User, bool) {
	if !requireAccountScope(ctx, w, userID) {
		return nil, false
	}
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return nil, false
	}
	if db.Instance == nil || userID <= 0 {
		writeSecurityError(w, response.ParamsError, "目标账号无效")
		return nil, false
	}
	var target models.User
	found, err := db.Instance.ID(userID).Get(&target)
	if err != nil || !found {
		writeSecurityError(w, response.ParamsError, "无法读取目标账号")
		return nil, false
	}
	own := ctx.UserID == target.ID
	if write {
		if err := authorizeCapability(ctx, access.CapabilityNotificationManage, target.EffectiveRole(), own); err != nil {
			if access.IsDenied(err, access.CodeRecentAuthRequired) {
				writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "请先确认当前管理员密码", string(access.CodeRecentAuthRequired))
			} else if access.IsDenied(err, access.CodeRecoveryCodeRequired) {
				writeSecurityError(w, response.NoAccessPrivileges, "请先为当前管理员配置安全码")
			} else {
				writeSecurityError(w, response.NoAccessPrivileges, "无权修改该账号的系统通知授权")
			}
			return nil, false
		}
	} else if !own && !access.CanManageNotification(ctx.EffectiveRole(), target.EffectiveRole(), false) {
		writeSecurityError(w, response.NoAccessPrivileges, "只能查看本人或管理范围内的账号状态")
		return nil, false
	}
	return &target, true
}

// GetNotificationStatus 不返回授权码或摘要，普通用户只能查询自己的状态。
func GetNotificationStatus(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	userID := ctx.UserID
	if value := req.URL.Query().Get("user_id"); value != "" {
		var err error
		userID, err = strconv.Atoi(value)
		if err != nil || userID <= 0 {
			writeSecurityError(w, response.ParamsError, "目标账号无效")
			return
		}
	}
	user, ok := loadNotificationUser(ctx, w, userID, false)
	if !ok {
		return
	}
	item, err := notification.Load(db.Instance, user.ID)
	if err != nil {
		writeSecurityError(w, response.ServerError, "读取系统通知授权失败")
		return
	}
	minuteUsed, dayUsed := 0, 0
	now := time.Now().UTC()
	if item.MinuteKey == now.Format("2006-01-02T15:04") {
		minuteUsed = item.MinuteCount
	}
	if item.DayKey == now.Format("2006-01-02") {
		dayUsed = item.DayCount
	}
	domain := config.CurrentMailDomains().Primary
	ports := config.CurrentMailPorts()
	response.NewSuccessResponse(map[string]any{
		"user_id": user.ID, "account": user.Account, "enabled": item.Enabled == 1,
		"configured": notification.Configured(item, *user), "version": item.CodeVersion,
		"can_reveal": notification.Configured(item, *user) && item.CodeCiphertext != "" &&
			(ctx.UserID == user.ID || access.CanManageNotification(ctx.EffectiveRole(), user.EffectiveRole(), false)),
		"account_available": user.Disabled == 0 && user.MustChangePassword == 0,
		"can_manage":        access.CanManageNotification(ctx.EffectiveRole(), user.EffectiveRole(), ctx.UserID == user.ID),
		"minute_limit":      item.MinuteLimit, "day_limit": item.DayLimit,
		"minute_used": minuteUsed, "day_used": dayUsed,
		"host": "smtp." + domain, "tls_port": ports.SMTPTLS, "starttls_port": ports.SMTPSTARTTLS,
		"username": notification.UsernamePrefix + user.Account + "@" + domain,
		"sender":   validation.NotificationAddress(domain), "sender_name": "系统通知",
		"max_recipients": notification.MaxRecipients, "max_message_bytes": notification.MaxMessageBytes,
	}).FPrint(w)
}

// ListNotificationAccounts 仅列出当前管理员可以管理的账号，分页且不暴露安全字段。
func ListNotificationAccounts(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || (ctx.EffectiveRole() != access.RoleAdmin && ctx.EffectiveRole() != access.RoleSuperAdmin) {
		writeSecurityError(w, response.NoAccessPrivileges, "无权查看通知授权账号列表")
		return
	}
	if db.Instance == nil {
		writeSecurityError(w, response.ServerError, "数据库不可用")
		return
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 || page > 100000 {
		page = 1
	}
	query := db.Instance.Table(&models.User{})
	defer query.Close()
	accountgroup.ApplyScope(query, ctx.UserID, ctx.EffectiveRole())
	if ctx.EffectiveRole() == access.RoleAdmin {
		query.And("("+models.OrdinaryUserPredicate+") OR id = ?", ctx.UserID)
	}
	var users []models.User
	total, err := query.Asc("id").Limit(20, (page-1)*20).FindAndCount(&users)
	if err != nil {
		writeSecurityError(w, response.ServerError, "读取账号列表失败")
		return
	}
	items := make([]map[string]any, 0, len(users))
	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}
	grants := make(map[int]models.NotificationGrant)
	if len(userIDs) > 0 {
		var rows []models.NotificationGrant
		if err := db.Instance.In("user_id", userIDs).Find(&rows); err != nil {
			writeSecurityError(w, response.ServerError, "读取账号通知状态失败")
			return
		}
		for _, item := range rows {
			grants[item.UserID] = item
		}
	}
	for _, user := range users {
		item := grants[user.ID]
		items = append(items, map[string]any{"user_id": user.ID, "account": user.Account, "name": user.Name, "disabled": user.Disabled != 0,
			"enabled": item.Enabled == 1, "configured": notification.Configured(item, user)})
	}
	response.NewSuccessResponse(map[string]any{"list": items, "total": total, "page": page}).FPrint(w)
}

func SetNotificationAuthorization(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input notificationRequest
	if decodeJSONRequest(w, req, &input) != nil || input.Enabled == nil || input.Version < 0 {
		writeSecurityError(w, response.ParamsError, "通知授权参数无效")
		return
	}
	user, ok := loadNotificationUser(ctx, w, input.UserID, true)
	if !ok {
		return
	}
	err := notification.SetAuthorization(db.Instance, *user, input.Version, *input.Enabled, input.MinuteLimit, input.DayLimit, accountWriteGuard(ctx, *user))
	recordNotificationManagement(ctx, user.ID, "设置系统通知授权", err, map[string]any{"enabled": *input.Enabled, "minute_limit": input.MinuteLimit, "day_limit": input.DayLimit})
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"user_id": user.ID, "enabled": *input.Enabled}).FPrint(w)
}

func RotateNotificationCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input notificationRequest
	if decodeJSONRequest(w, req, &input) != nil || input.Version < 1 {
		writeSecurityError(w, response.ParamsError, "通知凭据参数无效")
		return
	}
	user, ok := loadNotificationUser(ctx, w, input.UserID, true)
	if !ok {
		return
	}
	code, err := notification.Rotate(db.Instance, *user, input.Version, config.ROOT_PATH, accountWriteGuard(ctx, *user))
	recordNotificationManagement(ctx, user.ID, "重置系统通知凭据", err, nil)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.NewSuccessResponse(map[string]any{"code": code, "user_id": user.ID}).FPrint(w)
}

// RevealNotificationCredential 每次查看都验证操作人密码，日志不得包含密码或凭据。
func RevealNotificationCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		UserID   int    `json:"user_id"`
		Version  int    `json:"version"`
		Password string `json:"password"`
	}
	if decodeJSONRequest(w, req, &input) != nil || input.Version < 1 || input.Password == "" {
		writeSecurityError(w, response.ParamsError, "请填写当前登录密码并选择有效账号")
		return
	}
	user, ok := loadNotificationUser(ctx, w, input.UserID, false)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityNotificationReveal, user.EffectiveRole(), ctx.UserID == user.ID); err != nil {
		writeSecurityError(w, response.NoAccessPrivileges, "无权查看该账号的通知发信凭据")
		return
	}
	userKey := reauthUserAttempts.accountKey(strconv.Itoa(ctx.UserID))
	ipKey := reauthIPAttempts.ipKey(req)
	if reauthUserAttempts.blocked(userKey) || reauthIPAttempts.blocked(ipKey) {
		writeReauthError(w, http.StatusTooManyRequests, "REAUTH_RATE_LIMITED", "尝试次数过多，请稍后再试")
		return
	}
	actor, err := auth.AuthenticateUserByID(ctx.UserID, input.Password)
	input.Password = ""
	if err != nil {
		recordNotificationManagement(ctx, user.ID, "查看系统通知凭据", err, nil)
		if !writePasswordBusy(w, err) {
			reauthUserAttempts.recordFailure(userKey)
			reauthIPAttempts.recordFailure(ipKey)
			writeSecurityError(w, response.ParamsError, "密码验证失败")
		}
		return
	}
	if actor.SessionVersion != ctx.SessionVersion || actor.SecurityVersion != ctx.SecurityVersion || actor.EffectiveRole() != ctx.EffectiveRole() {
		writeSecurityError(w, response.NeedLogin, "登录状态已变化，请重新登录")
		return
	}
	guard := accountgroup.OwnGuard(ctx.UserID, ctx.EffectiveRole(), ctx.SessionVersion, ctx.SecurityVersion)
	if ctx.UserID != user.ID {
		verified := *ctx
		verified.RecentAuth = true
		if _, ok := loadNotificationUser(&verified, w, user.ID, true); !ok {
			return
		}
		guard = accountWriteGuard(ctx, *user)
	}
	code, err := notification.Reveal(db.Instance, *user, input.Version, config.ROOT_PATH, guard)
	if err == nil {
		reauthUserAttempts.clear(userKey)
	}
	recordNotificationManagement(ctx, user.ID, "查看系统通知凭据", err, nil)
	if err != nil {
		writeNotificationError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"code": code, "user_id": user.ID}).FPrint(w)
}

func recordNotificationManagement(ctx *context.Context, userID int, action string, err error, changes map[string]any) {
	result := "success"
	if err != nil {
		result = "failure"
	}
	payload, _ := json.Marshal(map[string]any{"target_user_id": userID, "changes": changes})
	logstore.Record(ctx, logstore.Entry{Kind: "operation", ActorID: strconv.Itoa(ctx.UserID), ActorRole: string(ctx.EffectiveRole()), Action: action,
		Section: "系统通知发信", Result: result, CreatedAt: time.Now(), Payload: payload})
}

func writeNotificationError(w http.ResponseWriter, err error) {
	if errors.Is(err, accountgroup.ErrDenied) || errors.Is(err, accountgroup.ErrConflict) {
		writeAccountGroupError(w, err)
		return
	}
	switch {
	case errors.Is(err, notification.ErrNoCiphertext):
		writeSecurityError(w, response.ParamsError, "旧凭据未保留加密密文，请重新生成后再查看")
	case errors.Is(err, notification.ErrConflict):
		writeSecurityError(w, response.ParamsError, "授权状态已变化，请刷新后重试")
	case errors.Is(err, notification.ErrDenied):
		writeSecurityError(w, response.NoAccessPrivileges, "账号不可用或尚未开启系统通知发信")
	case errors.Is(err, notification.ErrLimits):
		writeSecurityError(w, response.ParamsError, "每分钟限额为 1–100，每日限额不能小于分钟限额且不能超过 10000")
	default:
		writeSecurityError(w, response.ServerError, "保存系统通知授权失败，请刷新确认状态")
	}
}
