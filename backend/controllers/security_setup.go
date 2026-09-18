package controllers

import (
	"net/http"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// AcknowledgeSecuritySetup 仅关闭当前登录会话的提醒，下次登录重新检查设置。
func AcknowledgeSecuritySetup(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var user models.User
	found, err := db.Instance.ID(ctx.UserID).Get(&user)
	if err != nil {
		writeSecurityError(w, response.ServerError, "读取安全引导状态失败，请重试")
		return
	}
	if !found || user.Disabled != 0 {
		writeSecurityError(w, response.NeedLogin, "请重新登录")
		return
	}
	if session.Instance == nil || req == nil {
		writeSecurityError(w, response.ServerError, "保存提醒状态失败，请重试")
		return
	}
	session.Instance.Put(req.Context(), session.SecuritySetupDismissedKey, true)
	response.NewSuccessResponse(map[string]any{"showSecuritySetupPrompt": false}).FPrint(w)
}
