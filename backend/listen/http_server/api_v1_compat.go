package http_server

import (
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/controllers/email"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// registerV1Routes 注册业务 API 的稳定版本入口。旧入口仍然保留，便于已有客户端平滑迁移。
func registerV1Routes(mux *http.ServeMux) {
	registerNotificationRoutes(mux)
	registerV1MethodRoute(mux, "/api/v1/security/setup/acknowledge", "/api/security/setup/acknowledge", http.MethodPost, 4<<10, controllers.AcknowledgeSecuritySetup)
	registerV1MethodRoute(mux, "/api/v1/login", "/api/login", http.MethodPost, 64<<10, controllers.Login)
	registerV1MethodRoute(mux, "/api/v1/security/reauth", "/api/security/reauth", http.MethodPost, 4<<10, controllers.ReauthenticateCurrentUser)
	registerV1MethodRoute(mux, "/api/v1/security/verification/request", "/api/security/verification/request", http.MethodPost, 16<<10, controllers.RequestVerification)
	registerV1MethodRoute(mux, "/api/v1/security/verification/verify", "/api/security/verification/verify", http.MethodPost, 16<<10, controllers.VerifyVerification)
	registerV1MethodRoute(mux, "/api/v1/security/login/verify", "/api/security/login/verify", http.MethodPost, 16<<10, controllers.CompleteLoginVerification)
	registerV1MethodRoute(mux, "/api/v1/security/login/resend", "/api/security/login/resend", http.MethodPost, 16<<10, controllers.ResendLoginVerification)
	registerV1MethodRoute(mux, "/api/v1/security/login/status", "/api/security/login/status", http.MethodPost, 4<<10, controllers.LoginVerificationStatus)
	registerV1MethodRoute(mux, "/api/v1/security/webauthn/registration/begin", "/api/security/webauthn/registration/begin", http.MethodPost, 16<<10, controllers.BeginWebAuthnRegistration)
	registerV1MethodRoute(mux, "/api/v1/security/webauthn/registration/finish", "/api/security/webauthn/registration/finish", http.MethodPost, 64<<10, controllers.FinishWebAuthnRegistration)
	registerV1MethodRoute(mux, "/api/v1/security/webauthn/credentials", "/api/security/webauthn/credentials", http.MethodGet, 0, controllers.ListWebAuthnCredentials)
	registerV1MethodRoute(mux, "/api/v1/security/webauthn/credentials/rename", "/api/security/webauthn/credentials/rename", http.MethodPost, 16<<10, controllers.RenameWebAuthnCredential)
	registerV1MethodRoute(mux, "/api/v1/security/webauthn/credentials/revoke", "/api/security/webauthn/credentials/revoke", http.MethodPost, 16<<10, controllers.RevokeWebAuthnCredential)
	registerV1MethodRoute(mux, "/api/v1/security/webauthn/login/begin", "/api/security/webauthn/login/begin", http.MethodPost, 16<<10, controllers.BeginWebAuthnLogin)
	registerV1MethodRoute(mux, "/api/v1/security/webauthn/login/finish", "/api/security/webauthn/login/finish", http.MethodPost, 64<<10, controllers.FinishWebAuthnLogin)
	registerV1MethodRoute(mux, "/api/v1/security/recovery/webauthn/begin", "/api/security/recovery/webauthn/begin", http.MethodPost, 16<<10, controllers.BeginWebAuthnRecovery)
	registerV1MethodRoute(mux, "/api/v1/security/recovery/webauthn/verify", "/api/security/recovery/webauthn/verify", http.MethodPost, 64<<10, controllers.VerifyWebAuthnRecovery)
	registerV1MethodRoute(mux, "/api/v1/security/recovery/begin", "/api/security/recovery/begin", http.MethodPost, 16<<10, controllers.BeginPasswordRecovery)
	registerV1MethodRoute(mux, "/api/v1/security/recovery/verify", "/api/security/recovery/verify", http.MethodPost, 16<<10, controllers.VerifyPasswordRecovery)
	registerV1MethodRoute(mux, "/api/v1/security/recovery/complete", "/api/security/recovery/complete", http.MethodPost, 16<<10, controllers.CompletePasswordRecovery)
	registerV1CredentialsQuestionRoute(mux)
	registerV1TOTPRoute(mux)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/recovery-codes", "/api/security/credentials/recovery-codes", http.MethodGet, 0, controllers.GetRecoveryCodeStatus)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/recovery-codes/generate", "/api/security/credentials/recovery-codes/generate", http.MethodPost, 16<<10, controllers.GenerateRecoveryCodes)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/recovery-codes/revoke", "/api/security/credentials/recovery-codes/revoke", http.MethodPost, 4<<10, controllers.RevokeRecoveryCodes)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/protocols", "/api/security/credentials/protocols", http.MethodGet, 0, controllers.ListProtocolCredentials)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/protocols/enabled", "/api/security/credentials/protocols/enabled", http.MethodPut, 16<<10, controllers.SetProtocolCredentialEnabled)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/protocols/generate", "/api/security/credentials/protocols/generate", http.MethodPost, 16<<10, controllers.GenerateProtocolCredential)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/protocols/revoke", "/api/security/credentials/protocols/revoke", http.MethodPost, 16<<10, controllers.RevokeProtocolCredential)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/protocols/reveal", "/api/security/credentials/protocols/reveal", http.MethodPost, 16<<10, controllers.RevealProtocolCredentials)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/clients", "/api/security/credentials/clients", http.MethodGet, 0, controllers.ListClientCredentials)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/clients/create", "/api/security/credentials/clients/create", http.MethodPost, 16<<10, controllers.CreateClientCredential)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/clients/regenerate", "/api/security/credentials/clients/regenerate", http.MethodPost, 16<<10, controllers.RegenerateClientCredential)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/clients/revoke", "/api/security/credentials/clients/revoke", http.MethodPost, 16<<10, controllers.RevokeClientCredential)
	registerV1MethodRoute(mux, "/api/v1/security/credentials/clients/reveal", "/api/security/credentials/clients/reveal", http.MethodPost, 16<<10, controllers.RevealClientCredentials)
	registerV1MethodRoute(mux, "/api/v1/security/external-email/list", "/api/security/external-email/list", http.MethodGet, 0, controllers.ListExternalEmails)
	registerV1MethodRoute(mux, "/api/v1/security/external-email/bind", "/api/security/external-email/bind", http.MethodPost, 16<<10, controllers.BindExternalEmail)
	registerV1MethodRoute(mux, "/api/v1/security/external-email/verify", "/api/security/external-email/verify", http.MethodPost, 16<<10, controllers.VerifyExternalEmail)
	registerV1MethodRoute(mux, "/api/v1/security/external-email/change", "/api/security/external-email/change", http.MethodPost, 16<<10, controllers.ChangeOwnExternalEmail)
	registerV1MethodRoute(mux, "/api/v1/security/external-email/change/verify", "/api/security/external-email/change/verify", http.MethodPost, 16<<10, controllers.VerifyOwnExternalEmail)
	registerV1MethodRoute(mux, "/api/v1/security/external-email/disable", "/api/security/external-email/disable", http.MethodPost, 16<<10, controllers.DisableExternalEmail)
	registerV1MethodRoute(mux, "/api/v1/logout", "/api/logout", http.MethodPost, 4<<10, controllers.Logout)
	registerV1MethodRoute(mux, "/api/v1/system/restart", "/api/v1/system/restart", http.MethodPost, 4<<10, controllers.RestartSmail)
	registerV1MethodRoute(mux, "/api/v1/system/restart/preview", "/api/v1/system/restart/preview", http.MethodPost, 4<<10, controllers.PreviewRestartSmail)

	registerV1MethodRoute(mux, "/api/v1/group", "/api/group", http.MethodGet, 0, controllers.GetUserGroup)
	registerV1MethodRoute(mux, "/api/v1/group/list", "/api/group/list", http.MethodGet, 0, controllers.GetUserGroupList)
	registerV1MethodRoute(mux, "/api/v1/group/add", "/api/group/add", http.MethodPost, 64<<10, controllers.AddGroup)
	registerV1MethodRoute(mux, "/api/v1/group/del", "/api/group/del", http.MethodPost, 64<<10, controllers.DelGroup)
	registerV1MethodRoute(mux, "/api/v1/email/list", "/api/email/list", http.MethodPost, 256<<10, email.EmailList)
	registerV1MethodRoute(mux, "/api/v1/email/del", "/api/email/del", http.MethodPost, 256<<10, email.EmailDelete)
	registerV1MethodRoute(mux, "/api/v1/email/read", "/api/email/read", http.MethodPost, 256<<10, email.MarkRead)
	registerV1MethodRoute(mux, "/api/v1/email/detail", "/api/email/detail", http.MethodPost, 256<<10, email.EmailDetail)
	registerV1MethodRoute(mux, "/api/v1/email/move", "/api/email/move", http.MethodPost, 256<<10, email.Move)
	registerV1MethodRoute(mux, "/api/v1/email/send", "/api/email/send", http.MethodPost, 40<<20, email.Send)
	registerV1MethodRoute(mux, "/api/v1/email/draft/save", "/api/email/draft/save", http.MethodPost, 40<<20, email.SaveDraft)
	registerV1MethodRoute(mux, "/api/v1/email/draft/detail", "/api/email/draft/detail", http.MethodPost, 64<<10, email.ReadDraft)
	registerV1MethodRoute(mux, "/api/v1/email/contacts", "/api/email/contacts", http.MethodGet, 0, email.ContactsList)
	registerV1MethodRoute(mux, "/api/v1/email/contacts/delete", "/api/email/contacts/delete", http.MethodPost, 16<<10, email.DeleteContact)
	registerV1MethodRoute(mux, "/api/v1/email/trust-sender", "/api/email/trust-sender", http.MethodPost, 16<<10, email.TrustSender)
	registerV1SenderFiltersRoute(mux)
	registerV1MethodRoute(mux, "/api/v1/email/sender-filters/delete", "/api/email/sender-filters/delete", http.MethodPost, 16<<10, email.DeleteSenderFilter)
	registerV1MethodRoute(mux, "/api/v1/email/sender-filters/toggle", "/api/email/sender-filters/toggle", http.MethodPost, 16<<10, email.ToggleSenderFilter)
	registerV1MethodRoute(mux, "/api/v1/settings/modify_password", "/api/settings/modify_password", http.MethodPost, 64<<10, controllers.ModifyPassword)
	registerV1MethodRoute(mux, "/api/v1/rule/get", "/api/rule/get", http.MethodPost, 4<<10, controllers.GetRule)
	registerV1MethodRoute(mux, "/api/v1/rule/add", "/api/rule/add", http.MethodPost, 64<<10, controllers.UpsertRule)
	registerV1MethodRoute(mux, "/api/v1/rule/update", "/api/rule/update", http.MethodPost, 64<<10, controllers.UpsertRule)
	registerV1MethodRoute(mux, "/api/v1/rule/del", "/api/rule/del", http.MethodPost, 64<<10, controllers.DelRule)
	registerV1MethodRoute(mux, "/api/v1/user/create", "/api/user/create", http.MethodPost, 64<<10, controllers.CreateUser)
	registerV1MethodRoute(mux, "/api/v1/user/edit", "/api/user/edit", http.MethodPost, 64<<10, controllers.EditUser)
	registerV1MethodRoute(mux, "/api/v1/user/profile", "/api/user/profile", http.MethodPost, 64<<10, controllers.UpdateProfile)
	registerV1MethodRoute(mux, "/api/v1/user/info", "/api/user/info", http.MethodPost, 64<<10, controllers.Info)
	registerV1MethodRoute(mux, "/api/v1/user/list", "/api/user/list", http.MethodPost, 64<<10, controllers.UserList)
	registerV1MethodRoute(mux, "/api/v1/user/groups", "/api/user/groups", http.MethodGet, 0, controllers.GetAccountGroups)
	registerV1MethodRoute(mux, "/api/v1/user/groups/save", "/api/user/groups/save", http.MethodPost, 4<<10, controllers.SaveAccountGroup)
	registerV1MethodRoute(mux, "/api/v1/user/groups/membership", "/api/user/groups/membership", http.MethodPost, 4<<10, controllers.SetAccountGroupMembership)
	registerV1MethodRoute(mux, "/api/v1/user/security/recovery-codes", "/api/user/security/recovery-codes", http.MethodGet, 0, controllers.GetManagedRecoveryCodeStatus)
	registerV1MethodRoute(mux, "/api/v1/user/security/recovery-codes/generate", "/api/user/security/recovery-codes/generate", http.MethodPost, 16<<10, controllers.GenerateManagedRecoveryCodes)
	registerV1MethodRoute(mux, "/api/v1/user/security/recovery-codes/revoke", "/api/user/security/recovery-codes/revoke", http.MethodPost, 4<<10, controllers.RevokeManagedRecoveryCodes)
	registerV1MethodRoute(mux, "/api/v1/user/security/external-email/list", "/api/user/security/external-email/list", http.MethodGet, 0, controllers.ListManagedExternalEmails)
	registerV1MethodRoute(mux, "/api/v1/user/security/external-email/change", "/api/user/security/external-email/change", http.MethodPost, 16<<10, controllers.ChangeManagedExternalEmail)
	registerV1MethodRoute(mux, "/api/v1/user/security/external-email/verify", "/api/user/security/external-email/verify", http.MethodPost, 16<<10, controllers.VerifyManagedExternalEmail)
	registerV1PluginRoutes(mux)
}

