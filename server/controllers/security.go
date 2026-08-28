package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/verification"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

// PublicRecoveryEnabled 判断公开自助恢复入口是否已经完整配置。
// 分区配置可用时，以分区配置作为唯一依据。
func PublicRecoveryEnabled() bool {
	if manager := config.CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		hosts := append([]string{runtime.Root.Domains.Web, runtime.Root.Domains.Primary}, runtime.Root.Domains.AccessHosts...)
		return runtime.SetupState == config.SetupStateReady && runtime.Email.Policy.ExternalRecoveryEnabled && hasConfiguredRecoveryHost(hosts...)
	}
	current := config.CurrentConfig()
	if current == nil || !current.IsSetupReady() || !current.ExternalRecoveryEnabled {
		return false
	}
	hosts := append([]string{current.WebDomain, current.Domain}, current.AccessHosts...)
	return hasConfiguredRecoveryHost(hosts...)
}

func hasConfiguredRecoveryHost(values ...string) bool {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.ContainsAny(value, " \t\r\n/@\\?#") {
			continue
		}
		parsed, err := url.Parse("//" + value)
		port := parsed.Port()
		portOK := port == ""
		if port != "" {
			portNumber, portErr := strconv.Atoi(port)
			portOK = portErr == nil && portNumber > 0 && portNumber <= 65535
		}
		if err == nil && portOK && parsed.Hostname() != "" && parsed.User == nil && parsed.Path == "" && parsed.RawQuery == "" && parsed.Fragment == "" {
			return true
		}
	}
	return false
}

func rejectUnavailableRecovery(w http.ResponseWriter) bool {
	if PublicRecoveryEnabled() {
		return false
	}
	writeSecurityError(w, response.ParamsError, "密码重置功能未启用")
	return true
}

var securityRuntime = struct {
	verification *verification.Service
	external     *externalemail.Repository
	recovery     *recovery.Service
	credentials  *credentials.Service
	webauthn     *webauthnservice.Service
}{}

// ConfigureSecurityRuntime 由主程序在数据库和内存安全服务就绪后调用。
func ConfigureSecurityRuntime(verify *verification.Service, external *externalemail.Repository, recover *recovery.Service, credential *credentials.Service) {
	securityRuntime.verification = verify
	securityRuntime.external = external
	securityRuntime.recovery = recover
	securityRuntime.credentials = credential
	if recover != nil {
		recover.SetSecurityStateInvalidator(InvalidateSecurityRuntimeUser)
	}
}

func ConfigureWebAuthnRuntime(service *webauthnservice.Service) {
	securityRuntime.webauthn = service
}

// InvalidateSecurityRuntimeUser 清除密码重置后仍驻留在内存中的安全流程状态。
func InvalidateSecurityRuntimeUser(userID int, account string) {
	if userID <= 0 {
		return
	}
	if securityRuntime.recovery != nil {
		securityRuntime.recovery.InvalidateUser(userID)
	}
	if securityRuntime.verification != nil && securityRuntime.verification.Store != nil {
		securityRuntime.verification.Store.InvalidateAccount(account)
	}
	if securityRuntime.webauthn != nil {
		securityRuntime.webauthn.InvalidateUser(userID)
	}
	loginVerificationState.Lock()
	for id, item := range loginVerificationState.items {
		if item.UserID == userID || strings.EqualFold(item.Account, account) {
			delete(loginVerificationState.items, id)
		}
	}
	loginVerificationState.Unlock()
}

func LoadSecurityContext(ctx *context.Context) {
	if ctx == nil || ctx.UserID <= 0 {
		return
	}
	if ctx.EffectiveRole() == access.RoleSuperAdmin {
		ctx.RecoveryCodeConfigured = false
		return
	}
	service := securityRuntime.credentials
	if service == nil && db.Instance != nil {
		service, _ = credentials.NewService(db.Instance)
	}
	if service == nil {
		return
	}
	configured, err := service.HasValidRecoveryCode(ctx.UserID)
	if err == nil {
		ctx.RecoveryCodeConfigured = configured
	}
}

func credentialRuntime() *credentials.Service {
	if securityRuntime.credentials != nil {
		return securityRuntime.credentials
	}
	if db.Instance == nil {
		return nil
	}
	service, _ := credentials.NewService(db.Instance)
	return service
}

type verificationRequest struct {
	Purpose    verification.Purpose `json:"purpose"`
	Identifier string               `json:"identifier"`
	Account    string               `json:"account"`
	Email      string               `json:"email"`
}

