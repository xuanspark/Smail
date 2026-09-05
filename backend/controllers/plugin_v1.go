package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/services/access"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

const maxTelegramUserSettingsBodyBytes int64 = 16 << 10

type telegramSettingsRequest struct {
	Settings pluginservice.TelegramUserSettings `json:"settings"`
	Version  uint64                             `json:"version"`
}

func GetTelegramUserSettings(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !authenticatedPluginUser(ctx) {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
	service := hooks.PluginConfigService()
	if service == nil {
		response.NewErrorResponse(response.ServerError, "插件配置服务不可用", "").FPrint(w)
		return
	}
	settings, err := service.GetOwnTelegramSettings(ctx.UserID)
	if err != nil {
		writeTelegramSettingsError(w, err)
		return
	}
	response.NewSuccessResponse(settings).FPrint(w)
}

func UpdateTelegramUserSettings(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !authenticatedPluginUser(ctx) {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
	var input telegramSettingsRequest
	if !decodePluginJSONBody(w, req, &input) {
		return
	}
	if err := pluginservice.ValidateTelegramUserSettings(input.Settings); err != nil {
		writeTelegramSettingsError(w, err)
		return
	}
	service := hooks.PluginConfigService()
	if service == nil {
		response.NewErrorResponse(response.ServerError, "插件配置服务不可用", "").FPrint(w)
		return
	}
	settings, err := service.UpdateOwnTelegramSettings(ctx.UserID, input.Settings, input.Version)
	if err != nil {
		writeTelegramSettingsError(w, err)
		return
	}
	if err := hooks.SyncTelegramUserSettings(ctx.UserID, settings.Settings); err != nil {
		log.WithError(err).Warnf("Telegram 用户设置同步失败，用户 ID=%d", ctx.UserID)
	}
	response.NewSuccessResponse(settings).FPrint(w)
}

func GetTelegramBotInfo(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !authenticatedPluginUser(ctx) {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
	hook, ok := hooks.GetHook("telegram_push")
	if !ok {
		response.NewErrorResponse(response.ServerError, "Telegram 插件不可用", "").FPrint(w)
		return
	}
	provider, ok := hook.(framework.BotInfoProvider)
	if !ok {
		response.NewErrorResponse(response.ServerError, "Telegram 插件不支持机器人信息", "").FPrint(w)
		return
	}
	info, err := provider.GetBotInfo()
	if err != nil {
		log.WithError(err).Warn("获取 Telegram 机器人信息失败")
		response.NewErrorResponse(response.ServerError, "获取 Telegram 机器人信息失败", "").FPrint(w)
		return
	}
	response.NewSuccessResponse(info).FPrint(w)
}

func TestTelegramMessage(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !authenticatedPluginUser(ctx) {
		response.NewErrorResponse(response.NeedLogin, "请先登录", "").FPrint(w)
		return
	}
	var input telegramSettingsRequest
	if !decodePluginJSONBody(w, req, &input) {
		return
	}
	if strings.TrimSpace(input.Settings.ChatID) == "" {
		response.NewErrorResponse(response.ParamsError, "请输入 Telegram 聊天 ID", "").FPrint(w)
		return
	}
	if err := pluginservice.ValidateTelegramUserSettings(input.Settings); err != nil {
		writeTelegramSettingsError(w, err)
		return
	}
	hook, ok := hooks.GetHook("telegram_push")
	if !ok {
		response.NewErrorResponse(response.ServerError, "Telegram 插件不可用", "").FPrint(w)
		return
	}
	provider, ok := hook.(framework.TestMessageProvider)
	if !ok {
		response.NewErrorResponse(response.ServerError, "Telegram 插件不支持测试消息", "").FPrint(w)
		return
	}
	payload, err := json.Marshal(input.Settings)
	if err != nil {
		response.NewErrorResponse(response.ServerError, "测试消息参数处理失败", "").FPrint(w)
		return
	}
	if err := provider.TestMessage(payload); err != nil {
		log.WithError(err).Warn("发送 Telegram 测试消息失败")
		response.NewErrorResponse(response.ServerError, "发送 Telegram 测试消息失败", "").FPrint(w)
		return
	}
	response.NewSuccessResponse(map[string]string{"message": "测试消息已发送"}).FPrint(w)
}

func authenticatedPluginUser(ctx *context.Context) bool {
	return ctx != nil && ctx.UserID > 0
}

func decodePluginJSONBody(w http.ResponseWriter, req *http.Request, target any) bool {
	req.Body = http.MaxBytesReader(w, req.Body, maxTelegramUserSettingsBodyBytes)
	defer req.Body.Close()
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			response.NewErrorResponse(response.ParamsError, "请求内容过大", "").FPrint(w)
		} else {
			response.NewErrorResponse(response.ParamsError, "请求参数格式不正确", "").FPrint(w)
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		response.NewErrorResponse(response.ParamsError, "请求参数格式不正确", "").FPrint(w)
		return false
	}
	return true
}

func writeTelegramSettingsError(w http.ResponseWriter, err error) {
	if errors.Is(err, pluginservice.ErrInvalidTelegramChatID) {
		response.NewErrorResponse(response.ParamsError, "Telegram 聊天 ID 格式不正确", "").FPrint(w)
		return
	}
	if errors.Is(err, pluginservice.ErrPluginSettingConflict) {
		response.NewErrorResponse(response.ParamsError, "配置已被其他操作更新，请刷新后重试", "").FPrint(w)
		return
	}
	var denial *access.Denial
	if errors.As(err, &denial) {
		response.NewErrorResponse(response.NoAccessPrivileges, "无权修改插件设置", "").FPrint(w)
		return
	}
	log.WithError(err).Error("Telegram 插件设置操作失败")
	response.NewErrorResponse(response.ServerError, "插件设置操作失败", "").FPrint(w)
}
