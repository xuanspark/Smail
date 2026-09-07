package cron_server

import (
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/setup/ssl"
	"git.pcux.cn/xuans/Smail/signal"
	log "github.com/sirupsen/logrus"
	"time"
	// 新增：HTTP 就绪探测
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
)

var expiredTime time.Time

func Start() {

	// 第一次启动，等待到初始化完成
	if cfg := config.CurrentConfig(); cfg == nil || !cfg.IsSetupReady() {
		for {
			time.Sleep(1 * time.Minute)
			if cfg := config.CurrentConfig(); cfg != nil && cfg.IsSetupReady() {
				break
			}
		}
	}

	go sslUpdateLoop()

}

// 只检查外部维护的本地邮件证书是否更新，不触发 ACME。
func sslCheck() {
	_, newExpTime, _, err := ssl.CheckSSLCrtInfo()
	if err != nil {
		log.Errorf("SSL Check Error! %+v", err)
		return
	}
	if expiredTime.IsZero() {
		expiredTime = newExpTime
		return
	}
	if newExpTime != expiredTime {
		expiredTime = newExpTime
		log.Infoln("SSL certificate had update! restarting")
		signal.RestartChan <- true
	}
}

// 每轮读取最新策略，切换反代模式后不再等待 HTTP 挑战或执行自动续期。
func maintainSSL() {
	cfg := config.CurrentConfig()
	if cfg == nil || !cfg.IsSetupReady() {
		return
	}
	if !cfg.AutomaticSSLEnabled() {
		sslCheck()
		return
	}
	expiredTime = time.Time{}
	if cfg.SSLType == config.SSLTypeAutoHTTP && !waitHTTPReady() {
		return
	}
	ssl.Update(true)
}

// 每天检查证书，外部管理模式仅保留本地证书变更检查。
func sslUpdateLoop() {
	for {
		maintainSSL()
		// 每24小时检测一次证书有效期
		time.Sleep(24 * time.Hour)
	}
}

// 新增：HTTP 就绪探测（最多等待 ~90 秒）
func waitHTTPReady() bool {
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
		if !config.CurrentConfig().AutomaticSSLEnabled() {
			return false
		}
		requestContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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
		time.Sleep(1 * time.Second)
	}
	log.Warnf("HTTP not ready after 90s, skipping SSL update this round")
	return false
}
