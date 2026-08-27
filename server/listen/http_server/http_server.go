package http_server

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/controllers/email"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

// 这个服务只负责 HTTP 到 HTTPS 的安全跳转，不承载业务会话。
var httpServer *http.Server

func HttpStop() {
	if httpServer != nil {
		if err := httpServer.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Warn("close HTTP server failed")
		}
		httpServer = nil
	}
}

func router(mux *http.ServeMux) error {
	fe, err := fs.Sub(local, "dist")
	if err != nil {
		return fmt.Errorf("load embedded frontend: %w", err)
	}
	// 超级管理员重置不属于主站 Web 路由，必须通过 smail-admin 本机安全通道执行。
	// 显式拦截大小写和尾斜杠变体，避免它们落入前端 SPA 回退。
	resetHandler := resetEntryHandler(fe)
	mux.HandleFunc("/reset", resetHandler)
	mux.HandleFunc("/reset/", resetHandler)
	mux.HandleFunc("/Reset", resetHandler)
	mux.HandleFunc("/Reset/", resetHandler)
	mux.Handle("/", guardedFrontendHandler(fe))
	mux.HandleFunc("/.well-known/", controllers.AcmeChallenge)
	mux.HandleFunc("/api/ping", controllers.Ping)
	mux.HandleFunc("/api/login", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.Login))))
	mux.HandleFunc("/api/security/verification/request", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.RequestVerification))))
	mux.HandleFunc("/api/security/verification/verify", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.VerifyVerification))))
	mux.HandleFunc("/api/security/login/verify", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.CompleteLoginVerification))))
	mux.HandleFunc("/api/security/webauthn/registration/begin", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.BeginWebAuthnRegistration))))
	mux.HandleFunc("/api/security/webauthn/registration/finish", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.FinishWebAuthnRegistration))))
	mux.HandleFunc("/api/security/webauthn/credentials", onlyMethod(http.MethodGet, contextIterceptor(controllers.ListWebAuthnCredentials)))
	mux.HandleFunc("/api/security/webauthn/credentials/rename", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.RenameWebAuthnCredential))))
	mux.HandleFunc("/api/security/webauthn/credentials/revoke", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.RevokeWebAuthnCredential))))
	mux.HandleFunc("/api/security/webauthn/login/begin", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.BeginWebAuthnLogin))))
	mux.HandleFunc("/api/security/webauthn/login/finish", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.FinishWebAuthnLogin))))
	mux.HandleFunc("/api/security/recovery/webauthn/begin", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.BeginWebAuthnRecovery))))
	mux.HandleFunc("/api/security/recovery/webauthn/verify", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.VerifyWebAuthnRecovery))))
	mux.HandleFunc("/api/security/recovery/begin", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.BeginPasswordRecovery))))
	mux.HandleFunc("/api/security/recovery/verify", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.VerifyPasswordRecovery))))
	mux.HandleFunc("/api/security/recovery/complete", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.CompletePasswordRecovery))))
	mux.HandleFunc("/api/security/credentials/question", credentialsQuestionHandler())
	mux.HandleFunc("/api/security/credentials/recovery-codes", onlyMethod(http.MethodGet, contextIterceptor(controllers.GetRecoveryCodeStatus)))
	mux.HandleFunc("/api/security/credentials/recovery-codes/generate", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.GenerateRecoveryCodes))))
	mux.HandleFunc("/api/security/credentials/recovery-codes/revoke", onlyMethod(http.MethodPost, limitBody(4<<10, contextIterceptor(controllers.RevokeRecoveryCodes))))
	mux.HandleFunc("/api/security/external-email/list", onlyMethod(http.MethodGet, contextIterceptor(controllers.ListExternalEmails)))
	mux.HandleFunc("/api/security/external-email/bind", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.BindExternalEmail))))
	mux.HandleFunc("/api/security/external-email/verify", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.VerifyExternalEmail))))
	mux.HandleFunc("/api/security/external-email/disable", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.DisableExternalEmail))))
	mux.HandleFunc("/api/logout", onlyMethod(http.MethodPost, limitBody(4<<10, contextIterceptor(controllers.Logout))))
	mux.HandleFunc("/api/group", onlyMethod(http.MethodGet, contextIterceptor(controllers.GetUserGroup)))
	mux.HandleFunc("/api/group/list", onlyMethod(http.MethodGet, contextIterceptor(controllers.GetUserGroupList)))
	mux.HandleFunc("/api/group/add", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.AddGroup))))
	mux.HandleFunc("/api/group/del", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.DelGroup))))
	mux.HandleFunc("/api/email/list", onlyMethod(http.MethodPost, limitBody(256<<10, contextIterceptor(email.EmailList))))
	mux.HandleFunc("/api/email/del", onlyMethod(http.MethodPost, limitBody(256<<10, contextIterceptor(email.EmailDelete))))
	mux.HandleFunc("/api/email/read", onlyMethod(http.MethodPost, limitBody(256<<10, contextIterceptor(email.MarkRead))))
	mux.HandleFunc("/api/email/detail", onlyMethod(http.MethodPost, limitBody(256<<10, contextIterceptor(email.EmailDetail))))
	mux.HandleFunc("/api/email/move", onlyMethod(http.MethodPost, limitBody(256<<10, contextIterceptor(email.Move))))
	mux.HandleFunc("/api/email/send", onlyMethod(http.MethodPost, limitBody(40<<20, contextIterceptor(email.Send))))
	mux.HandleFunc("/api/settings/modify_password", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.ModifyPassword))))
	mux.HandleFunc("/api/rule/get", onlyMethod(http.MethodPost, limitBody(4<<10, contextIterceptor(controllers.GetRule))))
	mux.HandleFunc("/api/rule/add", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.UpsertRule))))
	mux.HandleFunc("/api/rule/update", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.UpsertRule))))
	mux.HandleFunc("/api/rule/del", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.DelRule))))
	mux.HandleFunc("/attachments/", onlyMethod(http.MethodGet, contextIterceptor(controllers.GetAttachments)))
	mux.HandleFunc("/attachments/download/", onlyMethod(http.MethodGet, contextIterceptor(controllers.Download)))
	mux.HandleFunc("/api/user/create", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.CreateUser))))
	mux.HandleFunc("/api/user/edit", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.EditUser))))
	mux.HandleFunc("/api/user/info", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.Info))))
	mux.HandleFunc("/api/user/list", onlyMethod(http.MethodPost, limitBody(64<<10, contextIterceptor(controllers.UserList))))
	mux.HandleFunc("/api/plugin/settings/", contextIterceptor(controllers.SettingsHtml))
	mux.HandleFunc("/api/plugin/list", onlyMethod(http.MethodGet, contextIterceptor(controllers.GetPluginList)))
	mux.HandleFunc("/api/v1/config/", limitBody(4<<20, contextIterceptor(configRoute)))
	mux.HandleFunc("/api/v1/audits/config", limitBody(1<<20, contextIterceptor(configAuditRoute)))
	return nil
}

