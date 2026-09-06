package hook

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"path/filepath"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/ydzydzydz/pmail_telegram_push/model"
)

// escapeTelegramText 在完整字符和实体边界裁剪，再返回可安全嵌入 Telegram HTML 的文本。
func escapeTelegramText(text string, limit int) string {
	if limit < 3 {
		return ""
	}
	var result strings.Builder
	for _, char := range text {
		escaped := html.EscapeString(string(char))
		if result.Len()+len(escaped) > limit-3 {
			return result.String() + "..."
		}
		result.WriteString(escaped)
	}
	return result.String()
}

func telegramAddresses(users []*parsemail.User) string {
	var result strings.Builder
	for _, user := range users {
		if user == nil || user.EmailAddress == "" {
			continue
		}
		remaining := 480 - result.Len()
		if remaining < 12 {
			break
		}
		result.WriteString("&lt;" + escapeTelegramText(user.EmailAddress, remaining-9) + "&gt; ")
	}
	return result.String()
}

const (
	TELEGRAM_TEXT_MAX_SIZE        = 4096             // TELEGRAM_TEXT_MAX_SIZE Telegram 文本最大长度
	TELEGRAM_ATTACHMENT_MAX_COUNT = 10               // TELEGRAM_ATTACHMENT_MAX_COUNT Telegram 附件最大数量
	TELEGRAM_ATTACHMENT_MAX_SIZE  = 20 * 1024 * 1024 // TELEGRAM_ATTACHMENT_MAX_SIZE Telegram 附件最大大小
)

// getSubjectText 获取主题文本
func (h *PmailTelegramPushHook) getSubjectText(email *parsemail.Email) string {
	if len(email.Subject) <= 0 {
		return ""
	}
	return fmt.Sprintf("🔖 主题：<b>%s</b>\n", escapeTelegramText(email.Subject, 600))
}

// getFromText 获取发件人文本
func (h *PmailTelegramPushHook) getFromText(email *parsemail.Email) string {
	if email.From == nil || len(email.From.EmailAddress) <= 0 {
		return ""
	}
	return fmt.Sprintf("📤 发件：&lt;%s&gt;\n", escapeTelegramText(email.From.EmailAddress, 256))
}

// getToText 获取收件人文本
func (h *PmailTelegramPushHook) getToText(email *parsemail.Email) string {
	if len(email.To) <= 0 {
		return ""
	}
	return "📥 收件：" + telegramAddresses(email.To) + "\n"
}

// getCcText 获取抄送人文本
func (h *PmailTelegramPushHook) getCcText(email *parsemail.Email) string {
	if len(email.Cc) <= 0 {
		return ""
	}
	return "📋 抄送：" + telegramAddresses(email.Cc) + "\n"
}

// getBccText 获取密送人文本
func (h *PmailTelegramPushHook) getBccText(email *parsemail.Email) string {
	if len(email.Bcc) <= 0 {
		return ""
	}
	return "🕵️ 密送：" + telegramAddresses(email.Bcc) + "\n"
}

// getAttachmentsText 获取附件文本
func (h *PmailTelegramPushHook) getAttachmentsText(email *parsemail.Email) string {
	if len(email.Attachments) <= 0 {
		return ""
	}
	return fmt.Sprintf("📎 附件：%d 个\n", len(email.Attachments))
}

// getContentText 获取邮件内容文本
func (h *PmailTelegramPushHook) getContentText(email *parsemail.Email, setting *model.TelegramPushSetting) string {
	if !setting.ShowContent {
		return ""
	}
	if len(email.Text) > 0 {
		return string(email.Text)
	}
	if len(email.HTML) > 0 {
		return removeHTMLTag(string(email.HTML))
	}
	return ""
}

// getSpoilerText 获取spoiler文本
func (h *PmailTelegramPushHook) getSpoilerText(text string, setting *model.TelegramPushSetting) string {
	if !setting.SpoilerContent {
		return text
	}
	return fmt.Sprintf("<tg-spoiler>%s</tg-spoiler>", text)
}

