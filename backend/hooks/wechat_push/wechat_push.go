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
	"net"
	"net/http"
	urlpkg "net/url"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	maxWeChatResponseBytes = 1 << 20
	maxWeChatMessageBytes  = 4 << 10
)

var weChatHTTPClient = credential.NewRestrictedHTTPClient(10_000)

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
	httpClient   *http.Client
	userSettings map[int]weChatUserSettings
}

type weChatUserSettings struct {
	AppID       string `json:"appId"`
	OpenID      string `json:"openId"`
	DisplayName string `json:"displayName"`
	Enabled     bool   `json:"enabled"`
	ShowContent bool   `json:"showContent"`
}

func (w *WeChatPushHook) ConfigureUserSettings(userID int, data json.RawMessage) error {
	if userID <= 0 || len(data) > 4096 || !json.Valid(data) {
		return fmt.Errorf("微信个人配置无效")
	}
	var settings weChatUserSettings
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil || settings.Enabled && (settings.OpenID == "" || settings.AppID == "") {
		return fmt.Errorf("微信个人配置无效")
	}
	w.tokenMu.Lock()
	defer w.tokenMu.Unlock()
	if w.userSettings == nil {
		w.userSettings = make(map[int]weChatUserSettings)
	}
	w.userSettings[userID] = settings
	return nil
}

func (w *WeChatPushHook) TestMessage(data json.RawMessage) error {
	var settings weChatUserSettings
	if len(data) > 4096 || json.Unmarshal(data, &settings) != nil || settings.OpenID == "" {
		return fmt.Errorf("请先绑定本人的微信")
	}
	w.tokenMu.Lock()
	appID := w.appId
	w.tokenMu.Unlock()
	if appID == "" || settings.AppID != appID {
		return fmt.Errorf("系统公众号已变更，请重新绑定")
	}
	return w.sendUserMsg(&context.Context{}, settings.OpenID, "这是一条 Smail 微信通知测试消息。")
}

func (w *WeChatPushHook) GetName(ctx *context.Context) string {
	return "WeChatPushHook"
}

// Capabilities 声明插件需要的能力和配置字段。
func (w *WeChatPushHook) Capabilities() framework.Capabilities {
	return framework.Capabilities{
		PluginID:     "wechat_push",
		Capabilities: []string{"network"},
		Settings:     []string{"appId", "templateId", "userId", "webDomain"},
		Protocol:     1,
	}
}

type weChatRuntimeConfig struct {
	PluginID     string                          `json:"pluginId"`
	Generation   uint64                          `json:"generation"`
	Settings     json.RawMessage                 `json:"settings,omitempty"`
	Secrets      map[string]string               `json:"secrets,omitempty"`
	Capabilities []string                        `json:"capabilities,omitempty"`
	Network      []framework.PluginNetworkTarget `json:"network,omitempty"`
}

type weChatSettings struct {
	AppID      string `json:"appId"`
	TemplateID string `json:"templateId"`
	UserID     string `json:"userId"`
	WebDomain  string `json:"webDomain"`
}

// ValidateConfig 只校验主程序下发的运行时配置，不读取本地配置文件或环境变量。
func (w *WeChatPushHook) ValidateConfig(data json.RawMessage) error {
	_, _, _, err := parseWeChatRuntimeConfig(data)
	return err
}

// Configure 应用主程序下发的运行时配置，并在配置变更后清理缓存令牌。
func (w *WeChatPushHook) Configure(data json.RawMessage) error {
	_, settings, secret, err := parseWeChatRuntimeConfig(data)
	if err != nil {
		return err
	}
	w.tokenMu.Lock()
	w.appId = settings.AppID
	w.secret = secret
	w.templateId = settings.TemplateID
	w.pushUser = settings.UserID
	w.webDomain = settings.WebDomain
	w.httpClient = credential.NewRestrictedHTTPClientForTargets(10_000, []credential.NetworkTarget{{Scheme: "https", Host: "api.weixin.qq.com", Port: 443}})
	w.token = ""
	w.tokenExpires = 0
	w.tokenMu.Unlock()
	return nil
}

