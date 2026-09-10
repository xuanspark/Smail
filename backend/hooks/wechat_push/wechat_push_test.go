package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
)

type weChatRoundTripFunc func(*http.Request) (*http.Response, error)

func (f weChatRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func validWeChatRuntimePayload() []byte {
	payload := weChatRuntimeConfig{
		PluginID:     "wechat_push",
		Generation:   1,
		Settings:     json.RawMessage(`{"appId":"app-id","templateId":"template-id","userId":"user-id","webDomain":"mail.example.com"}`),
		Secrets:      map[string]string{"appSecret": "app-secret"},
		Capabilities: []string{"network"},
		Network:      []framework.PluginNetworkTarget{{Scheme: "https", Host: "api.weixin.qq.com", Port: 443}},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}

func TestWeChatRuntimeConfigAcceptsAuthorizedPayload(t *testing.T) {
	hook := NewWechatPushHook()
	if err := hook.ValidateConfig(validWeChatRuntimePayload()); err != nil {
		t.Fatal(err)
	}
	if err := hook.Configure(validWeChatRuntimePayload()); err != nil {
		t.Fatal(err)
	}
	if err := hook.Health(); err != nil {
		t.Fatal(err)
	}
	if hook.appId != "app-id" || hook.secret != "app-secret" || hook.webDomain != "mail.example.com" {
		t.Fatalf("unexpected runtime configuration: %+v", hook)
	}
}

func TestWeChatRuntimeConfigRejectsZeroGeneration(t *testing.T) {
	var payload weChatRuntimeConfig
	if err := json.Unmarshal(validWeChatRuntimePayload(), &payload); err != nil {
		t.Fatal(err)
	}
	payload.Generation = 0
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewWechatPushHook().ValidateConfig(data); err == nil {
		t.Fatal("zero plugin configuration generation was accepted")
	}
}

func TestWeChatRuntimeConfigRejectsSecurityBoundaryViolations(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*weChatRuntimeConfig)
	}{
		{name: "plugin id", mutate: func(payload *weChatRuntimeConfig) { payload.PluginID = "other" }},
		{name: "missing secret", mutate: func(payload *weChatRuntimeConfig) { payload.Secrets = nil }},
		{name: "missing capability", mutate: func(payload *weChatRuntimeConfig) { payload.Capabilities = nil }},
		{name: "extra capability", mutate: func(payload *weChatRuntimeConfig) { payload.Capabilities = []string{"network", "filesystem"} }},
		{name: "wrong network", mutate: func(payload *weChatRuntimeConfig) { payload.Network = nil }},
		{name: "extra network", mutate: func(payload *weChatRuntimeConfig) {
			payload.Network = append(payload.Network, framework.PluginNetworkTarget{Scheme: "https", Host: "example.com", Port: 443})
		}},
		{name: "unknown secret", mutate: func(payload *weChatRuntimeConfig) { payload.Secrets["other"] = "value" }},
		{name: "invalid domain", mutate: func(payload *weChatRuntimeConfig) {
			payload.Settings = json.RawMessage(`{"appId":"app-id","templateId":"template-id","userId":"user-id","webDomain":"https://example.com/path"}`)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var payload weChatRuntimeConfig
			if err := json.Unmarshal(validWeChatRuntimePayload(), &payload); err != nil {
				t.Fatal(err)
			}
			test.mutate(&payload)
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if err := NewWechatPushHook().ValidateConfig(data); err == nil {
				t.Fatal("invalid runtime configuration was accepted")
			} else if strings.Contains(err.Error(), "app-secret") {
				t.Fatal("secret material appeared in the error")
			}
		})
	}
}

func TestWeChatRuntimeConfigRejectsTrailingJSON(t *testing.T) {
	data := append(validWeChatRuntimePayload(), []byte(` {}`)...)
	if err := NewWechatPushHook().ValidateConfig(data); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestBuildWeChatMessageLimitsContent(t *testing.T) {
	message := buildWeChatMessage(&parsemail.Email{
		Subject: strings.Repeat("界", maxWeChatMessageBytes),
		Text:    []byte(strings.Repeat("body", maxWeChatMessageBytes)),
	})
	if len(message) > maxWeChatMessageBytes {
		t.Fatalf("message length = %d, want at most %d", len(message), maxWeChatMessageBytes)
	}
	if !utf8.ValidString(message) {
		t.Fatal("message is not valid UTF-8")
	}
	if message := buildWeChatMessage(nil); message != "" {
		t.Fatalf("nil email message = %q", message)
	}
}

func TestSendUserMsgLimitsOutboundContent(t *testing.T) {
	originalClient := weChatHTTPClient
	t.Cleanup(func() { weChatHTTPClient = originalClient })

	var captured sendMsgRequest
	weChatHTTPClient = &http.Client{Transport: weChatRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})}

	hook := &WeChatPushHook{
		token:        "test-token",
		tokenExpires: time.Now().Add(time.Minute).Unix(),
		templateId:   "template",
		webDomain:    "mail.example.com",
	}
	hook.sendUserMsg(nil, "user", strings.Repeat("x", maxWeChatMessageBytes+100))
	if len(captured.Data.Content.Value) != maxWeChatMessageBytes {
		t.Fatalf("outbound content length = %d, want %d", len(captured.Data.Content.Value), maxWeChatMessageBytes)
	}
}

func TestGetWxAccessTokenConcurrentRefresh(t *testing.T) {
	originalClient := weChatHTTPClient
	t.Cleanup(func() { weChatHTTPClient = originalClient })

	var requestCount atomic.Int32
	weChatHTTPClient = &http.Client{Transport: weChatRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"shared-token","expires_in":7200}`)),
		}, nil
	})}

	hook := &WeChatPushHook{appId: "test-app-id", secret: "test-secret"}
	const workerCount = 32
	start := make(chan struct{})
	results := make(chan string, workerCount)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			<-start
			results <- hook.getWxAccessToken()
		}()
	}

	close(start)
	workers.Wait()
	close(results)

	for token := range results {
		if token != "shared-token" {
			t.Fatalf("token = %q, want shared-token", token)
		}
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("token request count = %d, want 1", got)
	}
}
