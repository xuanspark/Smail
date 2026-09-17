package controllers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func TestSetupReturnsJSONForUnsupportedOperation(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/setup", strings.NewReader(`{"step":"unknown","action":"unknown"}`))
	response := httptest.NewRecorder()

	Setup(&appcontext.Context{}, response, request)

	if response.Code != 200 {
		t.Fatalf("setup status = %d, want 200", response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("setup content type = %q, want JSON", contentType)
	}
	var payload struct {
		ErrorNo  int    `json:"errorNo"`
		ErrorMsg string `json:"errorMsg"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode setup response: %v", err)
	}
	if payload.ErrorNo != 100 || payload.ErrorMsg != "不支持的初始化操作" {
		t.Fatalf("unexpected setup response: %+v", payload)
	}
}

func TestBuildSetupSSLResponseUsesConfiguredProxyPorts(t *testing.T) {
	cfg := &config.Config{HttpPort: 9001}
	response := buildSetupSSLResponse(
		config.SSLTypeUser,
		cfg,
		config.HTTPConfig{
			Port:         9001,
			HTTPSPort:    9002,
			TLSMode:      config.HTTPSTLSModeProxy,
			HTTPSEnabled: false,
		},
		"xuans.com",
		true,
	)

	if response.HTTPPort != 9001 || response.HTTPSPort != 9002 {
		t.Fatalf("unexpected configured ports: http=%d https=%d", response.HTTPPort, response.HTTPSPort)
	}
	if response.TLSMode != config.HTTPSTLSModeProxy || response.HTTPSEnabled {
		t.Fatalf("unexpected proxy settings: mode=%q enabled=%v", response.TLSMode, response.HTTPSEnabled)
	}
	if response.WebDomain != "xuans.com" || !response.Ready {
		t.Fatalf("unexpected setup response identity: domain=%q ready=%v", response.WebDomain, response.Ready)
	}
}

func TestBuildSetupSSLResponseSeparatesPublicPort(t *testing.T) {
	response := buildSetupSSLResponse("1", nil, config.HTTPConfig{
		Port: 8001, HTTPSPort: 8002, HTTPSEnabled: true, PublicHTTPSPort: 443,
	}, "mail.example.com", true)
	if response.HTTPSPort != 8002 || response.HTTPPort != 8001 || response.PublicHTTPSPort != 443 {
		t.Fatalf("初始化响应混淆公网与内部端口：%+v", response)
	}
}

func TestBuildSetupSSLResponseDefaultsHTTPSPort(t *testing.T) {
	response := buildSetupSSLResponse(
		config.SSLTypeAutoHTTP,
		&config.Config{HttpPort: 9001},
		config.HTTPConfig{Port: 9001},
		"mail.example.com",
		false,
	)
	if response.HTTPPort != 9001 || response.HTTPSPort != 443 {
		t.Fatalf("unexpected default ports: http=%d https=%d", response.HTTPPort, response.HTTPSPort)
	}
}
