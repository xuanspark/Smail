package wechatcallback

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"html/template"
	"net/http"
)

// 页面随主程序发布，无需外部字体、脚本或手机浏览器登录状态。
//
//go:embed page.html
var pageHTML string

//go:embed page.css
var pageCSS string

var resultTemplate = template.Must(template.New("wechat-result").Parse(pageHTML))

// Write 只呈现授权结果，不接收身份凭据，也不执行账号绑定。
func Write(w http.ResponseWriter, authorized bool) {
	digest := sha256.Sum256([]byte(pageCSS))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'sha256-"+base64.StdEncoding.EncodeToString(digest[:])+"'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	var body bytes.Buffer
	data := struct {
		Authorized bool
		CSS        template.CSS
	}{authorized, template.CSS(pageCSS)} // 仅信任编译时内嵌样式，禁止传入请求内容。
	if err := resultTemplate.Execute(&body, data); err != nil {
		http.Error(w, "页面暂时无法显示，请返回 Smail 查看绑定状态。", http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	if !authorized {
		status = http.StatusBadRequest
	}
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}
