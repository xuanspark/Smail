package http_server

import (
	"bytes"
	"fmt"
	"html"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/session"
)

const publicRecoveryPrefix = "/rest"

// newPublicRecoveryHandler 创建不经过主站会话和业务上下文的匿名恢复处理器。
// 该处理器只能由 publicRecoveryOrSessionHandler 在会话中间件之前调用。
func newPublicRecoveryHandler() (http.Handler, error) {
	frontend, err := fs.Sub(local, "dist")
	if err != nil {
		return nil, fmt.Errorf("load embedded frontend for public recovery: %w", err)
	}
	return &publicRecoveryHandler{
		frontend: frontend,
		static:   http.FileServer(http.FS(frontend)),
	}, nil
}

type publicRecoveryHandler struct {
	frontend fs.FS
	static   http.Handler
}

func (h *publicRecoveryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil || !controllers.PublicMailRecoveryEnabled() {
		http.NotFound(w, r)
		return
	}
	if !config.DevelopmentMode && requestScheme(r) != "https" {
		http.NotFound(w, r)
		return
	}

	path := normalizedPublicRecoveryPath(r.URL.Path)
	if path == "/public-password-recovery" {
		h.redirectFromWebLogin(w, r)
		return
	}

	if !isPublicRecoveryWebHost(r) {
		http.NotFound(w, r)
		return
	}

	switch path {
	case "/rest", "/rest/":
		h.servePage(w, r)
	case "/rest/api/recovery/begin":
		h.serveAPI(w, r, controllers.PublicPasswordRecoveryFlow)
	case "/rest/api/recovery/send-code", "/rest/api/recovery/verify", "/rest/api/recovery/reset":
		h.serveAPI(w, r, controllers.PublicPasswordRecoveryFlow)
	case "/rest/api/recovery/requirements":
		h.serveAPI(w, r, controllers.PublicLinkPasswordRecoveryRequirements)
	case "/rest/api/recovery/complete":
		http.NotFound(w, r)
	default:
		if isPublicRecoveryAssetPath(path) {
			h.serveStatic(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (h *publicRecoveryHandler) redirectFromWebLogin(w http.ResponseWriter, r *http.Request) {
	if !isCurrentWebHost(r) {
		http.NotFound(w, r)
		return
	}
	target, ok := publicRecoveryBaseURL()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, target, http.StatusFound)
}

func (h *publicRecoveryHandler) servePage(w http.ResponseWriter, r *http.Request) {
	loginURL, ok := publicRecoveryLoginURL()
	if !ok {
		http.NotFound(w, r)
		return
	}
	content, err := fs.ReadFile(h.frontend, "index.html")
	if err != nil {
		http.Error(w, "public recovery page is unavailable", http.StatusServiceUnavailable)
		return
	}
	marker := []byte(`<div id="app"></div>`)
	replacement := []byte(`<div id="app" data-login-url="` + html.EscapeString(loginURL) + `"></div>`)
	if !bytes.Contains(content, marker) {
		http.Error(w, "public recovery page is unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(bytes.Replace(content, marker, replacement, 1))
}

func (h *publicRecoveryHandler) serveStatic(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=86400")
	h.static.ServeHTTP(w, r)
}

func (h *publicRecoveryHandler) serveAPI(w http.ResponseWriter, r *http.Request, handler func(http.ResponseWriter, *http.Request)) {
	if !publicRecoveryAPIRequestAllowed(r) {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "invalid public recovery request", http.StatusBadRequest)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	handler(w, r)
}

// publicRecoveryOrSessionHandler 将匿名恢复路径从主站 Session 之前分流。
// 除匿名恢复页、静态资源和恢复 API 外，所有请求维持原有主站会话处理路径。
func publicRecoveryOrSessionHandler(publicRecovery, mainSite http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := normalizedPublicRecoveryPath(r.URL.Path)
		if path == "/public-password-recovery" || isPublicRecoveryReservedPath(path) ||
			(controllers.PublicMailRecoveryEnabled() && isPublicRecoveryWebHost(r) && isPublicRecoveryAssetPath(path)) {
			publicRecovery.ServeHTTP(w, r)
			return
		}
		mainSite.ServeHTTP(w, r)
	})
}

func normalizedPublicRecoveryPath(value string) string {
	path := strings.TrimSpace(value)
	for range 2 {
		decoded, err := url.PathUnescape(path)
		if err != nil || decoded == path {
			break
		}
		path = decoded
	}
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func isPublicRecoveryReservedPath(path string) bool {
	return path == publicRecoveryPrefix || strings.HasPrefix(path, publicRecoveryPrefix+"/")
}

func isPublicRecoveryAssetPath(path string) bool {
	return strings.HasPrefix(path, "/assets/") || path == "/smail-logo.png"
}

func publicRecoveryAPIRequestAllowed(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost || hasPublicRecoveryCookie(r) {
		return false
	}
	if strings.TrimSpace(r.Header.Get("X-CSRF-Token")) != "" ||
		strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return false
	}
	return publicRecoveryOriginsAllowed(r)
}

func hasPublicRecoveryCookie(r *http.Request) bool {
	if r == nil {
		return false
	}
	for _, cookie := range r.Cookies() {
		name := strings.ToLower(strings.TrimSpace(cookie.Name))
		if name == "smail_csrf" || name == "smail_recovery_context" || name == "smail_session" {
			return true
		}
		if session.Instance != nil && strings.EqualFold(cookie.Name, session.Instance.Cookie.Name) {
			return true
		}
	}
	return false
}

func publicRecoveryOriginsAllowed(r *http.Request) bool {
	for _, value := range []string{r.Header.Get("Origin"), r.Header.Get("Referer")} {
		value = strings.TrimSpace(value)
		if value != "" && !isPublicRecoveryOrigin(value) {
			return false
		}
	}
	return true
}

func isPublicRecoveryOrigin(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u.User != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return false
	}
	expectedHost, expectedPort, ok := configuredHTTPSAuthority(config.CurrentWebDomain())
	if !ok {
		return false
	}
	originHost, originPort, ok := normalizeAuthority(u.Host, "443")
	return ok && originHost == expectedHost && originPort == expectedPort
}

func isPublicRecoveryWebHost(r *http.Request) bool {
	return requestMatchesHTTPSAuthority(r, config.CurrentWebDomain())
}

func isCurrentWebHost(r *http.Request) bool {
	return requestMatchesHTTPSAuthority(r, config.CurrentWebDomain())
}

func requestMatchesHTTPSAuthority(r *http.Request, configured string) bool {
	if r == nil {
		return false
	}
	expectedHost, expectedPort, ok := configuredHTTPSAuthority(configured)
	if !ok {
		return false
	}
	requestHost, requestPort, ok := normalizeAuthority(r.Host, "443")
	return ok && requestHost == expectedHost && requestPort == expectedPort
}

func configuredHTTPSAuthority(value string) (string, string, bool) {
	port := currentPublicHTTPSPort()
	if port < 1 || port > 65535 {
		return "", "", false
	}
	defaultPort := strconv.Itoa(port)
	return normalizeAuthority(value, defaultPort)
}

func currentPublicHTTPSPort() int {
	return config.ResolvePublicHTTPSPort(config.CurrentHTTPConfig())
}

func publicRecoveryBaseURL() (string, bool) {
	return configuredHTTPSURL(config.CurrentWebDomain(), "/rest/", "")
}

func publicRecoveryLoginURL() (string, bool) {
	return configuredHTTPSURL(config.CurrentWebDomain(), "/", "/login")
}

func configuredHTTPSURL(value, path, fragment string) (string, bool) {
	publicPort := currentPublicHTTPSPort()
	if publicPort < 1 || publicPort > 65535 {
		return "", false
	}
	parsed, err := url.Parse("//" + strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" {
		return "", false
	}
	port := parsed.Port()
	if port == "" && publicPort != 443 {
		port = strconv.Itoa(publicPort)
	}
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", false
		}
		host = net.JoinHostPort(host, strconv.Itoa(portNumber))
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return (&url.URL{Scheme: "https", Host: host, Path: path, Fragment: fragment}).String(), true
}