// buildSendText 构建发送文本
func (h *PmailTelegramPushHook) buildSendText(email *parsemail.Email, setting *model.TelegramPushSetting) string {
	text := "📧 有新邮件\n"
	text += h.getSubjectText(email)
	text += h.getFromText(email)
	text += h.getToText(email)
	text += h.getCcText(email)
	text += h.getBccText(email)
	text += h.getAttachmentsText(email)
	// 按更保守的 UTF-8 字节预算分配正文，标签与实体不参与截断。
	remaining := TELEGRAM_TEXT_MAX_SIZE - len(text) - 20
	if setting.SpoilerContent {
		remaining -= len("<tg-spoiler></tg-spoiler>")
	}
	content := escapeTelegramText(h.getContentText(email, setting), remaining)
	return removeExtraSpace(text + h.getSpoilerText(content, setting))
}

// buildPamilLinkButton 创建邮件页面链接按钮
func (h *PmailTelegramPushHook) buildPamilLinkButton() *models.InlineKeyboardMarkup {
	domain := strings.TrimSpace(h.pluginConfig.WebDomain)
	if domain == "" {
		return nil
	}
	scheme := "https"
	if !h.pluginConfig.HTTPSEnabled {
		scheme = "http"
	}
	return &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{{{
			Text: "查收邮件",
			URL:  scheme + "://" + domain,
		}}},
	}
}

// sendText 发送文本消息
// sendText 发送文本消息
func (h *PmailTelegramPushHook) sendText(email *parsemail.Email, setting *model.TelegramPushSetting) (msg *models.Message, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.pluginConfig.Timeout)*time.Second)
	defer cancel()

	parmas := &bot.SendMessageParams{
		ChatID:      setting.ChatID,
		Text:        h.buildSendText(email, setting),
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: h.buildPamilLinkButton(),
		LinkPreviewOptions: &models.LinkPreviewOptions{
			IsDisabled: &setting.DisableLinkPreview,
		},
	}

	return h.bot.SendMessage(ctx, parmas)
}

// sendNotification 发送通知消息
// 先发送文本消息，再发送附件消息
func (h *PmailTelegramPushHook) sendNotification(email *parsemail.Email, setting *model.TelegramPushSetting) (err error) {
	msg, err := h.sendText(email, setting)
	if err != nil {
		return err
	}
	return h.sendAttachmentsBatch(msg.ID, email, setting)
}

// sendAttachmentsBatch 批量发送附件消息
func (h *PmailTelegramPushHook) sendAttachmentsBatch(id int, email *parsemail.Email, setting *model.TelegramPushSetting) (err error) {
	if !setting.SendAttachments {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.pluginConfig.Timeout)*time.Second)
	defer cancel()

	// 引用消息中包含附件关键字
	params := &bot.SendMediaGroupParams{
		ChatID: setting.ChatID,
		ReplyParameters: &models.ReplyParameters{
			MessageID: id,
			Quote:     fmt.Sprintf("📎 附件：%d 个", len(email.Attachments)),
		},
	}

	// 批量发送附件, 每个批次最多 TELEGRAM_ATTACHMENT_MAX_COUNT 个附件
	for i := 0; i < len(email.Attachments); i += TELEGRAM_ATTACHMENT_MAX_COUNT {
		end := min(len(email.Attachments), i+TELEGRAM_ATTACHMENT_MAX_COUNT)
		params.Media = nil
		batch := email.Attachments[i:end]
		for j, attachment := range batch {
			// 判断文件大小, 超过最大大小则跳过
			if len(attachment.Content) > TELEGRAM_ATTACHMENT_MAX_SIZE {
				continue
			}
			// 构建 InputMediaDocument
			params.Media = append(params.Media, &models.InputMediaDocument{
				Media:           fmt.Sprintf("attach://%s", filepath.Base(attachment.Filename)),
				Caption:         fmt.Sprintf("📎 附件 %d", i+j+1),
				MediaAttachment: bytes.NewReader(attachment.Content),
			})
		}
		if len(params.Media) == 0 {
			continue
		}
		if _, err = h.bot.SendMediaGroup(ctx, params); err != nil {
			return err
		}
		// 每个批次发送后休息 1 秒, 避免触发速率限制
		time.Sleep(time.Second)
	}

	return nil
}

func (h *PmailTelegramPushHook) sendTestMessage(setting *model.TelegramPushSetting) (msg *models.Message, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(h.pluginConfig.Timeout)*time.Second)
	defer cancel()
	parmas := &bot.SendMessageParams{
		ChatID:    setting.ChatID,
		Text:      "这是一条测试消息",
		ParseMode: models.ParseModeHTML,
	}
	return h.bot.SendMessage(ctx, parmas)
}
