package controllers

import (
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// TOTPSettings 所有登录角色均可管理自己的验证器，不接受客户端指定用户。
func TOTPSettings(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	service := currentSecurityRuntime().totp
	if ctx == nil || ctx.UserID <= 0 || service == nil {
		writeSecurityError(w, response.ParamsError, "动态码服务暂不可用")
		return
	}
	if req.Method == http.MethodGet {
		enabled, err := service.IsEnabled(ctx.UserID)
		if err != nil {
			writeSecurityError(w, response.ParamsError, "无法读取动态码状态")
			return
		}
		response.NewSuccessResponse(map[string]any{"enabled": enabled}).FPrint(w)
		return
	}
	if !ctx.RecentAuth {
		writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "请确认当前密码后继续操作", string(access.CodeRecentAuthRequired))
		return
	}
	var input struct {
		Action string `json:"action"`
		ID     string `json:"id"`
		Code   string `json:"code"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "动态码请求无效")
		return
	}
	var err error
	limitKey := loginSecondFactorAttempts.accountKey(ctx.UserAccount)
	if loginSecondFactorAttempts.blocked(limitKey) {
		writeSecurityError(w, response.ParamsError, "验证失败次数过多，请十五分钟后重试")
		return
	}
	switch input.Action {
	case "begin":
		enrollment, beginErr := service.BeginEnrollment(ctx.UserID, ctx.UserAccount)
		if beginErr != nil {
			writeSecurityError(w, response.ParamsError, "无法开始绑定，请刷新状态后重试")
			return
		}
		response.NewSuccessResponse(enrollment).FPrint(w)
		return
	case "confirm":
		err = service.ConfirmEnrollment(ctx.UserID, input.ID, strings.TrimSpace(input.Code))
	case "disable":
		err = service.Disable(ctx.UserID, strings.TrimSpace(input.Code))
	case "cancel":
		service.InvalidateUser(ctx.UserID)
	default:
		writeSecurityError(w, response.ParamsError, "动态码操作无效")
		return
	}
	if err != nil {
		loginSecondFactorAttempts.recordFailure(limitKey)
		writeSecurityError(w, response.ParamsError, "动态码无效、已使用或绑定已过期，请使用新的动态码重试")
		return
	}
	if input.Action == "confirm" || input.Action == "disable" {
		loginSecondFactorAttempts.clear(limitKey)
		InvalidateSecurityRuntimeUser(ctx.UserID, ctx.UserAccount)
	}
	response.NewSuccessResponse(map[string]any{"success": true}).FPrint(w)
}
