package hook

import (
	"encoding/json"
	"errors"
	"strings"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/go-telegram/bot"
	"github.com/ydzydzydz/pmail_telegram_push/logger"
	telegrammodel "github.com/ydzydzydz/pmail_telegram_push/model"
)

const PLUGIN_NAME = "telegram_push"

type PmailTelegramPushHook struct {
	bot          *bot.Bot
	pluginConfig pluginSettings
	settings     *settingStore
}

func NewPmailTelegramPushHook() *PmailTelegramPushHook {
	return &PmailTelegramPushHook{settings: newSettingStore()}
}

var _ framework.EmailHook = (*PmailTelegramPushHook)(nil)
var _ framework.CapabilityProvider = (*PmailTelegramPushHook)(nil)
var _ framework.ConfigurableHook = (*PmailTelegramPushHook)(nil)

func (h *PmailTelegramPushHook) Capabilities() framework.Capabilities {
	return framework.Capabilities{
		PluginID:     PLUGIN_NAME,
		Capabilities: []string{"network", "userSettings", "settingsHtml"},
		Settings:     []string{"proxy", "timeout", "debug", "webDomain", "httpsEnabled"},
		Protocol:     1,
	}
}

func (h *PmailTelegramPushHook) ValidateConfig(data json.RawMessage) error {
	_, _, _, err := parseRuntimeConfig(data)
	return err
}

func (h *PmailTelegramPushHook) Configure(data json.RawMessage) error {
	_, settings, token, err := parseRuntimeConfig(data)
	if err != nil {
		return err
	}
	newBot, err := NewBot(settings, token)
	if err != nil {
		return errors.New("Telegram Bot 初始化失败")
	}
	h.pluginConfig = settings
	h.bot = newBot
	return nil
}

func (h *PmailTelegramPushHook) Health() error {
	if h.bot == nil {
		return errors.New("Telegram Bot 尚未配置")
	}
	return nil
}

func (h *PmailTelegramPushHook) GetName(ctx *context.Context) string {
	return PLUGIN_NAME
}

func (h *PmailTelegramPushHook) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail) {
	if h.bot == nil || email == nil || email.MessageId <= 0 {
		return
	}
	for _, userEmail := range ue {
		if userEmail == nil || userEmail.IsRead != 0 || userEmail.Status != 0 {
			continue
		}
		setting := h.settings.get(userEmail.UserID)
		if setting.ChatID == "" {
			continue
		}
		telegramSettingModel := &telegrammodel.TelegramPushSetting{
			ChatID:             setting.ChatID,
			ShowContent:        setting.ShowContent,
			SpoilerContent:     setting.SpoilerContent,
			SendAttachments:    setting.SendAttachments,
			DisableLinkPreview: setting.DisableLinkPreview,
		}
		if err := h.sendNotification(email, telegramSettingModel); err != nil {
			logger.PluginLogger.Error().Err(err).Int64("email_message_id", email.MessageId).Msg("发送通知失败")
		}
	}
}

func (h *PmailTelegramPushHook) ReceiveParseBefore(ctx *context.Context, email *[]byte)         {}
func (h *PmailTelegramPushHook) ReceiveParseAfter(ctx *context.Context, email *parsemail.Email) {}
func (h *PmailTelegramPushHook) SendAfter(ctx *context.Context, email *parsemail.Email, err map[string]error) {
}
func (h *PmailTelegramPushHook) SendBefore(ctx *context.Context, email *parsemail.Email) {}

func (h *PmailTelegramPushHook) SettingsHtml(ctx *context.Context, url string, requestData string) string {
	if ctx == nil {
		return ErrorResponse("用户上下文无效").Json()
	}
	switch {
	case strings.Contains(url, "getSetting"):
		return h.getSetting(ctx.UserID)
	case strings.Contains(url, "getBotInfo"):
		return h.getBotInfo()
	case strings.Contains(url, "updateSetting"):
		return h.updateSetting(ctx.UserID, requestData)
	case strings.Contains(url, "testMessage"):
		return h.testMessage(ctx.UserID, requestData)
	default:
		return settingPage()
	}
}
