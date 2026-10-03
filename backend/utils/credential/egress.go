package credential

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	PluginEgressSocket           = "/egress.socket"
	egressTokenHeader            = "X-Smail-Egress-Token"
	egressTargetHeader           = "X-Smail-Egress-Target"
	maxEgressRequestBytes  int64 = 32 << 20
	maxEgressResponseBytes int64 = 8 << 20
)

type pluginEgressTransport struct {
	transport http.RoundTripper
	token     string
}

func newPluginEgressTransport(timeout time.Duration) http.RoundTripper {
	return &pluginEgressTransport{
		token: os.Getenv("SMAIL_PLUGIN_EGRESS_TOKEN"),
		transport: &http.Transport{
			Proxy: nil,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "unix", PluginEgressSocket)
			},
			DisableKeepAlives:      true,
			ResponseHeaderTimeout:  timeout,
			MaxResponseHeaderBytes: 32 << 10,
		},
	}
}

func (t *pluginEgressTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if len(t.token) != 64 {
		closeRequestBody(request)
		return nil, errors.New("plugin egress authorization is unavailable")
	}
	if request == nil || request.URL == nil {
		closeRequestBody(request)
		return nil, errors.New("invalid plugin egress request")
	}
	// 目标仅在本地通道传递，主进程仍独立校验，不信任插件侧校验结果。
	envelope := request.Clone(request.Context())
	envelope.URL = &url.URL{Scheme: "http", Host: "plugin-egress", Path: "/relay"}
	envelope.Host = "plugin-egress"
	envelope.RequestURI = ""
	if envelope.Header == nil {
		envelope.Header = make(http.Header)
	}
	envelope.Header.Set(egressTokenHeader, t.token)
	envelope.Header.Set(egressTargetHeader, request.URL.String())
	response, err := t.transport.RoundTrip(envelope)
	if err != nil {
		return nil, errors.New("plugin egress channel is unavailable")
	}
	response.Request = request
	return response, nil
}

func (t *pluginEgressTransport) CloseIdleConnections() {
	if transport, ok := t.transport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

// NewPluginEgressHandler 只供沙箱专属本地通道使用，不注册到公网路由。
// 每次重新取权限；空权限拒绝访问，禁止继承通用客户端的空名单语义。
func NewPluginEgressHandler(token string, authorize func() []NetworkTarget) http.Handler {
	return NewPluginEgressHandlerWithTelegramProxy(token, authorize, nil)
}

// NewPluginEgressHandlerWithTelegramProxy 的代理只能由主进程读取，插件请求不能指定代理。
func NewPluginEgressHandlerWithTelegramProxy(token string, authorize func() []NetworkTarget, telegramProxy func() (string, error)) http.Handler {
	slots := make(chan struct{}, 2)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if len(token) != 64 || len(request.Header.Values(egressTokenHeader)) != 1 ||
			subtle.ConstantTimeCompare([]byte(request.Header.Get(egressTokenHeader)), []byte(token)) != 1 {
			http.Error(writer, "出站认证失败", http.StatusForbidden)
			return
		}
		if request.URL.Path != "/relay" || request.URL.RawQuery != "" || request.URL.IsAbs() ||
			request.Host != "plugin-egress" || request.Header.Get("Upgrade") != "" ||
			(request.Method != http.MethodGet && request.Method != http.MethodPost && request.Method != http.MethodHead) {
			http.Error(writer, "不支持的出站请求", http.StatusBadRequest)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(writer, "出站请求繁忙", http.StatusTooManyRequests)
			return
		}
		var targets []NetworkTarget
		if authorize != nil {
			targets = normalizeNetworkTargets(authorize())
		}
		if len(targets) == 0 || len(request.Header.Values(egressTargetHeader)) != 1 {
			http.Error(writer, "出站目标未获授权", http.StatusForbidden)
			return
		}
		rawTarget := request.Header.Get(egressTargetHeader)
		if _, err := NormalizeHTTPSURL(rawTarget); err != nil {
			http.Error(writer, "出站目标无效", http.StatusForbidden)
			return
		}
		endpoint, err := url.Parse(rawTarget)
		if err != nil || !matchesNetworkTarget(endpoint, targets) {
			http.Error(writer, "出站目标未获授权", http.StatusForbidden)
			return
		}
		if request.ContentLength > maxEgressRequestBytes {
			http.Error(writer, "出站请求过大", http.StatusRequestEntityTooLarge)
			return
		}
		// 先读取有界请求体，防止未知长度请求在越限前已部分提交给上游。
		body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxEgressRequestBytes))
		if err != nil {
			http.Error(writer, "出站请求体无效或过大", http.StatusRequestEntityTooLarge)
			return
		}
		// 请求体读取期间权限可能被撤销，连接上游前再次按当前配置核对。
		targets = normalizeNetworkTargets(authorize())
		if len(targets) == 0 || !matchesNetworkTarget(endpoint, targets) {
			http.Error(writer, "出站目标未获授权", http.StatusForbidden)
			return
		}
		ctx, cancel := context.WithTimeout(request.Context(), 30*time.Second)
		defer cancel()
		outbound, err := http.NewRequestWithContext(ctx, request.Method, endpoint.String(), bytes.NewReader(body))
		if err != nil {
			http.Error(writer, "出站请求无效", http.StatusBadRequest)
			return
		}
		outbound.Header = request.Header.Clone()
		stripEgressHeaders(outbound.Header)
		client := newRestrictedHTTPClientForTargets(30_000, targets, false)
		if telegramProxy != nil {
			proxyURL, proxyErr := telegramProxy()
			if proxyErr != nil {
				http.Error(writer, "Telegram 代理配置无效", http.StatusForbidden)
				return
			}
			client, err = newTelegramHTTPClient(30_000, proxyURL, false)
			if err != nil {
				http.Error(writer, "Telegram 代理配置无效", http.StatusForbidden)
				return
			}
		}
		// 每次请求使用独立连接，避免配置撤销后继续复用已有上游连接。
		if restricted, ok := client.Transport.(restrictedRoundTripper); ok {
			if transport, ok := restricted.transport.(*http.Transport); ok {
				transport.DisableKeepAlives = true
				transport.MaxResponseHeaderBytes = 32 << 10
			}
		}
		defer client.CloseIdleConnections()
		// 重定向交还调用方，每一跳重新通过身份、权限与公网地址检查。
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := client.Do(outbound)
		if err != nil {
			// 原始错误可能包含上游 URL 中的令牌，绝不回传或记录。
			http.Error(writer, "获准目标连接失败", http.StatusBadGateway)
			return
		}
		defer response.Body.Close()
		payload, err := io.ReadAll(io.LimitReader(response.Body, maxEgressResponseBytes+1))
		if err != nil || int64(len(payload)) > maxEgressResponseBytes {
			http.Error(writer, "上游响应无效或过大", http.StatusBadGateway)
			return
		}
		stripEgressHeaders(response.Header)
		response.Header.Del("Content-Length")
		for name, values := range response.Header {
			for _, value := range values {
				writer.Header().Add(name, value)
			}
		}
		writer.WriteHeader(response.StatusCode)
		_, _ = writer.Write(payload)
	})
}

func stripEgressHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"TE", "Trailer", "Transfer-Encoding", "Upgrade", egressTokenHeader, egressTargetHeader,
	} {
		header.Del(name)
	}
}
