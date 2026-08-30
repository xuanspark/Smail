package http_server

import (
	"embed"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	"git.pcux.cn/xuans/Smail/utils/id"
	log "github.com/sirupsen/logrus"
	olog "log"
)

//go:embed dist/*
var local embed.FS

var httpsServer *http.Server

type nullWrite struct{}

func (w *nullWrite) Write(p []byte) (int, error) {
	return len(p), nil
}

// HttpsStart 启动仅承载业务的 HTTPS 服务。
func HttpsStart() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	mux := http.NewServeMux()
	if err := router(mux); err != nil {
		return err
	}

	nullLog := olog.New(&nullWrite{}, "", olog.Ldate)
	httpsPort := 443
	if cfg.HttpsPort > 0 {
		httpsPort = cfg.HttpsPort
	}

	if config.DevelopmentMode && cfg.HttpsEnabled == 2 {
		return nil
	}
	certificatePath, privateKeyPath, err := config.ResolveRuntimeSSLPaths(cfg)
	if err != nil {
		return fmt.Errorf("resolve HTTPS certificate: %w", err)
	}

	httpsServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", httpsPort),
		Handler:           securityHeaders(session.Instance.LoadAndSave(mux)),
		ReadTimeout:       time.Second * 90,
		ReadHeaderTimeout: time.Second * 10,
		WriteTimeout:      time.Second * 90,
		IdleTimeout:       time.Second * 30,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          nullLog,
	}
	log.Infof("HTTPS server started on port %d", httpsPort)
	if err := httpsServer.ListenAndServeTLS(certificatePath, privateKeyPath); err != nil {
		if errors.Is(err, http.ErrServerClosed) {
			log.Infof("HTTPS server closed normally on port %d", httpsPort)
			return nil
		}
		return fmt.Errorf("serve HTTPS on port %d: %w", httpsPort, err)
	}
	return nil
}

func HttpsStop() {
	if httpsServer != nil {
		if err := httpsServer.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Warn("close HTTPS server failed")
		}
		httpsServer = nil
	}
}

// contextIterceptor 注入请求上下文并统一执行会话校验。
func contextIterceptor(h controllers.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setLegacyAPIHeaders(w, r)
			if w.Header().Get("Content-Type") == "" {
				w.Header().Set("Content-Type", "application/json")
			}
			w.Header().Set("Cache-Control", "no-store")
			r.Body = http.MaxBytesReader(w, r.Body, 64<<20)

			if normalizedAPIPath(r.URL.Path) == "/api/login" && !loginRequestAllowed(r) {
				response.NewErrorResponse(response.ParamsError, "\u8bf7\u6c42\u6765\u6e90\u6821\u9a8c\u5931\u8d25", "").FPrint(w)
				return
			}

			if stateChangingMethod(r.Method) && hasSessionCookie(r) && (!sameOriginRequest(r) || !csrf.Valid(r)) {
				response.NewErrorResponse(response.ParamsError, "请求来源校验失败", "").FPrint(w)
				return
			}

			ctx := &context.Context{}
			ctx.Context = r.Context()
			ctx.SetValue(context.LogID, id.GenLogID())
			lang := r.Header.Get("Lang")
			if lang == "" {
				lang = "en"
			}
			ctx.Lang = lang

			if config.IsSetupReady() {
				identity, identityErr := session.GetUser(ctx)
				if identityErr != nil {
					csrf.Clear(w, r, !config.DevelopmentMode)
					if err := session.Instance.Destroy(ctx); err != nil {
						log.WithContext(ctx).WithError(err).Warn("destroy invalid session failed")
					}
				}
				if identityErr == nil && identity.ID > 0 {
					var currentUser models.User
					found, err := db.Instance.ID(identity.ID).Get(&currentUser)
					if err != nil {
						log.WithContext(ctx).WithError(err).Error("validate session user failed")
						response.NewErrorResponse(response.ServerError, "\u4f1a\u8bdd\u6821\u9a8c\u5931\u8d25", "").FPrint(w)
						return
					}
					if !found || currentUser.Disabled != 0 || currentUser.Account != identity.Account || currentUser.SessionVersion != identity.SessionVersion || (identity.SecurityVersion > 0 && currentUser.SecurityVersion != identity.SecurityVersion) {
						csrf.Clear(w, r, !config.DevelopmentMode)
						if err := session.Instance.Destroy(ctx); err != nil {
							log.WithContext(ctx).WithError(err).Warn("destroy revoked session failed")
						}
					} else {
						ctx.UserID = currentUser.ID
						ctx.UserName = currentUser.Name
						ctx.UserAccount = currentUser.Account
						ctx.Role = currentUser.EffectiveRole()
						ctx.IsAdmin = ctx.Role == access.RoleSuperAdmin || ctx.Role == access.RoleAdmin
						ctx.MustChangePassword = currentUser.MustChangePassword
						controllers.LoadSecurityContext(ctx)
					}
				}
				if ctx.UserID == 0 && !publicSecurityPath(r.URL.Path) {
					response.NewErrorResponse(response.NeedLogin, i18n.GetText(ctx.Lang, "login_exp"), "").FPrint(w)
					return
				}
				if ctx.UserID > 0 && ctx.MustChangePassword != 0 && !mustChangePasswordAllowedPath(r.URL.Path) {
					response.NewErrorResponse(response.NoAccessPrivileges, "请先修改临时密码", "PASSWORD_CHANGE_REQUIRED").FPrint(w)
					return
				}
			} else if normalizedAPIPath(r.URL.Path) != "/api/setup" {
				response.NewErrorResponse(response.NeedSetup, "", "").FPrint(w)
				return
			}
			h(ctx, w, r)
		})).ServeHTTP(w, r)
	}
}

