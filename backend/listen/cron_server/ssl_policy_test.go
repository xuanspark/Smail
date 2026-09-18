package cron_server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

type policyHTTPTransport struct {
	calls int
}

func (transport *policyHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: request}, nil
}

func TestMaintenanceFollowsManagementSwitch(t *testing.T) {
	previousRoot, previousSnapshot := config.ROOT_PATH, config.CurrentConfig()
	previousManager, previousTransport := config.CurrentManager(), http.DefaultTransport
	config.ROOT_PATH = t.TempDir()
	config.SetRuntimeManager(nil)
	transport := &policyHTTPTransport{}
	http.DefaultTransport = transport
	t.Cleanup(func() {
		config.ROOT_PATH = previousRoot
		config.PublishConfigSnapshot(previousSnapshot)
		config.SetRuntimeManager(previousManager)
		http.DefaultTransport = previousTransport
	})
	cfg := &config.Config{
		HttpPort: 8000, SSLType: config.SSLTypeAutoHTTP, SetupState: config.SetupStateReady,
		SSL: config.SSLConfig{Management: config.SSLManagementProject},
	}
	config.PublishConfigSnapshot(cfg)
	if !waitHTTPReady() || transport.calls != 1 {
		t.Fatal("项目托管未启用 HTTP 就绪检查")
	}
	for _, management := range []string{config.SSLManagementCustom, config.SSLManagementWebServer} {
		cfg.SSL.Management = management
		config.PublishConfigSnapshot(cfg)
		if waitHTTPReady() {
			t.Fatal("外部托管仍等待 HTTP 验证")
		}
		maintainSSL()
		if transport.calls != 1 {
			t.Fatal("切换为外部托管后仍发起了网络请求")
		}
	}
	cfg.SSL.Management = config.SSLManagementProject
	config.PublishConfigSnapshot(cfg)
	if !waitHTTPReady() || transport.calls != 2 {
		t.Fatal("切回项目托管未恢复 HTTP 就绪检查")
	}
}
