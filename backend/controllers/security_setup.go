package controllers

import (
	"net/http"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// AcknowledgeSecuritySetup 仅确认当前账号已经查看引导，不代表完成安全设置。
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
	if user.SecuritySetupAcknowledged == 0 {
		user.SecuritySetupAcknowledged = 1
		if _, err := db.Instance.ID(ctx.UserID).Cols("security_setup_acknowledged").Update(&user); err != nil {
			writeSecurityError(w, response.ServerError, "保存安全引导状态失败，请重试")
			return
		}
	}
	response.NewSuccessResponse(map[string]any{"showSecuritySetupPrompt": false}).FPrint(w)
}
