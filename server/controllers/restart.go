package controllers

import (
	"net/http"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/restart"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// PreviewRestartSmail 返回固定的 Smail 重启预览，不执行重启动作。
func PreviewRestartSmail(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityServiceRestart, "", false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "重启预览需要近期认证和有效离线恢复码", "").FPrint(w)
		return
	}
	manager := config.CurrentManager()
	if manager == nil {
		response.NewErrorResponse(response.ServerError, "重启预览暂时不可用", "").FPrint(w)
		return
	}
	status := manager.Status()
	pending := make([]string, 0, len(status.PendingRestart))
	for _, service := range status.PendingRestart {
		pending = append(pending, string(service))
	}
	preview := restart.BuildPreview(status.ActiveGeneration, pending, restart.Current() != nil)
	response.NewSuccessResponse(preview).FPrint(w)
}

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