func registerV1MethodRoute(mux *http.ServeMux, path string, legacyPath string, method string, maxBody int64, handler controllers.HandlerFunc) {
	wrapped := contextIterceptor(rewriteV1Path(legacyPath, handler))
	if maxBody > 0 {
		wrapped = limitBody(maxBody, wrapped)
	}
	mux.HandleFunc(path, onlyMethod(method, wrapped))
}

func registerNotificationRoutes(mux *http.ServeMux) {
	registerV1MethodRoute(mux, "/api/v1/system/notification/status", "/api/system/notification/status", http.MethodGet, 0, controllers.GetNotificationStatus)
	registerV1MethodRoute(mux, "/api/v1/system/notification/accounts", "/api/system/notification/accounts", http.MethodGet, 0, controllers.ListNotificationAccounts)
	registerV1MethodRoute(mux, "/api/v1/system/notification/authorization", "/api/system/notification/authorization", http.MethodPost, 4<<10, controllers.SetNotificationAuthorization)
	registerV1MethodRoute(mux, "/api/v1/system/notification/credential", "/api/system/notification/credential", http.MethodPost, 4<<10, controllers.RotateNotificationCredential)
}

func registerV1SenderFiltersRoute(mux *http.ServeMux) {
	get := contextIterceptor(rewriteV1Path("/api/email/sender-filters", email.SenderFiltersList))
	post := limitBody(16<<10, contextIterceptor(rewriteV1Path("/api/email/sender-filters", email.AddSenderFilter)))
	del := limitBody(16<<10, contextIterceptor(rewriteV1Path("/api/email/sender-filters", email.DeleteSenderFilter)))
	mux.HandleFunc("/api/v1/email/sender-filters", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			get.ServeHTTP(w, r)
		case http.MethodPost:
			post.ServeHTTP(w, r)
		case http.MethodDelete:
			del.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, POST, DELETE")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func registerV1CredentialsQuestionRoute(mux *http.ServeMux) {
	get := contextIterceptor(rewriteV1Path("/api/security/credentials/question", controllers.GetSecurityQuestion))
	post := limitBody(16<<10, contextIterceptor(rewriteV1Path("/api/security/credentials/question", controllers.SetSecurityQuestion)))
	mux.HandleFunc("/api/v1/security/credentials/question", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			get.ServeHTTP(w, r)
		case http.MethodPost:
			post.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func registerV1TOTPRoute(mux *http.ServeMux) {
	handler := limitBody(4<<10, contextIterceptor(rewriteV1Path("/api/security/totp", controllers.TOTPSettings)))
	mux.HandleFunc("/api/v1/security/totp", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodPost:
			handler.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
}

func registerV1PluginRoutes(mux *http.ServeMux) {
	for _, id := range []string{"telegram_push", "wechat_push"} {
		mux.HandleFunc("/api/v1/plugin/secrets/"+id, limitBody(16<<10, contextIterceptor(controllers.PluginSecret)))
	}
	getUserSettings := contextIterceptor(controllers.GetTelegramUserSettings)
	updateUserSettings := limitBody(16<<10, contextIterceptor(controllers.UpdateTelegramUserSettings))
	mux.HandleFunc("/api/v1/plugin/settings/telegram_push/user", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getUserSettings.ServeHTTP(w, r)
		case http.MethodPut:
			updateUserSettings.ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/plugin/settings/telegram_push/bot", onlyMethod(http.MethodGet, contextIterceptor(controllers.GetTelegramBotInfo)))
	mux.HandleFunc("/api/v1/plugin/settings/telegram_push/test", onlyMethod(http.MethodPost, limitBody(16<<10, contextIterceptor(controllers.TestTelegramMessage))))
	mux.HandleFunc("/api/v1/plugin/settings/", contextIterceptor(rewriteV1PathPrefix("/api/plugin/settings/", controllers.SettingsHtml)))
	registerV1MethodRoute(mux, "/api/v1/plugin/list", "/api/plugin/list", http.MethodGet, 0, controllers.GetPluginList)
}

func normalizedAPIPath(path string) string {
	if path == "/api/v1" {
		return "/api"
	}
	if strings.HasPrefix(path, "/api/v1/") {
		return "/api/" + strings.TrimPrefix(path, "/api/v1/")
	}
	return path
}

func setLegacyAPIHeaders(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		w.Header().Set("Deprecation", "true")
		w.Header().Set("X-Smail-API-Version", "legacy")
		w.Header().Set("Link", "</api/v1>; rel=\"successor-version\"")
	}
}

func rewriteV1Path(legacyPath string, handler controllers.HandlerFunc) controllers.HandlerFunc {
	return func(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
		originalPath := r.URL.Path
		originalRawPath := r.URL.RawPath
		r.URL.Path = legacyPath
		r.URL.RawPath = ""
		defer func() {
			r.URL.Path = originalPath
			r.URL.RawPath = originalRawPath
		}()
		handler(ctx, w, r)
	}
}

func rewriteV1PathPrefix(legacyPrefix string, handler controllers.HandlerFunc) controllers.HandlerFunc {
	return func(ctx *context.Context, w http.ResponseWriter, r *http.Request) {
		originalPath := r.URL.Path
		originalRawPath := r.URL.RawPath
		path := strings.TrimPrefix(originalPath, "/api/v1/plugin/settings/")
		r.URL.Path = strings.TrimSuffix(legacyPrefix, "/") + "/" + path
		r.URL.RawPath = ""
		defer func() {
			r.URL.Path = originalPath
			r.URL.RawPath = originalRawPath
		}()
		handler(ctx, w, r)
	}
}