func resetEntryHandler(fe fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 该入口已经永久从主站移除。fe 参数保留以避免影响已有调用方，
		// 任何方法、任何会话状态都不得获得重置页面。
		_ = fe
		http.NotFound(w, r)
	}
}

func guardedFrontendHandler(fe fs.FS) http.Handler {
	frontend := http.FileServer(http.FS(fe))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isReservedResetPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		frontend.ServeHTTP(w, r)
	})
}

func isReservedResetPath(path string) bool {
	for range 2 {
		decoded, err := url.PathUnescape(path)
		if err != nil || decoded == path {
			break
		}
		path = decoded
	}
	path = strings.TrimRight(strings.TrimSpace(path), "/")
	path = strings.ToLower(path)
	return path == "/reset" || strings.HasPrefix(path, "/reset/")
}

func configRoute(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/v1/config/status":
		if !allowRouteMethod(w, r, http.MethodGet) {
			return
		}
		controllers.ConfigStatus(ctx, w, r)
	case path == "/api/v1/config/backups":
		if !allowRouteMethod(w, r, http.MethodGet) {
			return
		}
		controllers.ListConfigBackups(ctx, w, r)
	case path == "/api/v1/config/rollback/preview":
		if !allowRouteMethod(w, r, http.MethodPost) {
			return
		}
		controllers.PreviewConfigRollback(ctx, w, r)
	case path == "/api/v1/config/rollback/ticket":
		if !allowRouteMethod(w, r, http.MethodPost) {
			return
		}
		controllers.IssueConfigRollbackTicket(ctx, w, r)
	case path == "/api/v1/config/rollback":
		if !allowRouteMethod(w, r, http.MethodPost) {
			return
		}
		controllers.RollbackConfig(ctx, w, r)
	case configSectionRoute(path, "validate"):
		if !allowRouteMethod(w, r, http.MethodPost) {
			return
		}
		controllers.ValidateConfig(ctx, w, r)
	case configSectionRoute(path, "apply"):
		if !allowRouteMethod(w, r, http.MethodPost) {
			return
		}
		controllers.ApplyConfig(ctx, w, r)
	case configSectionRoute(path, ""):
		switch r.Method {
		case http.MethodGet:
			controllers.GetConfigSection(ctx, w, r)
		case http.MethodPut:
			controllers.UpdateConfig(ctx, w, r)
		default:
			allowRouteMethod(w, r, http.MethodGet, http.MethodPut)
		}
	default:
		http.NotFound(w, r)
	}
}

