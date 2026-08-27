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

	cfg := config.CurrentConfig()
	if cfg != nil && (cfg.SSLType == config.SSLTypeAutoHTTP || cfg.SSLType == config.SSLTypeAutoDNS) {
		go sslUpdateLoop()
	} else {
		go sslCheck()
	}

}

// 每天检查一遍SSL证书是否更新，更新就重启
func sslCheck() {
	var err error
	_, expiredTime, _, err = ssl.CheckSSLCrtInfo()
	if err != nil {
		log.Errorf("SSL Check Error! %+v", err)
		expiredTime = time.Time{}
	}

	for {
		time.Sleep(24 * time.Hour)
		_, newExpTime, _, err := ssl.CheckSSLCrtInfo()
		if err != nil {
			log.Errorf("SSL Check Error! %+v", err)
			continue
		}
		if expiredTime.IsZero() {
			expiredTime = newExpTime
			continue
		}
		if newExpTime != expiredTime {
			expiredTime = newExpTime
			log.Infoln("SSL certificate had update! restarting")
			signal.RestartChan <- true
		}

	}
}

// 每天检查一遍SSL证书是否即将过期，即将过期就重新生成
func sslUpdateLoop() {
	for {
		// 等待 HTTP 就绪后再执行 ACME 续期，避免挑战失败
		waitHTTPReady()
		ssl.Update(true)
		// 每24小时检测一次证书有效期
		time.Sleep(24 * time.Hour)
	}
}

// 新增：HTTP 就绪探测（最多等待 ~90 秒）
func waitHTTPReady() {
	port := 80
	if cfg := config.CurrentConfig(); cfg != nil && cfg.HttpPort > 0 {
		port = cfg.HttpPort
	}
	if port < 1 || port > 65535 {
		log.Warnf("invalid HTTP port %d, skipping SSL update this round", port)
		return
	}

	target := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		Path:   "/api/ping",
	}
	client := &http.Client{Timeout: 2 * time.Second}

	for i := 0; i < 90; i++ {
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
			return
		}
		time.Sleep(1 * time.Second)
	}
	log.Warnf("HTTP not ready after 90s, skipping SSL update this round")
}
