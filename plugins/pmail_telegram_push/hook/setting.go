package hook

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"git.pcux.cn/xuans/Smail/hooks/framework"
	"github.com/go-telegram/bot/models"
	"github.com/ydzydzydz/pmail_telegram_push/logger"
	telegrammodel "github.com/ydzydzydz/pmail_telegram_push/model"
)

var (
	//go:embed dist/index.html
	settingHTML embed.FS
)

func settingPage() string {
	content, err := settingHTML.ReadFile("dist/index.html")
	if err != nil {
		return "<div>Telegram 插件页面暂不可用</div>"
	}
	return string(content)
}

type Response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func SuccessResponse(message string, data any) *Response {
	return &Response{Code: 0, Message: message, Data: data}
}

func ErrorResponse(message string) *Response {
	return &Response{Code: -1, Message: message}
}

func (r *Response) Json() string {
	data, err := json.Marshal(r)
	if err != nil {
		logger.PluginLogger.Error().Err(err).Msg("序列化响应失败")
		return ""
	}
	return string(data)
}

func (h *PmailTelegramPushHook) getSetting(userID int) string {
	setting := h.settings.get(userID)
	return SuccessResponse("获取 Telegram 推送设置成功", setting).Json()
}

func (h *PmailTelegramPushHook) testMessage(userID int, requestData string) string {
	var setting telegrammodel.TelegramPushSetting
	if err := json.Unmarshal([]byte(requestData), &setting); err != nil {
		return ErrorResponse("测试消息请求格式不正确").Json()
	}
	setting.ChatID = strings.TrimSpace(setting.ChatID)
	if !validChatID(setting.ChatID) || setting.ChatID == "" {
		return ErrorResponse("测试消息 Chat ID 格式不正确").Json()
	}
	if h.bot == nil {
		return ErrorResponse("Telegram Bot 尚未配置").Json()
	}
	if _, err := h.sendTestMessage(&setting); err != nil {
		logger.PluginLogger.Error().Err(err).Msg("发送测试消息失败")
		return ErrorResponse("发送测试消息失败").Json()
	}
	return SuccessResponse("测试 Telegram 推送消息成功", nil).Json()
}

func (h *PmailTelegramPushHook) updateSetting(userID int, requestData string) string {
	var setting telegrammodel.TelegramPushSetting
	decoder := json.NewDecoder(strings.NewReader(requestData))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&setting); err != nil {
		return ErrorResponse("设置请求格式不正确").Json()
	}
	if err := h.settings.update(userID, telegramSetting{
		ChatID:             setting.ChatID,
		ShowContent:        setting.ShowContent,
		SpoilerContent:     setting.SpoilerContent,
		SendAttachments:    setting.SendAttachments,
		DisableLinkPreview: setting.DisableLinkPreview,
	}); err != nil {
		return ErrorResponse(err.Error()).Json()
	}
	return SuccessResponse("更新 Telegram 推送设置成功", nil).Json()
}

func NewBotInfo(username string) framework.BotInfo {
	return framework.BotInfo{Username: username, BotLink: fmt.Sprintf("https://t.me/%s", username)}
}

func (h *PmailTelegramPushHook) GetBotInfo() (framework.BotInfo, error) {
	if h.bot == nil {
		return framework.BotInfo{}, errors.New("Telegram Bot 尚未配置")
	}
	me, err := h.bot.GetMe(context.Background())
	if err != nil {
		return framework.BotInfo{}, errors.New("获取 Telegram Bot 信息失败")
	}
	return NewBotInfo(me.Username), nil
}

func (h *PmailTelegramPushHook) TestMessage(data json.RawMessage) error {
	if h.bot == nil {
		return errors.New("Telegram Bot 尚未配置")
	}
	payload, err := decodeTelegramUserSettings(data)
	if err != nil {
		return err
	}
	setting := payload.setting()
	if setting.ChatID == "" {
		return errors.New("Telegram 聊天 ID 不能为空")
	}
	telegramSetting := &telegrammodel.TelegramPushSetting{
		ChatID:             setting.ChatID,
		ShowContent:        setting.ShowContent,
		SpoilerContent:     setting.SpoilerContent,
		SendAttachments:    setting.SendAttachments,
		DisableLinkPreview: setting.DisableLinkPreview,
	}
	if _, err := h.sendTestMessage(telegramSetting); err != nil {
		return errors.New("发送测试消息失败")
	}
	return nil
}

func (h *PmailTelegramPushHook) getBotInfo() string {
	if h.bot == nil {
		return ErrorResponse("Telegram Bot 尚未配置").Json()
	}
	me, err := h.bot.GetMe(context.Background())
	if err != nil {
		return ErrorResponse("获取 Telegram Bot 信息失败").Json()
	}
	return SuccessResponse("获取 Telegram Bot 信息成功", NewBotInfo(me.Username)).Json()
}

var _ = models.Message{}
