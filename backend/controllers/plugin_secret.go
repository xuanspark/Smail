package controllers

import (
	"errors"
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/services/access"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// PluginSecret 只维护数据库中的插件密钥，不写入文件配置或候选载荷。
func PluginSecret(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if req.Method != http.MethodGet && req.Method != http.MethodPut {
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	capability := access.CapabilityPluginReadGlobal
	if req.Method == http.MethodPut {
		capability = access.CapabilityPluginWriteGlobal
	}
	// 共享公众号和机器人密钥不属于分组管理员权限，首次配置和后续维护使用同一边界。
	if ctx == nil || ctx.UserID <= 0 || ctx.EffectiveRole() != access.RoleSuperAdmin {
		writeConfigAccessError(w, &access.Denial{Code: access.CodeAccessDenied})
		return
	}
	if err := authorizeCapability(ctx, capability, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	pluginID := strings.TrimPrefix(req.URL.Path, "/api/v1/plugin/secrets/")
	field := map[string]string{"telegram_push": "botToken", "wechat_push": "appSecret"}[pluginID]
	if field == "" {
		http.NotFound(w, req)
		return
	}
	service := hooks.PluginConfigService()
	if service == nil {
		response.NewErrorResponse(response.ServerError, "插件配置服务不可用", "").FPrint(w)
		return
	}
	var status pluginservice.SecretStatus
	var err error
	if req.Method == http.MethodGet {
		status, err = service.GetSecretStatus(pluginID, field)
	} else {
		var input struct {
			Change  config.SecretChange `json:"change"`
			Version uint64              `json:"version"`
		}
		if !decodePluginJSONBody(w, req, &input) {
			return
		}
		status, err = service.ChangeSecret(pluginID, field, input.Change, input.Version)
	}
	if err != nil {
		if errors.Is(err, pluginservice.ErrPluginSettingConflict) {
			writeConfigError(w, config.ErrGenerationConflict)
		} else {
			response.NewErrorResponse(response.ParamsError, "插件密钥操作失败，请检查输入后重试", "").FPrint(w)
		}
		return
	}
	response.NewSuccessResponse(status).FPrint(w)
}
