package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/services/access"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func weChatHook() framework.EmailHook {
	for _, name := range []string{"wechat_push", "WeChatPushHook", "WechatPush"} {
		if hook, found := hooks.GetHook(name); found {
			return hook
		}
	}
	return nil
}

func notificationBindingActor(ctx *appcontext.Context) (pluginservice.NotificationBindingActor, error) {
	if !authorizedPluginUser(ctx) || session.Instance == nil || ctx.Context == nil {
		return pluginservice.NotificationBindingActor{}, pluginservice.ErrWeChatBinding
	}
	token := session.Instance.Token(ctx)
	if token == "" {
		return pluginservice.NotificationBindingActor{}, pluginservice.ErrWeChatBinding
	}
	hash := sha256.Sum256([]byte(token))
	return pluginservice.NotificationBindingActor{UserID: ctx.UserID, SessionVersion: ctx.SessionVersion,
		SecurityVersion: ctx.SecurityVersion, SessionHash: hex.EncodeToString(hash[:])}, nil
}

func weChatRequestService(ctx *appcontext.Context, w http.ResponseWriter, recent bool) *pluginservice.ConfigService {
	if !authorizedPluginUser(ctx) {
		writeConfigAccessError(w, &access.Denial{Code: access.CodeAccessDenied})
		return nil
	}
	if recent && !ctx.RecentAuth {
		writeConfigAccessError(w, &access.Denial{Code: access.CodeRecentAuthRequired})
		return nil
	}
	service := hooks.PluginConfigService()
	if service == nil {
		response.NewErrorResponse(response.ServerError, "通知配置服务不可用", "").FPrint(w)
	}
	return service
}

func weChatPublicSettings(status pluginservice.WeChatUserSettingsStatus) map[string]any {
	return map[string]any{"version": status.Version, "bound": status.Settings.OpenID != "", "enabled": status.Settings.Enabled,
		"showContent": status.Settings.ShowContent, "displayName": status.Settings.DisplayName}
}

func writeWeChatSettings(w http.ResponseWriter, userID int, status pluginservice.WeChatUserSettingsStatus) {
	data := weChatPublicSettings(status)
	data["synced"] = hooks.SyncWeChatUserSettings(userID, status.Settings) == nil
	response.NewSuccessResponse(data).FPrint(w)
}

func WeChatUserSettings(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	service := weChatRequestService(ctx, w, req.Method != http.MethodGet)
	if service == nil {
		return
	}
	if req.Method == http.MethodGet {
		status, err := service.GetOwnWeChatSettings(ctx.UserID)
		if err != nil {
			writeWeChatError(w, err)
			return
		}
		available, message := service.WeChatBindingAvailability()
		running := weChatHook() != nil
		if available && !running {
			message = "微信推送插件未运行，请联系超级管理员"
		}
		data := weChatPublicSettings(status)
		data["available"], data["configured"], data["running"], data["message"] = available && running, available, running, message
		response.NewSuccessResponse(data).FPrint(w)
		return
	}
	if req.Method != http.MethodPut && req.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "请求方法不支持", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Enabled     bool   `json:"enabled"`
		ShowContent bool   `json:"showContent"`
		Version     uint64 `json:"version"`
	}
	if !decodePluginJSONBody(w, req, &input) {
		return
	}
	status, err := service.UpdateOwnWeChatPreferences(ctx.UserID, input.Enabled, input.ShowContent, req.Method == http.MethodDelete, input.Version)
	if err != nil {
		writeWeChatError(w, err)
		return
	}
	writeWeChatSettings(w, ctx.UserID, status)
}

func WeChatBinding(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	var input struct {
		Action string `json:"action"`
		Ticket string `json:"ticket"`
	}
	if !decodePluginJSONBody(w, req, &input) {
		return
	}
	service := weChatRequestService(ctx, w, input.Action == "begin" || input.Action == "confirm")
	if service == nil {
		return
	}
	actor, err := notificationBindingActor(ctx)
	if err != nil {
		writeWeChatError(w, err)
		return
	}
	var result any
	switch input.Action {
	case "begin":
		if weChatHook() == nil {
			writeWeChatError(w, pluginservice.WeChatNotice("微信推送插件未运行，请联系超级管理员"))
			return
		}
		result, err = service.BeginWeChatBinding(actor)
	case "status":
		result, err = service.GetWeChatBinding(actor, input.Ticket)
	case "cancel":
		err = service.CancelWeChatBinding(actor, input.Ticket)
	case "confirm":
		var status pluginservice.WeChatUserSettingsStatus
		status, err = service.ConfirmWeChatBinding(actor, input.Ticket)
		if err == nil {
			writeWeChatSettings(w, ctx.UserID, status)
			return
		}
	default:
		err = pluginservice.WeChatNotice("绑定操作无效")
	}
	if err != nil {
		writeWeChatError(w, err)
		return
	}
	response.NewSuccessResponse(result).FPrint(w)
}

// WeChatBindingCallback 不依赖手机浏览器的登录 Cookie，也不自动完成绑定。
func WeChatBindingCallback(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	service := hooks.PluginConfigService()
	if !config.IsSetupReady() || service == nil || service.ClaimWeChatBinding(req.Context(), req.URL.Query().Get("state"), req.URL.Query().Get("code")) != nil {
		http.Error(w, "授权失败或二维码已失效，请返回 Smail 页面重新扫码。", http.StatusBadRequest)
		return
	}
	_, _ = w.Write([]byte("微信授权已完成。请返回发起扫码的 Smail 页面，核对微信昵称后点击确认绑定。"))
}

func TestWeChatMessage(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	service := weChatRequestService(ctx, w, true)
	if service == nil {
		return
	}
	status, err := service.GetOwnWeChatSettings(ctx.UserID)
	if err != nil || status.Settings.OpenID == "" {
		writeWeChatError(w, pluginservice.WeChatNotice("请先绑定本人的微信"))
		return
	}
	provider, ok := weChatHook().(framework.TestMessageProvider)
	if !ok {
		writeWeChatError(w, pluginservice.WeChatNotice("微信推送插件未运行，请联系超级管理员"))
		return
	}
	if err := service.ReserveWeChatTest(ctx.UserID); err != nil {
		writeWeChatError(w, err)
		return
	}
	payload, err := json.Marshal(status.Settings)
	if err != nil || provider.TestMessage(payload) != nil {
		writeWeChatError(w, pluginservice.WeChatNotice("测试消息发送失败，请确认已关注公众号及模板配置正确"))
		return
	}
	response.NewSuccessResponse(nil).FPrint(w)
}

func writeWeChatError(w http.ResponseWriter, err error) {
	message := "微信通知操作失败，请稍后重试"
	if errors.Is(err, pluginservice.ErrPluginSettingConflict) {
		message = "设置已被其他操作更新，请刷新后重试"
	} else if errors.Is(err, pluginservice.ErrWeChatBinding) {
		message = pluginservice.ErrWeChatBinding.Error()
	} else {
		// 只公开固定业务提示，避免数据库错误携带内部路径、查询或连接信息。
		var notice pluginservice.WeChatNotice
		if errors.As(err, &notice) {
			message = notice.Error()
		}
	}
	response.NewErrorResponse(response.ParamsError, message, "").FPrint(w)
}
