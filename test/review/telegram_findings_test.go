package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"github.com/go-telegram/bot"
	"github.com/ydzydzydz/pmail_telegram_push/model"
)

type auditTelegramRequest struct {
	method string
	fields map[string]string
	files  int
}

type auditTelegramClient struct {
	requests []auditTelegramRequest
}

func (c *auditTelegramClient) Do(request *http.Request) (*http.Response, error) {
	// 完整消费 SDK 的管道请求体，不建立任何网络连接。
	data, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	defer request.Body.Close()
	_, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil {
		return nil, err
	}
	reader := multipart.NewReader(bytes.NewReader(data), params["boundary"])
	captured := auditTelegramRequest{method: path.Base(request.URL.Path), fields: make(map[string]string)}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		value, err := io.ReadAll(part)
		if err != nil {
			return nil, err
		}
		captured.fields[part.FormName()] = string(value)
		if part.FileName() != "" {
			captured.files++
		}
	}
	c.requests = append(c.requests, captured)
	result := `{"ok":true,"result":{"message_id":1}}`
	if captured.method == "sendMediaGroup" {
		result = `{"ok":true,"result":[]}`
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(result)), Header: make(http.Header)}, nil
}

func auditTelegramHook(t *testing.T) (*PmailTelegramPushHook, *auditTelegramClient) {
	t.Helper()
	client := &auditTelegramClient{}
	instance, err := bot.New("1:audit-placeholder-not-a-real-token", bot.WithSkipGetMe(), bot.WithHTTPClient(time.Second, client))
	if err != nil {
		t.Fatal(err)
	}
	hook := NewPmailTelegramPushHook()
	hook.bot = instance
	hook.pluginConfig = pluginSettings{Timeout: 5}
	return hook, client
}

func TestAuditDisabledAttachmentsMustNotBeSent(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%v", enabled), func(t *testing.T) {
			hook, client := auditTelegramHook(t)
			settings := json.RawMessage(fmt.Sprintf(`{"chatId":"123","sendAttachments":%v}`, enabled))
			if err := hook.ConfigureUserSettings(1, settings); err != nil {
				t.Fatal(err)
			}
			email := &parsemail.Email{MessageId: 1, From: &parsemail.User{EmailAddress: "sender@example.invalid"},
				Attachments: []*parsemail.Attachment{
					{Filename: "first.txt", Content: []byte("audit-first")},
					{Filename: "second.txt", Content: []byte("audit-second")},
				}}
			hook.ReceiveSaveAfter(nil, email, []*models.UserEmail{{UserID: 1}})
			messages, files := 0, 0
			for _, request := range client.requests {
				if request.method == "sendMessage" {
					messages++
				}
				files += request.files
			}
			t.Logf("enabled=%v messages=%d uploaded_files=%d", enabled, messages, files)
			if messages != 1 {
				t.Fatalf("通知调用链未成功执行：%d", messages)
			}
			if !enabled && files != 0 {
				t.Errorf("已关闭附件发送，仍上传 %d 个附件", files)
			}
			if enabled && files != 2 {
				t.Errorf("启用附件发送的对照未上传完整附件")
			}
		})
	}
}

func TestAuditTelegramPlainTextMustBeEscaped(t *testing.T) {
	hook, client := auditTelegramHook(t)
	subject := "<audit-subject> & status"
	content := "<audit-body> & details"
	email := &parsemail.Email{From: &parsemail.User{}, Subject: subject, Text: []byte(content)}
	_, err := hook.sendText(email, &model.TelegramPushSetting{ChatID: "123", ShowContent: true})
	if err != nil {
		t.Fatal(err)
	}
	request := client.requests[0]
	t.Logf("parse_mode=%q raw_subject=%v raw_body=%v", request.fields["parse_mode"],
		strings.Contains(request.fields["text"], subject), strings.Contains(request.fields["text"], content))
	if request.fields["parse_mode"] != "HTML" {
		t.Fatal("测试前提不满足：未启用 HTML 解析")
	}
	if !strings.Contains(request.fields["text"], html.EscapeString(subject)) || !strings.Contains(request.fields["text"], html.EscapeString(content)) {
		t.Error("主题和纯文本正文没有作为普通文字转义")
	}
}

func TestAuditTelegramTruncationMustKeepUTF8(t *testing.T) {
	hook, _ := auditTelegramHook(t)
	for prefix := 0; prefix < 3; prefix++ {
		t.Run(fmt.Sprintf("prefix=%d", prefix), func(t *testing.T) {
			email := &parsemail.Email{From: &parsemail.User{}, Text: []byte(strings.Repeat("x", prefix) + strings.Repeat("中", 2000))}
			text := hook.buildSendText(email, &model.TelegramPushSetting{ShowContent: true})
			t.Logf("source_valid=%v output_bytes=%d output_valid=%v", utf8.Valid(email.Text), len(text), utf8.ValidString(text))
			if !utf8.ValidString(text) {
				t.Error("正常中文被截断为无效 UTF-8")
			}
		})
	}
}
