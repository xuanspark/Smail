package controllers

import (
	"encoding/json"
	"net/http"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/services/access"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func telegramRequestService(ctx *appcontext.Context, w http.ResponseWriter, recent bool) *pluginservice.ConfigService {
	if !authorizedPluginUser(ctx) {
		writeConfigAccessError(w, &access.Denial{Code: access.CodeAccessDenied})
		return nil
	}
	if recent && !ctx.RecentAuth {
		writeConfigAccessError(w, &access.Denial{Code: access.CodeRecentAuthRequired})
		return nil
	}
	service := hooks.PluginConfigService()
	if service == nil { response.NewErrorResponse(response.ServerError, "通知配置服务不可用", "").FPrint(w) }
	return service
}

func telegramPublicSettings(service *pluginservice.ConfigService, status pluginservice.UserSettingsStatus) map[string]any {
	settings := status.Settings
	return map[string]any{
		"version": status.Version, "bound": settings.ChatID != "", "displayName": settings.DisplayName,
		"needsRebind": service.TelegramBindingNeedsRebind(settings),
		"enabled": settings.ChatID != "" && settings.Verified && !settings.Disabled,
		"showContent": settings.ShowContent, "spoilerContent": settings.SpoilerContent,
		"sendAttachments": settings.SendAttachments, "disableLinkPreview": settings.DisableLinkPreview,
	}
}

func writeTelegramUserSettings(w http.ResponseWriter, service *pluginservice.ConfigService, userID int, status pluginservice.UserSettingsStatus) {
	data := telegramPublicSettings(service, status)
	data["synced"] = hooks.SyncTelegramUserSettings(userID, status.Settings) == nil
	response.NewSuccessResponse(data).FPrint(w)
}

// TelegramUserSettings 的接收身份只能来自授权流程，页面仅维护本人偏好。
func TelegramUserSettings(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	service := telegramRequestService(ctx, w, req.Method != http.MethodGet)
	if service == nil { return }
	if req.Method == http.MethodGet {
		status, err := service.GetOwnTelegramSettings(ctx.UserID)
		if err != nil { writeTelegramSettingsError(w, err); return }
		data := telegramPublicSettings(service, status)
		configured, message := service.TelegramBindingAvailability()
		_, running := hooks.GetHook("telegram_push")
		if configured && !running { message = "Telegram 推送插件未运行，请联系超级管理员" }
		data["configured"], data["running"], data["available"], data["message"] = configured, running, configured && running, message
		response.NewSuccessResponse(data).FPrint(w)
		return
	}
	if req.Method != http.MethodPut && req.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Version uint64 `json:"version"`
		Enabled bool `json:"enabled"`
		ShowContent bool `json:"showContent"`
		SpoilerContent bool `json:"spoilerContent"`
		SendAttachments bool `json:"sendAttachments"`
		DisableLinkPreview bool `json:"disableLinkPreview"`
	}
	if !decodePluginJSONBody(w, req, &input) { return }
	preferences := pluginservice.TelegramUserSettings{Disabled: !input.Enabled, ShowContent: input.ShowContent,
		SpoilerContent: input.SpoilerContent, SendAttachments: input.SendAttachments, DisableLinkPreview: input.DisableLinkPreview}
	status, err := service.UpdateOwnTelegramPreferences(ctx.UserID, preferences, req.Method == http.MethodDelete, input.Version)
	if err != nil { writeTelegramSettingsError(w, err); return }
	writeTelegramUserSettings(w, service, ctx.UserID, status)
}

func TelegramBinding(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	var input struct { Action string `json:"action"`; Ticket string `json:"ticket"` }
	if !decodePluginJSONBody(w, req, &input) { return }
	service := telegramRequestService(ctx, w, input.Action == "begin" || input.Action == "confirm")
	if service == nil { return }
	actor, err := notificationBindingActor(ctx)
	if err != nil { writeTelegramSettingsError(w, pluginservice.ErrTelegramBinding); return }
	var result any
	switch input.Action {
	case "begin":
		if _, running := hooks.GetHook("telegram_push"); !running {
			writeTelegramSettingsError(w, pluginservice.TelegramNotice("Telegram 推送插件未运行，请联系超级管理员"))
			return
		}
		result, err = service.BeginTelegramBinding(req.Context(), actor)
	case "status":
		result, err = service.GetTelegramBinding(actor, input.Ticket)
	case "cancel":
		err = service.CancelTelegramBinding(actor, input.Ticket)
	case "confirm":
		var status pluginservice.UserSettingsStatus
		status, err = service.ConfirmTelegramBinding(actor, input.Ticket)
		if err == nil { writeTelegramUserSettings(w, service, ctx.UserID, status); return }
	default:
		err = pluginservice.TelegramNotice("不支持的绑定操作")
	}
	if err != nil { writeTelegramSettingsError(w, err); return }
	response.NewSuccessResponse(result).FPrint(w)
}

func TelegramCallbackSettings(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || ctx.EffectiveRole() != access.RoleSuperAdmin {
		writeConfigAccessError(w, &access.Denial{Code: access.CodeAccessDenied})
		return
	}
	service := telegramRequestService(ctx, w, req.Method == http.MethodPost)
	if service == nil { return }
	var status pluginservice.TelegramCallbackStatus
	var err error
	switch req.Method {
	case http.MethodGet:
		status, err = service.TelegramCallbackState(req.Context())
	case http.MethodPost:
		status, err = service.ConfigureTelegramCallback(req.Context(), ctx.UserID)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	if err != nil { writeTelegramSettingsError(w, err); return }
	response.NewSuccessResponse(status).FPrint(w)
}

// TelegramBindingCallback 不使用网页登录会话，只接受带独立回调密钥的 Telegram 请求。
func TelegramBindingCallback(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	service := hooks.PluginConfigService()
	if !config.IsSetupReady() || service == nil { http.Error(w, "服务未就绪", http.StatusServiceUnavailable); return }
	var update pluginservice.TelegramBindingUpdate
	decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 64<<10))
	if decoder.Decode(&update) != nil { http.Error(w, "回调格式无效", http.StatusBadRequest); return }
	if err := service.ClaimTelegramBinding(req.Header.Get("X-Telegram-Bot-Api-Secret-Token"), update); err != nil {
		http.Error(w, "回调认证失败", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestBoundTelegramMessage(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	service := telegramRequestService(ctx, w, true)
	if service == nil { return }
	status, err := service.GetOwnTelegramSettings(ctx.UserID)
	if err != nil || !service.TelegramCanNotify(status.Settings) {
		writeTelegramSettingsError(w, pluginservice.TelegramNotice("请先绑定本人 Telegram 并启用通知"))
		return
	}
	hook, _ := hooks.GetHook("telegram_push")
	provider, ok := hook.(framework.TestMessageProvider)
	if !ok { writeTelegramSettingsError(w, pluginservice.TelegramNotice("Telegram 推送插件未运行，请联系超级管理员")); return }
	if err := service.ReserveTelegramTest(ctx.UserID); err != nil { writeTelegramSettingsError(w, err); return }
	if provider.TestMessage(pluginservice.TelegramDeliverySettings(status.Settings)) != nil {
		writeTelegramSettingsError(w, pluginservice.TelegramNotice("测试消息发送失败，请确认没有屏蔽机器人"))
		return
	}
	response.NewSuccessResponse(nil).FPrint(w)
}
