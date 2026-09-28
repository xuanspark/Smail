package plugin

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

const WeChatCallbackPath = "/api/v1/plugin/binding/wechat/callback"

// NotificationBindingActor 将绑定限制在发起扫码的登录会话，角色不影响本人绑定。
type NotificationBindingActor struct {
	UserID          int
	SessionVersion  int
	SecurityVersion int
	SessionHash     string
}

type WeChatBindingStatus struct {
	Ticket      string    `json:"ticket,omitempty"`
	URL         string    `json:"url,omitempty"`
	Phase       string    `json:"phase"`
	DisplayName string    `json:"displayName,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type weChatBinding struct {
	Actor       NotificationBindingActor
	Version     uint64
	Fingerprint string
	Settings    WeChatUserSettings
	Status      WeChatBindingStatus
}

type weChatBindingConfig struct {
	AppID       string
	Secret      string
	Callback    string
	Fingerprint string
}

func (s *ConfigService) weChatConfiguration() (weChatBindingConfig, error) {
	var result weChatBindingConfig
	manager := config.CurrentManager()
	current := config.CurrentConfig()
	if manager == nil || current == nil {
		return result, WeChatNotice("请联系超级管理员配置微信公众号")
	}
	instance, exists := manager.Snapshot().Plugins.Plugins[weChatPluginID]
	if !exists || !instance.Enabled {
		return result, WeChatNotice("请联系超级管理员配置并启用微信公众号")
	}
	var settings struct {
		AppID      string `json:"appId"`
		TemplateID string `json:"templateId"`
		WebDomain  string `json:"webDomain"`
	}
	if json.Unmarshal(instance.Settings, &settings) != nil || !validWeChatIdentity(settings.AppID) || settings.TemplateID == "" {
		return result, WeChatNotice("请联系超级管理员补全微信公众号配置")
	}
	domain := strings.TrimSpace(current.WebDomain)
	parsed, err := url.Parse("https://" + domain)
	if err != nil || domain == "" || parsed.Host != domain || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || !strings.EqualFold(settings.WebDomain, domain) {
		return result, WeChatNotice("公众号授权域名与后台域名不一致，请联系超级管理员")
	}
	secret, err := s.openSecretForPlugin(weChatPluginID, "appSecret")
	if err != nil || secret == "" {
		return result, WeChatNotice("请联系超级管理员配置微信公众号密钥")
	}
	fingerprint, err := s.vault.Fingerprint(settings.AppID + "\x00" + secret + "\x00" + domain)
	if err != nil {
		return result, ErrPluginStoreUnavailable
	}
	return weChatBindingConfig{AppID: settings.AppID, Secret: secret, Callback: "https://" + domain + WeChatCallbackPath, Fingerprint: fingerprint}, nil
}

func (s *ConfigService) WeChatBindingAvailability() (bool, string) {
	if _, err := s.weChatConfiguration(); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (s *ConfigService) validateBindingActor(actor NotificationBindingActor) error {
	user, err := s.activeNotificationUser(actor.UserID)
	if err != nil || len(actor.SessionHash) != 64 || user.SessionVersion != actor.SessionVersion || user.SecurityVersion != actor.SecurityVersion {
		return ErrWeChatBinding
	}
	return nil
}

func (s *ConfigService) BeginWeChatBinding(actor NotificationBindingActor) (WeChatBindingStatus, error) {
	if err := s.validateBindingActor(actor); err != nil {
		return WeChatBindingStatus{}, err
	}
	configuration, err := s.weChatConfiguration()
	if err != nil {
		return WeChatBindingStatus{}, err
	}
	current, err := s.GetOwnWeChatSettings(actor.UserID)
	if err != nil {
		return WeChatBindingStatus{}, err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return WeChatBindingStatus{}, ErrPluginStoreUnavailable
	}
	ticket := base64.RawURLEncoding.EncodeToString(nonce[:])
	now := s.currentTime()
	status := WeChatBindingStatus{Ticket: ticket, Phase: "waiting", ExpiresAt: now.Add(5 * time.Minute)}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	if s.wechatBindings == nil {
		s.wechatBindings = make(map[string]weChatBinding)
	}
	for key, binding := range s.wechatBindings {
		if !binding.Status.ExpiresAt.After(now) || binding.Actor.UserID == actor.UserID {
			delete(s.wechatBindings, key)
		}
	}
	if len(s.wechatBindings) >= 2048 {
		return WeChatBindingStatus{}, WeChatNotice("当前绑定请求较多，请稍后重试")
	}
	s.wechatBindings[ticket] = weChatBinding{Actor: actor, Version: current.Version, Fingerprint: configuration.Fingerprint, Status: status}
	query := url.Values{"appid": {configuration.AppID}, "redirect_uri": {configuration.Callback},
		"response_type": {"code"}, "scope": {"snsapi_userinfo"}, "state": {ticket}}
	status.URL = "https://open.weixin.qq.com/connect/oauth2/authorize?" + query.Encode() + "#wechat_redirect"
	return status, nil
}

func (s *ConfigService) GetWeChatBinding(actor NotificationBindingActor, ticket string) (WeChatBindingStatus, error) {
	if err := s.validateBindingActor(actor); err != nil {
		return WeChatBindingStatus{}, err
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	binding, found := s.wechatBindings[ticket]
	if !found || binding.Actor != actor || !binding.Status.ExpiresAt.After(s.currentTime()) {
		return WeChatBindingStatus{}, ErrWeChatBinding
	}
	return binding.Status, nil
}

func (s *ConfigService) CancelWeChatBinding(actor NotificationBindingActor, ticket string) error {
	if err := s.validateBindingActor(actor); err != nil {
		return err
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	// 过期、回调失败或重复取消都允许关闭页面，但不得撤销其他会话的请求。
	if binding, found := s.wechatBindings[ticket]; found && binding.Actor != actor {
		return ErrWeChatBinding
	}
	delete(s.wechatBindings, ticket)
	return nil
}

// ClaimWeChatBinding 只认领一次外部身份，不直接写入个人设置；仍需原页面确认。
func (s *ConfigService) ClaimWeChatBinding(ctx context.Context, ticket, code string) error {
	if s == nil || len(ticket) != 43 || code == "" || len(code) > 512 {
		return ErrWeChatBinding
	}
	s.bindingMu.Lock()
	binding, found := s.wechatBindings[ticket]
	if !found || binding.Status.Phase != "waiting" || !binding.Status.ExpiresAt.After(s.currentTime()) {
		s.bindingMu.Unlock()
		return ErrWeChatBinding
	}
	binding.Status.Phase = "processing"
	s.wechatBindings[ticket] = binding
	s.bindingMu.Unlock()
	accepted := false
	defer func() {
		if !accepted {
			s.bindingMu.Lock()
			delete(s.wechatBindings, ticket)
			s.bindingMu.Unlock()
		}
	}()
	if err := s.validateBindingActor(binding.Actor); err != nil {
		return err
	}
	configuration, err := s.weChatConfiguration()
	if err != nil || configuration.Fingerprint != binding.Fingerprint {
		return ErrWeChatBinding
	}
	var token struct {
		AccessToken string `json:"access_token"`
		OpenID      string `json:"openid"`
		ErrorCode   int    `json:"errcode"`
	}
	if err := requestWeChatBindingJSON(ctx, "/sns/oauth2/access_token", url.Values{
		"appid": {configuration.AppID}, "secret": {configuration.Secret}, "code": {code}, "grant_type": {"authorization_code"},
	}, &token); err != nil || token.ErrorCode != 0 || !validWeChatIdentity(token.OpenID) || token.AccessToken == "" || len(token.AccessToken) > 4096 {
		return WeChatNotice("微信授权失败，请重新扫码")
	}
	var profile struct {
		OpenID    string `json:"openid"`
		Nickname  string `json:"nickname"`
		ErrorCode int    `json:"errcode"`
	}
	if err := requestWeChatBindingJSON(ctx, "/sns/userinfo", url.Values{
		"access_token": {token.AccessToken}, "openid": {token.OpenID}, "lang": {"zh_CN"},
	}, &profile); err != nil || profile.ErrorCode != 0 || profile.OpenID != token.OpenID || len(profile.Nickname) > 256 {
		return WeChatNotice("微信身份读取失败，请重新扫码")
	}
	binding.Settings = WeChatUserSettings{AppID: configuration.AppID, OpenID: token.OpenID, DisplayName: profile.Nickname, Enabled: true}
	binding.Status.Phase = "claimed"
	binding.Status.DisplayName = profile.Nickname
	if binding.Status.DisplayName == "" {
		binding.Status.DisplayName = "微信用户 · " + token.OpenID[len(token.OpenID)-6:]
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	if current, exists := s.wechatBindings[ticket]; !exists || current.Status.Phase != "processing" || !current.Status.ExpiresAt.After(s.currentTime()) {
		return ErrWeChatBinding
	}
	s.wechatBindings[ticket] = binding
	accepted = true
	return nil
}

func (s *ConfigService) ConfirmWeChatBinding(actor NotificationBindingActor, ticket string) (WeChatUserSettingsStatus, error) {
	if err := s.validateBindingActor(actor); err != nil {
		return WeChatUserSettingsStatus{}, err
	}
	configuration, err := s.weChatConfiguration()
	if err != nil {
		return WeChatUserSettingsStatus{}, err
	}
	s.bindingMu.Lock()
	defer s.bindingMu.Unlock()
	binding, found := s.wechatBindings[ticket]
	if !found || binding.Actor != actor || binding.Status.Phase != "claimed" ||
		!binding.Status.ExpiresAt.After(s.currentTime()) || binding.Fingerprint != configuration.Fingerprint {
		return WeChatUserSettingsStatus{}, ErrWeChatBinding
	}
	delete(s.wechatBindings, ticket)
	return s.saveWeChatSettings(actor.UserID, binding.Settings, binding.Version)
}

func (s *ConfigService) ReserveWeChatTest(userID int) error {
	if _, err := s.activeNotificationUser(userID); err != nil {
		return err
	}
	s.weChatTestMu.Lock()
	defer s.weChatTestMu.Unlock()
	now := s.currentTime()
	if s.weChatTestAttempts == nil {
		s.weChatTestAttempts = make(map[int]time.Time)
	}
	for id, last := range s.weChatTestAttempts {
		if now.Sub(last) >= 30*time.Second {
			delete(s.weChatTestAttempts, id)
		}
	}
	if _, exists := s.weChatTestAttempts[userID]; exists || len(s.weChatTestAttempts) >= 4096 {
		return WeChatNotice("测试消息发送过于频繁，请三十秒后重试")
	}
	s.weChatTestAttempts[userID] = now
	return nil
}

func requestWeChatBindingJSON(ctx context.Context, path string, query url.Values, target any) error {
	client := credential.NewRestrictedHTTPClientForTargets(10_000, []credential.NetworkTarget{{Scheme: "https", Host: "api.weixin.qq.com", Port: 443}})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.weixin.qq.com"+path+"?"+query.Encode(), nil)
	if err != nil {
		return ErrWeChatBinding
	}
	response, err := client.Do(request)
	if err != nil {
		return ErrWeChatBinding
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ErrWeChatBinding
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 || json.Unmarshal(data, target) != nil {
		return ErrWeChatBinding
	}
	return nil
}
