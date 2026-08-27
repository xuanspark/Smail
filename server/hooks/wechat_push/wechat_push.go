package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/credential"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	urlpkg "net/url"
	"os"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxWeChatResponseBytes = 1 << 20
	maxWeChatMessageBytes  = 4 << 10
)

var weChatHTTPClient = &http.Client{Timeout: 10 * time.Second}

type accessTokenRes struct {
	AccessToken string `db:"access_token" json:"access_token"`
	ExpiresIn   int    `db:"expires_in" json:"expires_in"`
}

type WeChatPushHook struct {
	appId        string
	secret       string
	tokenMu      sync.Mutex
	token        string
	tokenExpires int64
	templateId   string
	pushUser     string
	webDomain    string
	httpsEnabled int
}

func (w *WeChatPushHook) GetName(ctx *context.Context) string {
	return "WeChatPushHook"
}

// SettingsHtml 插件页面
func (w *WeChatPushHook) SettingsHtml(ctx *context.Context, url string, requestData string) string {
	return fmt.Sprintf(`
<div>
	 Wechat push No Settings Page
</div>
`)
}

func (w *WeChatPushHook) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail) {
	if email == nil || w.appId == "" || w.secret == "" || w.pushUser == "" {
		return
	}

	for _, u := range ue {
		if u == nil {
			continue
		}
		// 管理员（Uid=1）收到邮件且非已读、非已删除 触发通知
		if u.UserID == 1 && u.IsRead == 0 && u.Status == 0 && email.MessageId > 0 {
			w.sendUserMsg(ctx, w.pushUser, buildWeChatMessage(email))
		}
	}

}

func (w *WeChatPushHook) SendBefore(ctx *context.Context, email *parsemail.Email) {

}

func (w *WeChatPushHook) SendAfter(ctx *context.Context, email *parsemail.Email, err map[string]error) {

}

func (w *WeChatPushHook) ReceiveParseBefore(ctx *context.Context, email *[]byte) {

}

func (w *WeChatPushHook) ReceiveParseAfter(ctx *context.Context, email *parsemail.Email) {}

func (w *WeChatPushHook) getWxAccessToken() string {
	w.tokenMu.Lock()
	defer w.tokenMu.Unlock()

	if w.token != "" && w.tokenExpires > time.Now().Unix() {
		return w.token
	}
	appID, err := credential.ValidateSecret(w.appId, 256)
	if err != nil {
		return ""
	}
	secret, err := credential.ValidateSecret(w.secret, 4096)
	if err != nil {
		return ""
	}

	endpoint, _ := urlpkg.Parse("https://api.weixin.qq.com/cgi-bin/token")
	query := endpoint.Query()
	query.Set("grant_type", "client_credential")
	query.Set("appid", appID)
	query.Set("secret", secret)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ""
	}
	resp, err := weChatHTTPClient.Do(request)
	if err != nil || resp == nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWeChatResponseBytes+1))
	if err != nil || len(body) > maxWeChatResponseBytes {
		return ""
	}
	var ret accessTokenRes
	if json.Unmarshal(body, &ret) != nil || ret.ExpiresIn <= 0 {
		return ""
	}
	if token, err := credential.ValidateSecret(ret.AccessToken, 4096); err == nil {
		w.token = token
		w.tokenExpires = time.Now().Unix() + int64(ret.ExpiresIn)
	}
	return w.token
}

type sendMsgRequest struct {
	Touser      string   `db:"touser" json:"touser"`
	Template_id string   `db:"template_id" json:"template_id"`
	Url         string   `db:"url" json:"url"`
	Data        SendData `db:"data" json:"data"`
}
type SendData struct {
	Content DataItem `json:"Content"`
}
type DataItem struct {
	Value string `json:"value"`
	Color string `json:"color"`
}

func (w *WeChatPushHook) sendUserMsg(ctx *context.Context, userId string, content string) {
	content = truncateWeChatText(content, maxWeChatMessageBytes)
	url := w.webDomain
	if w.httpsEnabled > 1 {
		url = "http://" + url
	} else {
		url = "https://" + url
	}

	sendMsgReq, err := json.Marshal(sendMsgRequest{
		Touser:      userId,
		Template_id: w.templateId,
		Url:         url,
		Data:        SendData{Content: DataItem{Value: content, Color: "#000000"}},
	})
	if err != nil {
		log.WithContext(ctx).Error("wechat push request build failed")
		return
	}
	token := w.getWxAccessToken()
	if token == "" {
		log.WithContext(ctx).Error("wechat push authentication failed")
		return
	}
	endpoint, _ := urlpkg.Parse("https://api.weixin.qq.com/cgi-bin/message/template/send")
	query := endpoint.Query()
	query.Set("access_token", token)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(sendMsgReq))
	if err != nil {
		log.WithContext(ctx).Error("wechat push request build failed")
		return
	}
	request.Header.Set("Content-Type", "application/json")
	resp, err := weChatHTTPClient.Do(request)
	if err != nil || resp == nil {
		log.WithContext(ctx).Error("wechat push request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		log.WithContext(ctx).Error("wechat push request was rejected")
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWeChatResponseBytes))
}

func buildWeChatMessage(email *parsemail.Email) string {
	if email == nil {
		return ""
	}
	const prefix = "<<"
	const separator = ">>  "
	subjectLimit := maxWeChatMessageBytes - len(prefix) - len(separator)
	subject := truncateWeChatText(email.Subject, subjectLimit)
	remaining := maxWeChatMessageBytes - len(prefix) - len(subject) - len(separator)
	text := truncateWeChatText(string(email.Text), remaining)
	return prefix + subject + separator + text
}

func truncateWeChatText(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func NewWechatPushHook() *WeChatPushHook {
	httpsEnabled := 0
	if value, err := strconv.Atoi(os.Getenv("SMAIL_HTTPS_ENABLED")); err == nil {
		httpsEnabled = value
	}
	return &WeChatPushHook{
		appId:        os.Getenv("SMAIL_WECHAT_APP_ID"),
		secret:       os.Getenv("SMAIL_WECHAT_SECRET"),
		templateId:   os.Getenv("SMAIL_WECHAT_TEMPLATE_ID"),
		pushUser:     os.Getenv("SMAIL_WECHAT_USER_ID"),
		webDomain:    os.Getenv("SMAIL_WEB_DOMAIN"),
		httpsEnabled: httpsEnabled,
	}
}

// 插件将以独立进程运行，因此需要主函数。
func main() {
	if err := framework.CreatePlugin("wechat_push", NewWechatPushHook()).Run(); err != nil {
		log.WithError(err).Error("wechat push plugin stopped")
		os.Exit(1)
	}
}
