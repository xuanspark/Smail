package controllers

import (
	"net/http"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/restart"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// RestartSmail 请求固定的 Smail 服务重启，不接受外部命令或服务参数。
func RestartSmail(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityServiceRestart, "", false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "重启需要近期认证和有效离线恢复码", "").FPrint(w)
		return
	}
	trigger := restart.Current()
	if trigger == nil {
		response.NewErrorResponse(response.ServerError, "重启服务不可用", "").FPrint(w)
		return
	}
	if err := trigger.Request(req.Context()); err != nil {
		response.NewErrorResponse(response.ServerError, "重启请求未能提交", "").FPrint(w)
		return
	}
	response.NewSuccessResponse(map[string]string{"status": "accepted"}).FPrint(w)
}
