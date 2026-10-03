package wechatcallback

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestWriteAuthorizationResult(t *testing.T) {
	for _, test := range []struct {
		name       string
		authorized bool
		status     int
		message    string
	}{
		{"authorized", true, http.StatusOK, "等待确认绑定"},
		{"expired", false, http.StatusBadRequest, "请重新扫码"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			Write(response, test.authorized)
			if response.Code != test.status {
				t.Fatalf("授权结果状态码错误：%d", response.Code)
			}
			body := response.Body.String()
			for _, expected := range []string{test.message, `lang="zh-CN"`, `width=device-width, initial-scale=1`, "回到发起扫码的 Smail 页面", "<style>" + pageCSS + "</style>"} {
				if !strings.Contains(body, expected) {
					t.Fatalf("结果页缺少预期内容：%s", expected)
				}
			}
			for _, forbidden := range []string{"<script", "<iframe", "<form", "http://", "https://", "#ZgotmplZ", "&#x", "绑定成功"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("结果页包含不应出现的内容：%s", forbidden)
				}
			}
			if test.authorized && !strings.Contains(body, "本次授权不会自动绑定账号") {
				t.Fatal("授权成功页面必须明确仍需原会话确认")
			}
			if strings.Count(body, `<li>`) != 2 {
				t.Fatal("结果页应仅展示两步操作指引")
			}
			for _, expected := range []string{`aria-labelledby="next-step-title"`, `id="next-step-title"`, "无需在微信中登录 Smail"} {
				if !strings.Contains(body, expected) {
					t.Fatalf("结果页缺少操作引导：%s", expected)
				}
			}
			if test.authorized && (!strings.Contains(body, "核对微信昵称") || !strings.Contains(body, "「确认绑定」")) {
				t.Fatal("授权成功页面必须引导核对昵称并手动确认")
			}
			if !test.authorized && !strings.Contains(body, "以原页面状态为准") {
				t.Fatal("失效页面不能将重复回调误报为已经解除绑定")
			}
		})
	}
}

func TestStyleHashAfterHTMLParsing(t *testing.T) {
	for name, lineEnding := range map[string]string{"LF": "\n", "CRLF": "\r\n", "CR": "\r"} {
		t.Run(name, func(t *testing.T) {
			original := pageCSS
			defer func() { pageCSS = original }()
			pageCSS = normalizeStyle(strings.ReplaceAll(original, "\n", lineEnding))
			for _, authorized := range []bool{true, false} {
				response := httptest.NewRecorder()
				Write(response, authorized)
				tokenizer := html.NewTokenizer(strings.NewReader(response.Body.String()))
				var styles []string
				for token := tokenizer.Next(); token != html.ErrorToken; token = tokenizer.Next() {
					if token == html.StartTagToken && tokenizer.Token().Data == "style" {
						if tokenizer.Next() != html.TextToken {
							t.Fatal("内嵌样式必须包含有效文本")
						}
						styles = append(styles, tokenizer.Token().Data)
					}
				}
				if len(styles) != 1 || styles[0] != original {
					t.Fatal("浏览器解析后的样式必须与规范化样式一致")
				}
				digest := sha256.Sum256([]byte(styles[0]))
				expected := "style-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"
				if !strings.Contains(response.Header().Get("Content-Security-Policy"), expected) {
					t.Fatal("安全策略必须允许浏览器实际解析出的样式摘要")
				}
			}
		})
	}
}

func TestWriteResultSecurityHeaders(t *testing.T) {
	for _, authorized := range []bool{true, false} {
		response := httptest.NewRecorder()
		Write(response, authorized)
		for name, expected := range map[string]string{
			"Content-Type":           "text/html; charset=utf-8",
			"Cache-Control":          "no-store",
			"Referrer-Policy":        "no-referrer",
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
		} {
			if response.Header().Get(name) != expected {
				t.Fatalf("结果页安全响应头不符：%s", name)
			}
		}
		digest := sha256.Sum256([]byte(pageCSS))
		policy := response.Header().Get("Content-Security-Policy")
		for _, expected := range []string{"default-src 'none'", "base-uri 'none'", "form-action 'none'", "frame-ancestors 'none'", "style-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'"} {
			if !strings.Contains(policy, expected) {
				t.Fatalf("结果页内容安全策略缺少约束：%s", expected)
			}
		}
		if strings.Contains(policy, "unsafe-inline") {
			t.Fatal("仅允许内嵌样式的固定摘要，不应放开任意内联内容")
		}
	}
}