func configSectionRoute(path, operation string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if operation == "" {
		return len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "config"
	}
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "config" && parts[4] == operation
}
func configAuditRoute(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		controllers.ConfigAuditQuery(ctx, w, r)
	case http.MethodPost:
		controllers.ExportConfigAudit(ctx, w, r)
	default:
		allowRouteMethod(w, r, http.MethodGet, http.MethodPost)
	}
}

func allowRouteMethod(w http.ResponseWriter, r *http.Request, allowed ...string) bool {
	for _, method := range allowed {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func HttpStart() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	mux := http.NewServeMux()
	httpPort := 80
	if cfg.HttpPort > 0 {
		httpPort = cfg.HttpPort
	}

	var handler http.Handler = mux
	if config.DevelopmentMode && cfg.HttpsEnabled == 2 {
		if err := router(mux); err != nil {
			return err
		}
		handler = session.Instance.LoadAndSave(mux)
	} else {
		mux.HandleFunc("/.well-known/", controllers.AcmeChallenge)
		mux.HandleFunc("/api/ping", controllers.Ping)
		mux.HandleFunc("/", redirectToHTTPS)
	}

	httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", httpPort),
		Handler:           securityHeaders(handler),
		ReadTimeout:       time.Second * 90,
		ReadHeaderTimeout: time.Second * 10,
		WriteTimeout:      time.Second * 90,
		IdleTimeout:       time.Second * 30,
		MaxHeaderBytes:    16 << 10,
	}
	log.Infof("HTTP server started on port %d", httpPort)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}

func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	cfg := config.CurrentConfig()
	if cfg == nil {
		w.Header().Set("Connection", "close")
		http.Error(w, "HTTPS is required", http.StatusUpgradeRequired)
		return
	}
	host := strings.TrimSpace(cfg.WebDomain)
	if host == "" {
		w.Header().Set("Connection", "close")
		http.Error(w, "HTTPS is required", http.StatusUpgradeRequired)
		return
	}
	parsed, err := url.Parse("//" + host)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		w.Header().Set("Connection", "close")
		http.Error(w, "HTTPS is required", http.StatusUpgradeRequired)
		return
	}
	host = strings.TrimSuffix(parsed.Hostname(), ".")
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if cfg.HttpsPort > 0 && cfg.HttpsPort != 443 {
		host = fmt.Sprintf("%s:%d", host, cfg.HttpsPort)
	}
	target := "https://" + host + r.URL.EscapedPath()
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, target, http.StatusPermanentRedirect)
}