func mustChangePasswordAllowedPath(path string) bool {
	switch normalizedAPIPath(path) {
	case "/api/settings/modify_password", "/api/logout", "/api/user/info":
		return true
	default:
		return false
	}
}

func publicSecurityPath(path string) bool {
	switch normalizedAPIPath(path) {
	case "/api/ping", "/api/login", "/api/security/login/verify", "/api/security/recovery/begin", "/api/security/recovery/verify", "/api/security/recovery/complete", "/api/security/recovery/webauthn/begin", "/api/security/recovery/webauthn/verify", "/api/security/webauthn/login/begin", "/api/security/webauthn/login/finish":
		return true
	default:
		return false
	}
}
func credentialsQuestionHandler() http.HandlerFunc {
	get := contextIterceptor(controllers.GetSecurityQuestion)
	post := limitBody(16<<10, contextIterceptor(controllers.SetSecurityQuestion))
	return func(w http.ResponseWriter, r *http.Request) {
		setLegacyAPIHeaders(w, r)
		switch r.Method {
		case http.MethodGet:
			get.ServeHTTP(w, r)
		case http.MethodPost:
			post.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func onlyMethod(method string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		setLegacyAPIHeaders(w, r)
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	}
}

func limitBody(maxBytes int64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLegacyAPIHeaders(w, r)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if strings.HasPrefix(normalizedAPIPath(r.URL.Path), "/api/plugin/settings/") {
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'self'; form-action 'none'; img-src data:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'none'")
		} else if strings.HasPrefix(r.URL.Path, "/attachments/") {
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'; img-src 'self' data:")
		} else {
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; img-src 'self' data: blob: https:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self' https:; frame-src 'self' https:")
		}
		if !config.DevelopmentMode {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func stateChangingMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func hasSessionCookie(r *http.Request) bool {
	if session.Instance == nil {
		return false
	}
	cookie, err := r.Cookie(session.Instance.Cookie.Name)
	return err == nil && cookie.Value != ""
}

func loginRequestAllowed(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		return false
	}
	if strings.TrimSpace(r.Header.Get("Origin")) != "" || strings.TrimSpace(r.Header.Get("Referer")) != "" {
		return sameOriginRequest(r)
	}
	return true
}

func sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		origin = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	expectedScheme := "http"
	defaultPort := "80"
	if r.TLS != nil {
		expectedScheme = "https"
		defaultPort = "443"
	}
	if err != nil || u.User != nil || u.Scheme != expectedScheme || u.Host == "" {
		return false
	}
	originHost, originPort, ok := normalizeAuthority(u.Host, defaultPort)
	if !ok {
		return false
	}
	requestHost, requestPort, ok := normalizeAuthority(r.Host, defaultPort)
	if !ok || originHost != requestHost || originPort != requestPort {
		return false
	}
	if config.DevelopmentMode {
		return true
	}
	cfg := config.CurrentConfig()
	if cfg == nil {
		return false
	}
	configuredHost, _, ok := normalizeAuthority(strings.TrimSpace(cfg.WebDomain), defaultPort)
	return ok && requestHost == configuredHost
}

func normalizeAuthority(value string, defaultPort string) (string, string, bool) {
	parsed, err := url.Parse("//" + strings.TrimSpace(value))
	if err != nil || parsed.User != nil || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", false
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	port := parsed.Port()
	if port == "" {
		port = defaultPort
	}
	portNumber, err := strconv.Atoi(port)
	if host == "" || err != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", false
	}
	return host, strconv.Itoa(portNumber), true
}
