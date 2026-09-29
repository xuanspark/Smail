package controllers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/services/access"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

const maxTelegramUserSettingsBodyBytes int64 = 16 << 10

func GetTelegramUserSettings(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	TelegramUserSettings(ctx, w, req)
}

func UpdateTelegramUserSettings(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	TelegramUserSettings(ctx, w, req)
}

func GetTelegramBotInfo(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !authorizedPluginUser(ctx) {
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
	TestBoundTelegramMessage(ctx, w, req)
}

func authenticatedPluginUser(ctx *context.Context) bool {
	return ctx != nil && ctx.UserID > 0
}

func authorizedPluginUser(ctx *context.Context) bool {
	return authenticatedPluginUser(ctx) && authorizeCapability(ctx, access.CapabilityPluginRead, "", true) == nil
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
	var notice pluginservice.TelegramNotice
	if errors.Is(err, pluginservice.ErrTelegramBinding) || errors.As(err, &notice) {
		response.NewErrorResponse(response.ParamsError, err.Error(), "").FPrint(w)
		return
	}
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
