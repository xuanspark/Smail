package cron_server

import (
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/setup/ssl"
	log "github.com/sirupsen/logrus"
	"time"
	// 新增：HTTP 就绪探测
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
)

// Runtime 将证书维护绑定到核心服务，避免恢复配置时并发争用状态锁。
type Runtime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// Start 只能在配置恢复及核心服务初始化完成后调用。
func Start() *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(runtime.done)
		sslUpdateLoop(ctx)
	}()
	return runtime
}

// Stop 取消后续维护并等待当前操作结束；超时不能继续关闭配置及数据库。
func (runtime *Runtime) Stop(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	runtime.cancel()
	select {
	case <-runtime.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// 只检查外部维护的本地邮件证书是否更新，不触发 ACME。
func sslCheck() {
	// 校验失败时保留尚未过期的内存证书；不发送服务重启信号。
	_ = config.ReloadRuntimeTLSCertificate()
}

// 每轮读取最新策略，切换反代模式后不再等待 HTTP 挑战或执行自动续期。
func maintainSSL() {
	maintainSSLContext(context.Background())
}

func maintainSSLContext(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	config.MaintainManagedSSLHistory()
	cfg := config.CurrentCertificateConfig()
	if cfg == nil || !cfg.IsSetupReady() {
		return
	}
	if !cfg.AutomaticSSLEnabled() {
		sslCheck()
		return
	}
	if cfg.EffectiveSSLType() == config.SSLTypeAutoHTTP && !waitHTTPReadyContext(ctx) {
		return
	}
	if ctx.Err() != nil {
		return
	}
	ssl.Update(true)
}

// 每天检查证书，外部管理模式仅保留本地证书变更检查。
func sslUpdateLoop(ctx context.Context) {
	lastRenewal := time.Time{}
	for ctx.Err() == nil {
		if time.Since(lastRenewal) >= 24*time.Hour {
			maintainSSLContext(ctx)
			lastRenewal = time.Now()
		} else {
			config.MaintainManagedSSLHistory()
			sslCheck()
		}
		// 每分钟清理已到期的上一套证书；自动签发仍最多每天检查一次。
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// 新增：HTTP 就绪探测（最多等待 ~90 秒）
func waitHTTPReady() bool {
	return waitHTTPReadyContext(context.Background())
}

func waitHTTPReadyContext(ctx context.Context) bool {
	port := config.CurrentHTTPConfig().Port
	if port < 1 || port > 65535 {
		log.Warnf("invalid HTTP port %d, skipping SSL update this round", port)
		return false
	}

	target := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		Path:   "/api/ping",
	}
	client := &http.Client{Timeout: 2 * time.Second}

	for i := 0; i < 90; i++ {
		if ctx.Err() != nil {
			return false
		}
		if !config.CurrentCertificateConfig().AutomaticSSLEnabled() {
			return false
		}
		requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		request := (&http.Request{
			Method: http.MethodGet,
			URL:    target,
			Header: make(http.Header),
		}).WithContext(requestContext)
		resp, err := client.Do(request)
		statusOK := err == nil && resp != nil && resp.StatusCode == http.StatusOK
		if resp != nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Debugf("failed to close HTTP readiness response: %v", closeErr)
			}
		}
		cancel()
		if statusOK {
			log.Infof("HTTP ready: %s", target.String())
			return true
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	log.Warnf("HTTP not ready after 90s, skipping SSL update this round")
	return false
}