type verificationCheckRequest struct {
	ID           string               `json:"id"`
	Method       string               `json:"method"`
	Purpose      verification.Purpose `json:"purpose"`
	Identifier   string               `json:"identifier"`
	Account      string               `json:"account"`
	Email        string               `json:"email"`
	Code         string               `json:"code"`
	Question     string               `json:"question"`
	Answer       string               `json:"answer"`
	RecoveryCode string               `json:"recovery_code"`
	BindingID    int                  `json:"binding_id"`
}

type recoveryCompleteRequest struct {
	Ticket      string `json:"ticket"`
	NewPassword string `json:"new_password"`
}

const recoveryContextCookieName = "smail_recovery_context"

type externalEmailRequest struct {
	Email string `json:"email"`
	Type  string `json:"type"`
	ID    int    `json:"id"`
}

type securityQuestionRequest struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type recoveryCodeRequest struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

func rejectSuperAdminRecoveryCodes(ctx *context.Context, w http.ResponseWriter) bool {
	if ctx != nil && ctx.EffectiveRole() == access.RoleSuperAdmin {
		writeSecurityError(w, response.NoAccessPrivileges, "超级管理员恢复码只能通过部署端专用通道管理")
		return true
	}
	return false
}

// GetSecurityQuestion 返回当前用户的密保问题，不返回答案哈希。
func GetSecurityQuestion(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	question, configured, err := service.GetSecurityQuestion(ctx.UserID)
	if err != nil {
		log.WithError(err).Error("load security question failed")
		writeSecurityError(w, response.ServerError, "读取密保问题失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"configured": configured, "question": question}).FPrint(w)
}

// SetSecurityQuestion 设置或替换当前用户的密保问题。
func SetSecurityQuestion(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input securityQuestionRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	if err := service.SetSecurityQuestion(ctx.UserID, input.Question, input.Answer); err != nil {
		writeSecurityError(w, response.ParamsError, "密保问题设置失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"configured": true}).FPrint(w)
}

// GenerateRecoveryCodes 生成一组只显示一次的离线恢复码。
func GenerateRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if rejectSuperAdminRecoveryCodes(ctx, w) {
		return
	}
	var input recoveryCodeRequest
	if req != nil && req.Body != nil {
		if err := decodeJSONRequest(w, req, &input); err != nil {
			writeSecurityError(w, response.ParamsError, "请求无效")
			return
		}
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	codes, err := service.GenerateRecoveryCodes(ctx.UserID, input.Count)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "离线恢复码生成失败")
		return
	}
	ctx.RecoveryCodeConfigured = len(codes) > 0
	response.NewSuccessResponse(map[string]any{"codes": codes, "shown_once": true}).FPrint(w)
}

// GetRecoveryCodeStatus 返回当前用户是否配置了有效离线恢复码。
func GetRecoveryCodeStatus(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if rejectSuperAdminRecoveryCodes(ctx, w) {
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	configured, err := service.HasValidRecoveryCode(ctx.UserID)
	if err != nil {
		writeSecurityError(w, response.ServerError, "读取恢复码状态失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"configured": configured}).FPrint(w)
}

// RevokeRecoveryCodes 撤销当前用户的全部有效离线恢复码。
func RevokeRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if rejectSuperAdminRecoveryCodes(ctx, w) {
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	if err := service.RevokeRecoveryCodes(ctx.UserID); err != nil {
		writeSecurityError(w, response.ServerError, "撤销恢复码失败")
		return
	}
	ctx.RecoveryCodeConfigured = false
	response.NewSuccessResponse(map[string]any{"revoked": true}).FPrint(w)
}

// RequestVerification 统一处理登录、密码恢复和外部邮箱验证申请。
func RequestVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input verificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	if input.Account == "" {
		input.Account = input.Identifier
	}
	if !strings.EqualFold(strings.TrimSpace(input.Account), strings.TrimSpace(ctx.UserAccount)) {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	if input.Email == "" || strings.TrimSpace(input.Account) == "" || !verification.IsSupportedPurpose(input.Purpose) || securityRuntime.verification == nil {
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	challenge, err := securityRuntime.verification.Issue(verification.IssueRequest{Purpose: input.Purpose, UserID: ctx.UserID, Account: input.Account, Email: input.Email}, remoteIP(req))
	if err != nil {
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	response.NewSuccessResponse(map[string]any{"id": challenge.ID, "expires_at": challenge.ExpiresAt.UTC()}).FPrint(w)
}

// VerifyVerification 消费一次性验证码，只返回后续流程所需的短时票据。
func VerifyVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input verificationCheckRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || securityRuntime.verification == nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if input.Account == "" {
		input.Account = input.Identifier
	}
	if !strings.EqualFold(strings.TrimSpace(input.Account), strings.TrimSpace(ctx.UserAccount)) {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if err := securityRuntime.verification.Verify(input.ID, input.Account, input.Email, input.Code, input.Purpose, remoteIP(req)); err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"verified": true}).FPrint(w)
}

func BeginPasswordRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if rejectUnavailableRecovery(w) {
		return
	}
	var input verificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || securityRuntime.recovery == nil {
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	beginKey := emailRecoveryBeginIPAttempts.ipKey(req)
	if emailRecoveryBeginIPAttempts.blocked(beginKey) {
		id, err := newOpaqueRecoveryChallengeID()
		if err != nil {
			writeSecurityError(w, response.ParamsError, "\u8bf7\u6c42\u5df2\u53d7\u7406")
			return
		}
		response.NewSuccessResponse(map[string]any{"id": id}).FPrint(w)
		return
	}
	if input.Account == "" {
		input.Account = input.Identifier
	}
	browserContext, ok := recoveryBrowserContext(req, w, true)
	if !ok {
		writeSecurityError(w, response.ParamsError, "\u8bf7\u6c42\u5df2\u53d7\u7406")
		return
	}
	id, err := securityRuntime.recovery.Begin(recovery.BeginRequest{Identifier: input.Account, Email: input.Email, BrowserContext: browserContext}, remoteIP(req))
	if err != nil && id == "" {
		emailRecoveryFailureIPAttempts.recordFailure(emailRecoveryFailureIPAttempts.ipKey(req))
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	emailRecoveryBeginIPAttempts.recordFailure(beginKey)
	response.NewSuccessResponse(map[string]any{"id": id}).FPrint(w)
}

func VerifyPasswordRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if rejectUnavailableRecovery(w) {
		return
	}
	failureKey := emailRecoveryFailureIPAttempts.ipKey(req)
	if emailRecoveryFailureIPAttempts.blocked(failureKey) {
		writeSecurityError(w, response.ParamsError, "\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	var input verificationCheckRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || securityRuntime.recovery == nil {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if input.Account == "" {
		input.Account = input.Identifier
	}
	var ticket string
	var err error
	methodOffline := strings.EqualFold(strings.TrimSpace(input.Method), "offline")
	browserContext, ok := recoveryBrowserContext(req, w, methodOffline)
	if !ok {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	if methodOffline {
		ticket, err = securityRuntime.recovery.VerifyOfflineWithContext(input.Account, input.Question, input.Answer, input.RecoveryCode, remoteIP(req), browserContext)
	} else {
		ticket, err = securityRuntime.recovery.VerifyWithContext(input.ID, input.Account, input.Email, input.Code, input.RecoveryCode, remoteIP(req), browserContext)
	}
	if err != nil {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"ticket": ticket}).FPrint(w)
}

func CompletePasswordRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if rejectUnavailableRecovery(w) {
		return
	}
	failureKey := emailRecoveryFailureIPAttempts.ipKey(req)
	if emailRecoveryFailureIPAttempts.blocked(failureKey) {
		writeSecurityError(w, response.ParamsError, "\u91cd\u7f6e\u5931\u8d25")
		return
	}
	var input recoveryCompleteRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || securityRuntime.recovery == nil || strings.TrimSpace(input.Ticket) == "" {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "重置失败")
		return
	}
	browserContext, ok := recoveryBrowserContext(req, w, false)
	if !ok {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	if err := securityRuntime.recovery.CompleteWithContext(input.Ticket, input.NewPassword, browserContext); err != nil {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "重置失败")
		return
	}
	clearRecoveryBrowserContext(w, req)
	response.NewSuccessResponse(map[string]any{"reset": true}).FPrint(w)
}

func recoveryBrowserContext(req *http.Request, w http.ResponseWriter, create bool) (string, bool) {
	if req == nil || w == nil {
		return "", false
	}
	if cookie, err := req.Cookie(recoveryContextCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value, true
	}
	if !create {
		return "", false
	}
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", false
	}
	value := hex.EncodeToString(raw[:])
	secure := req.TLS != nil || !config.DevelopmentMode
	http.SetCookie(w, &http.Cookie{
		Name: recoveryContextCookieName, Value: value, Path: "/api/security/recovery",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 15 * 60,
	})
	return value, true
}

func clearRecoveryBrowserContext(w http.ResponseWriter, req *http.Request) {
	if w == nil {
		return
	}
	secure := req != nil && (req.TLS != nil || !config.DevelopmentMode)
	http.SetCookie(w, &http.Cookie{
		Name: recoveryContextCookieName, Value: "", Path: "/api/security/recovery",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
}

func newOpaqueRecoveryChallengeID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func ListExternalEmails(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || securityRuntime.external == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	items, err := securityRuntime.external.List(ctx.UserID)
	if err != nil {
		log.WithError(err).Error("list external email failed")
		writeSecurityError(w, response.ServerError, "读取外部邮箱失败")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		email, err := securityRuntime.external.Decrypt(item)
		if err != nil {
			continue
		}
		result = append(result, map[string]any{"id": item.ID, "type": item.EmailType, "status": item.Status, "email": maskEmail(email), "created_at": item.CreatedAt, "updated_at": item.UpdatedAt})
	}
	response.NewSuccessResponse(result).FPrint(w)
}

func BindExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || securityRuntime.external == nil || securityRuntime.verification == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input externalEmailRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	item, err := securityRuntime.external.Bind(externalemail.BindRequest{UserID: ctx.UserID, Email: input.Email, Type: input.Type})
	if err != nil {
		writeSecurityError(w, response.ParamsError, "外部邮箱绑定失败")
		return
	}
	challenge, err := securityRuntime.verification.Issue(verification.IssueRequest{Purpose: verification.PurposeExternalEmailBind, UserID: ctx.UserID, Account: ctx.UserAccount, Email: input.Email, ResourceID: item.ID}, remoteIP(req))
	if err != nil {
		_ = securityRuntime.external.AbandonPending(ctx.UserID, item.ID)
		writeSecurityError(w, response.ParamsError, "外部邮箱绑定失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"id": item.ID, "challenge_id": challenge.ID, "status": item.Status, "email": maskEmail(input.Email), "expires_at": challenge.ExpiresAt.UTC()}).FPrint(w)
}

func VerifyExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || securityRuntime.external == nil || securityRuntime.verification == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input verificationCheckRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.BindingID <= 0 {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if err := securityRuntime.verification.VerifyBound(input.ID, ctx.UserAccount, input.Email, input.Code, verification.PurposeExternalEmailBind, input.BindingID, remoteIP(req)); err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if err := securityRuntime.external.Verify(ctx.UserID, input.BindingID); err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"verified": true}).FPrint(w)
}

func DisableExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || securityRuntime.external == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input externalEmailRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ID <= 0 {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	if err := securityRuntime.external.Disable(ctx.UserID, input.ID); err != nil {
		writeSecurityError(w, response.ParamsError, "外部邮箱停用失败")
		return
	}
	if securityRuntime.verification != nil && securityRuntime.verification.Store != nil {
		securityRuntime.verification.Store.InvalidateResource(input.ID, verification.PurposeExternalEmailBind)
		securityRuntime.verification.Store.InvalidateAccount(ctx.UserAccount)
	}
	if securityRuntime.recovery != nil {
		securityRuntime.recovery.InvalidateUser(ctx.UserID)
	}
	loginVerificationState.Lock()
	for id, item := range loginVerificationState.items {
		if item.UserID == ctx.UserID {
			delete(loginVerificationState.items, id)
		}
	}
	loginVerificationState.Unlock()
	response.NewSuccessResponse(map[string]any{"disabled": true}).FPrint(w)
}

func remoteIP(req *http.Request) string {
	if req == nil {
		return ""
	}
	return normalizeRemoteHost(req.RemoteAddr)
}

func normalizeRemoteHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(value, "[]")
	if parsed := net.ParseIP(value); parsed != nil {
		return parsed.String()
	}
	return value
}

func maskEmail(value string) string {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(value)), "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "***"
	}
	if len(parts[0]) == 1 {
		return "*@" + parts[1]
	}
	return parts[0][:1] + "***@" + parts[1]
}

func writeSecurityError(w http.ResponseWriter, code int, message string) {
	response.NewErrorResponse(code, message, "").FPrint(w)
}