// Health 检查插件是否已经完成安全运行时配置。
func (w *WeChatPushHook) Health() error {
	if w == nil {
		return fmt.Errorf("微信插件不可用")
	}
	w.tokenMu.Lock()
	configured := w.appId != "" && w.secret != "" && w.templateId != "" && w.webDomain != "" && w.httpClient != nil
	w.tokenMu.Unlock()
	if !configured {
		return fmt.Errorf("微信插件尚未完成运行时配置")
	}
	return nil
}

// SettingsHtml 插件页面
func (w *WeChatPushHook) SettingsHtml(ctx *context.Context, url string, requestData string) string {
	return fmt.Sprintf(`
<div>
	暂无设置页面
</div>
`)
}

func (w *WeChatPushHook) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail) {
	w.tokenMu.Lock()
	appID := w.appId
	secret := w.secret
	w.tokenMu.Unlock()
	if email == nil || appID == "" || secret == "" || email.MessageId <= 0 {
		return
	}

	seen := make(map[int]bool)
	for _, u := range ue {
		if u == nil || u.UserID <= 0 || u.IsRead != 0 || u.Status != 0 || seen[u.UserID] {
			continue
		}
		seen[u.UserID] = true
		w.tokenMu.Lock()
		settings := w.userSettings[u.UserID]
		w.tokenMu.Unlock()
		if !settings.Enabled || settings.OpenID == "" || settings.AppID != appID {
			continue
		}
		content := "您收到一封新邮件，请登录邮箱查看。"
		if settings.ShowContent {
			content = buildWeChatMessage(email)
		}
		if err := w.sendUserMsg(ctx, settings.OpenID, content); err != nil {
			log.WithContext(ctx).Warn("微信个人通知发送失败")
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
	client := w.httpClient
	if client == nil {
		client = weChatHTTPClient
	}
	resp, err := client.Do(request)
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

func (w *WeChatPushHook) sendUserMsg(ctx *context.Context, userId string, content string) error {
	content = truncateWeChatText(content, maxWeChatMessageBytes)
	w.tokenMu.Lock()
	webDomain := w.webDomain
	templateID := w.templateId
	client := w.httpClient
	w.tokenMu.Unlock()
	if client == nil {
		client = weChatHTTPClient
	}
	url := "https://" + webDomain

	sendMsgReq, err := json.Marshal(sendMsgRequest{
		Touser:      userId,
		Template_id: templateID,
		Url:         url,
		Data:        SendData{Content: DataItem{Value: content, Color: "#000000"}},
	})
	if err != nil {
		log.WithContext(ctx).Error("wechat push request build failed")
		return fmt.Errorf("微信推送请求构造失败")
	}
	token := w.getWxAccessToken()
	if token == "" {
		log.WithContext(ctx).Error("wechat push authentication failed")
		return fmt.Errorf("微信公众号认证失败")
	}
	endpoint, _ := urlpkg.Parse("https://api.weixin.qq.com/cgi-bin/message/template/send")
	query := endpoint.Query()
	query.Set("access_token", token)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(sendMsgReq))
	if err != nil {
		log.WithContext(ctx).Error("wechat push request build failed")
		return fmt.Errorf("微信推送请求构造失败")
	}
	request.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(request)
	if err != nil || resp == nil {
		log.WithContext(ctx).Error("wechat push request failed")
		return fmt.Errorf("微信推送请求失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		log.WithContext(ctx).Error("wechat push request was rejected")
		return fmt.Errorf("微信推送请求被拒绝")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWeChatResponseBytes+1))
	var result struct {
		ErrorCode *int `json:"errcode"`
	}
	if err != nil || len(body) > maxWeChatResponseBytes || json.Unmarshal(body, &result) != nil || result.ErrorCode == nil {
		log.WithContext(ctx).Error("微信推送响应无效或超过大小限制")
		return fmt.Errorf("微信推送响应无效")
	}
	if *result.ErrorCode != 0 {
		// 只记录业务码，不能输出可能含凭据或收件人信息的响应正文。
		log.WithContext(ctx).WithField("errcode", *result.ErrorCode).Error("微信推送业务请求失败")
		switch *result.ErrorCode {
		case 40001, 40014, 42001:
			w.tokenMu.Lock()
			if w.token == token {
				w.token, w.tokenExpires = "", 0
			}
			w.tokenMu.Unlock()
		}
		return fmt.Errorf("微信推送失败，业务码：%d", *result.ErrorCode)
	}
	return nil
}

func buildWeChatMessage(email *parsemail.Email) string {
	if email == nil {
		return ""
	}
	if code, ok := extractWeChatVerificationCode(email); ok {
		return "验证码：" + code
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
	return &WeChatPushHook{}
}

func parseWeChatRuntimeConfig(data json.RawMessage) (weChatRuntimeConfig, weChatSettings, string, error) {
	var payload weChatRuntimeConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信插件运行时配置无效")
	}
	if payload.PluginID != "wechat_push" {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信插件 ID 不匹配")
	}
	if payload.Generation == 0 {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信插件配置代次无效")
	}
	if !hasOnlyWeChatCapability(payload.Capabilities, "network") {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信插件网络权限未配置")
	}
	if len(payload.Settings) == 0 || len(payload.Settings) > maxWeChatResponseBytes {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信插件配置无效")
	}
	var settings weChatSettings
	settingsDecoder := json.NewDecoder(bytes.NewReader(payload.Settings))
	settingsDecoder.DisallowUnknownFields()
	if err := settingsDecoder.Decode(&settings); err != nil || settingsDecoder.Decode(&struct{}{}) != io.EOF {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信插件配置无效")
	}
	appID, err := credential.ValidateSecret(settings.AppID, 256)
	if err != nil {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信应用 ID 无效")
	}
	templateID, err := credential.ValidateSecret(settings.TemplateID, 256)
	if err != nil {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信模板 ID 无效")
	}
	webDomain, err := normalizeWeChatWebDomain(settings.WebDomain)
	if err != nil {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信回调域名无效")
	}
	secret, err := credential.ValidateSecret(payload.Secrets["appSecret"], 4096)
	if err != nil {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信应用密钥未配置")
	}
	for name := range payload.Secrets {
		if name != "appSecret" {
			return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信插件密钥字段无效")
		}
	}
	if !hasOnlyWeChatNetworkTarget(payload.Network) {
		return weChatRuntimeConfig{}, weChatSettings{}, "", fmt.Errorf("微信网络权限未配置")
	}
	settings.AppID = appID
	settings.TemplateID = templateID
	settings.WebDomain = webDomain
	return payload, settings, secret, nil
}

func hasOnlyWeChatCapability(capabilities []string, expected string) bool {
	return len(capabilities) == 1 && capabilities[0] == expected
}

func hasOnlyWeChatNetworkTarget(targets []framework.PluginNetworkTarget) bool {
	return len(targets) == 1 && strings.EqualFold(targets[0].Scheme, "https") && strings.EqualFold(targets[0].Host, "api.weixin.qq.com") && targets[0].Port == 443
}

func normalizeWeChatWebDomain(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/\\?#\x00\r\n\t ") {
		return "", fmt.Errorf("invalid domain")
	}
	if net.ParseIP(strings.Trim(value, "[]")) != nil {
		return "", fmt.Errorf("invalid domain")
	}
	parsed, err := urlpkg.Parse("https://" + value)
	if err != nil || parsed.Host != value || parsed.Hostname() == "" || parsed.User != nil {
		return "", fmt.Errorf("invalid domain")
	}
	return strings.TrimSuffix(strings.ToLower(parsed.Host), "."), nil
}

// 插件将以独立进程运行，因此需要主函数。
func main() {
	if err := framework.CreatePlugin("wechat_push", NewWechatPushHook()).Run(); err != nil {
		log.WithError(err).Error("wechat push plugin stopped")
		os.Exit(1)
	}
}
