package cron_server

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/signal"
)

type auditHTTPTransport struct {
	calls int
}

func (transport *auditHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: request}, nil
}

// 代理检查和就绪探测只使用隔离配置，传输层替身阻止访问真实服务。
func TestAuditProxyMaintenanceSkipsNetwork(t *testing.T) {
	previousRoot, previousSnapshot := config.ROOT_PATH, config.CurrentConfig()
	previousTransport, previousExpiry, previousRestart := http.DefaultTransport, expiredTime, signal.RestartChan
	config.ROOT_PATH = t.TempDir()
	transport := &auditHTTPTransport{}
	http.DefaultTransport = transport
	signal.RestartChan = make(chan bool, 1)
	expiredTime = time.Time{}
	t.Cleanup(func() {
		config.ROOT_PATH = previousRoot
		config.PublishConfigSnapshot(previousSnapshot)
		http.DefaultTransport, expiredTime, signal.RestartChan = previousTransport, previousExpiry, previousRestart
	})
	for _, sslType := range []string{config.SSLTypeAutoHTTP, config.SSLTypeAutoDNS, config.SSLTypeUser} {
		config.PublishConfigSnapshot(&config.Config{TLSMode: config.HTTPSTLSModeProxy, SSLType: sslType, SetupState: config.SetupStateReady})
		if waitHTTPReady() {
			t.Fatal("反代模式不应探测 HTTP 挑战服务")
		}
		maintainSSL()
	}
	if transport.calls != 0 || len(signal.RestartChan) != 0 {
		t.Fatal("反代模式触发了网络请求或无证书重启")
	}
	// 切回直接 HTTPS 后，每轮读取最新策略，而非沿用启动时的分支。
	config.PublishConfigSnapshot(&config.Config{TLSMode: config.HTTPSTLSModeDirect, SSLType: config.SSLTypeAutoHTTP, SetupState: config.SetupStateReady})
	if !waitHTTPReady() || transport.calls != 1 {
		t.Fatal("直接 HTTPS 的 HTTP 就绪探测未恢复")
	}
}
