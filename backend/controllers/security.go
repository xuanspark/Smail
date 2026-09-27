package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/totp"
	"git.pcux.cn/xuans/Smail/services/verification"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	"git.pcux.cn/xuans/Smail/utils/clientip"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/password"
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

// PublicMailRecoveryEnabled 仅判断 Web 域名下匿名恢复链接入口是否可用。
// 旧主站恢复流程仍使用 PublicRecoveryEnabled，以保持原有兼容行为。
func PublicMailRecoveryEnabled() bool {
	if !config.IsSetupReady() || !config.CurrentEmailPolicy().ExternalRecoveryEnabled {
		return false
	}
	return hasConfiguredRecoveryHost(config.CurrentWebDomain())
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

type securityRuntimeState struct {
	verification *verification.Service
	external     *externalemail.Repository
	recovery     *recovery.Service
	credentials  *credentials.Service
	webauthn     *webauthnservice.Service
	totp         *totp.Service
}

var securityRuntime securityRuntimeState
var securityRuntimeMu sync.RWMutex

// 每个请求持有同一份服务快照，发布锁不跨越数据库、投递和失效回调。
func currentSecurityRuntime() securityRuntimeState {
	securityRuntimeMu.RLock()
	defer securityRuntimeMu.RUnlock()
	return securityRuntime
}

// ConfigureSecurityRuntime 由主程序在数据库和内存安全服务就绪后调用。
func ConfigureSecurityRuntime(verify *verification.Service, external *externalemail.Repository, recover *recovery.Service, credential *credentials.Service) {
	if recover != nil {
		recover.SetSecurityStateInvalidator(InvalidateSecurityRuntimeUser)
	}
	securityRuntimeMu.Lock()
	defer securityRuntimeMu.Unlock()
	securityRuntime.verification = verify
	securityRuntime.external = external
	securityRuntime.recovery = recover
	securityRuntime.credentials = credential
}

func ConfigureWebAuthnRuntime(service *webauthnservice.Service) {
	securityRuntimeMu.Lock()
	defer securityRuntimeMu.Unlock()
	securityRuntime.webauthn = service
}

func ConfigureTOTPRuntime(service *totp.Service) {
	securityRuntimeMu.Lock()
	defer securityRuntimeMu.Unlock()
	securityRuntime.totp = service
}

// ClearSecurityRuntime 在停止入口后一次撤下全部安全服务，避免保留已停止的旧库依赖。
func ClearSecurityRuntime() {
	securityRuntimeMu.Lock()
	securityRuntime = securityRuntimeState{}
	securityRuntimeMu.Unlock()
	loginVerificationState.Lock()
	clear(loginVerificationState.items)
	loginVerificationState.Unlock()
}

// InvalidateSecurityRuntimeUser 清除密码重置后仍驻留在内存中的安全流程状态。
func InvalidateSecurityRuntimeUser(userID int, account string) {
	if userID <= 0 {
		return
	}
	runtime := currentSecurityRuntime()
	if runtime.totp != nil {
		runtime.totp.InvalidateUser(userID)
	}
	if runtime.recovery != nil {
		runtime.recovery.InvalidateUser(userID)
	}
	if runtime.verification != nil && runtime.verification.Store != nil {
		runtime.verification.Store.InvalidateAccount(account)
	}
	if runtime.webauthn != nil {
		runtime.webauthn.InvalidateUser(userID)
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
	service := currentSecurityRuntime().credentials
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
	if service := currentSecurityRuntime().credentials; service != nil {
		if service.VaultRoot() == "" && config.ROOT_PATH != "" {
			service.SetVaultRoot(config.ROOT_PATH)
		}
		return service
	}
	if db.Instance == nil {
		return nil
	}
	service, _ := credentials.NewService(db.Instance)
	if service != nil && config.ROOT_PATH != "" {
		service.SetVaultRoot(config.ROOT_PATH)
	}
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
	Continuation string               `json:"continuation"`
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

func requireSuperAdminRecoveryCodeAuth(ctx *context.Context, w http.ResponseWriter) bool {
	if ctx != nil && ctx.EffectiveRole() == access.RoleSuperAdmin && !ctx.RecentAuth {
		writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "请先重新验证当前密码", string(access.CodeRecentAuthRequired))
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
		if writePasswordBusy(w, err) {
			return
		}
		writeSecurityError(w, response.ParamsError, "密保问题设置失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"configured": true}).FPrint(w)
}

// GenerateRecoveryCodes 生成一组离线恢复码，未使用的有效恢复码可在安全验证后再次查看。
func GenerateRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if requireSuperAdminRecoveryCodeAuth(ctx, w) {
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
	codes, err := service.GenerateRecoveryCodesContext(recoveryRequestContext(req), ctx.UserID, input.Count)
	if err != nil {
		if writePasswordBusy(w, err) {
			return
		}
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

// RevealRecoveryCodes 在服务端确认近期安全验证后返回当前用户有效安全码。
func RevealRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if !ctx.RecentAuth {
		writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "请先重新验证当前密码", string(access.CodeRecentAuthRequired))
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	codes, err := service.RevealRecoveryCodes(ctx.UserID)
	if err != nil {
		if errors.Is(err, credentials.ErrNoCiphertext) {
			writeSecurityError(w, response.ParamsError, "现有安全码无法重新查看，请重新生成安全码")
			return
		}
		if errors.Is(err, credentials.ErrCodeNotFound) {
			writeSecurityError(w, response.ParamsError, "当前没有有效安全码")
			return
		}
		log.WithError(err).Error("reveal recovery codes failed")
		writeSecurityError(w, response.ServerError, "读取安全码失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"codes": codes, "revealed": true}).FPrint(w)
}

// RevokeRecoveryCodes 撤销当前用户的全部有效离线恢复码。
func RevokeRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if requireSuperAdminRecoveryCodeAuth(ctx, w) {
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
type protocolCredentialRequest struct {
	Protocol string `json:"protocol"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

func requireProtocolCredentialContext(ctx *context.Context, w http.ResponseWriter) (*credentials.Service, bool) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return nil, false
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "安全设置暂时不可用")
		return nil, false
	}
	return service, true
}

// ListProtocolCredentials 返回当前用户的协议开关和授权码状态。
func ListProtocolCredentials(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	items, err := service.ListProtocolCredentials(ctx.UserID)
	if err != nil {
		log.WithError(err).Error("load protocol credentials failed")
		writeSecurityError(w, response.ParamsError, "读取邮件客户端协议设置失败")
		return
	}
	response.NewSuccessResponse(items).FPrint(w)
}

// SetProtocolCredentialEnabled 设置当前用户的邮件客户端协议开关，不改变服务监听状态。
func SetProtocolCredentialEnabled(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input protocolCredentialRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.Enabled == nil {
		writeSecurityError(w, response.ParamsError, "协议设置请求无效")
		return
	}
	if err := service.SetProtocolEnabled(ctx.UserID, input.Protocol, *input.Enabled); err != nil {
		writeSecurityError(w, response.ParamsError, "协议设置失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"protocol": strings.ToLower(strings.TrimSpace(input.Protocol)), "enabled": *input.Enabled}).FPrint(w)
}

// GenerateProtocolCredential 为当前用户生成指定协议授权码，明文只返回一次。
func GenerateProtocolCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input protocolCredentialRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.Protocol) == "" {
		writeSecurityError(w, response.ParamsError, "协议请求无效")
		return
	}
	code, err := service.GenerateProtocolCode(ctx.UserID, input.Protocol)
	if err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		writeSecurityError(w, response.ParamsError, "请先开启协议后再生成授权码")
		return
	}
	protocol, _ := credentials.NormalizeProtocol(input.Protocol)
	response.NewSuccessResponse(map[string]any{"protocol": protocol, "code": code, "shown_once": true}).FPrint(w)
}

// RevokeProtocolCredential 撤销当前用户指定协议的授权码。
func RevokeProtocolCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input protocolCredentialRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.Protocol) == "" {
		writeSecurityError(w, response.ParamsError, "协议请求无效")
		return
	}
	if err := service.RevokeProtocolCode(ctx.UserID, input.Protocol); err != nil {
		writeSecurityError(w, response.ParamsError, "撤销授权码失败")
		return
	}
	protocol, _ := credentials.NormalizeProtocol(input.Protocol)
	response.NewSuccessResponse(map[string]any{"protocol": protocol, "revoked": true}).FPrint(w)
}

// RevealProtocolCredentials 在安全身份验证成功后解密并返回当前用户所有有效协议授权码。
func RevealProtocolCredentials(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.Password) == "" {
		writeSecurityError(w, response.ParamsError, "请输入当前账号密码进行安全验证")
		return
	}
	if _, err := auth.AuthenticateUserByID(ctx.UserID, input.Password); err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		writeSecurityError(w, response.ParamsError, "密码验证失败")
		return
	}
	codes, err := service.RevealProtocolCodes(ctx.UserID)
	if err != nil {
		writeSecurityError(w, response.ServerError, "解密授权码失败")
		return
	}
	response.NewSuccessResponse(codes).FPrint(w)
}
func RequestVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
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
	if input.Purpose == verification.PurposeAdminLocalReset {
		writeSecurityError(w, response.NoAccessPrivileges, "请使用本机部署重置通道")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(input.Account), strings.TrimSpace(ctx.UserAccount)) {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	if input.Purpose == verification.PurposePasswordChange {
		requestPasswordChangeVerification(ctx, w, req, input, runtime)
		return
	}
	if input.Email == "" || strings.TrimSpace(input.Account) == "" || !verification.IsSupportedPurpose(input.Purpose) || runtime.verification == nil {
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	challenge, err := runtime.verification.Issue(verification.IssueRequest{Purpose: input.Purpose, UserID: ctx.UserID, Account: input.Account, Email: input.Email}, remoteIP(req))
	if err != nil {
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	response.NewSuccessResponse(map[string]any{"id": challenge.ID, "expires_at": challenge.ExpiresAt.UTC()}).FPrint(w)
}

// VerifyVerification 消费一次性验证码，只返回后续流程所需的短时票据。
func VerifyVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input verificationCheckRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || runtime.verification == nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if input.Account == "" {
		input.Account = input.Identifier
	}
	if input.Purpose == verification.PurposeAdminLocalReset {
		writeSecurityError(w, response.NoAccessPrivileges, "请使用本机部署重置通道")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(input.Account), strings.TrimSpace(ctx.UserAccount)) {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if err := runtime.verification.Verify(input.ID, input.Account, input.Email, input.Code, input.Purpose, remoteIP(req)); err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"verified": true}).FPrint(w)
}

func BeginPasswordRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if rejectUnavailableRecovery(w) {
		return
	}
	var input verificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || runtime.recovery == nil {
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	beginKey := emailRecoveryBeginIPAttempts.ipKey(req)
	if emailRecoveryBeginIPAttempts.blocked(beginKey) {
		id, err := newOpaqueRecoveryChallengeID()
		if err != nil {
			writeSecurityError(w, response.ParamsError, "请求已受理")
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
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	id, err := runtime.recovery.Begin(recovery.BeginRequest{Identifier: input.Account, Email: input.Email, BrowserContext: browserContext}, remoteIP(req))
	if err != nil && id == "" {
		emailRecoveryFailureIPAttempts.recordFailure(emailRecoveryFailureIPAttempts.ipKey(req))
		writeSecurityError(w, response.ParamsError, "请求已受理")
		return
	}
	emailRecoveryBeginIPAttempts.recordFailure(beginKey)
	response.NewSuccessResponse(map[string]any{"id": id}).FPrint(w)
}

func VerifyPasswordRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if rejectUnavailableRecovery(w) {
		return
	}
	failureKey := emailRecoveryFailureIPAttempts.ipKey(req)
	if emailRecoveryFailureIPAttempts.blocked(failureKey) {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	var input verificationCheckRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || runtime.recovery == nil {
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
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if methodOffline {
		ticket, err = runtime.recovery.VerifyOfflineWithContext(input.Account, input.Question, input.Answer, input.RecoveryCode, remoteIP(req), browserContext)
	} else if strings.EqualFold(strings.TrimSpace(input.Method), "email_recovery_code") {
		ticket, err = runtime.recovery.ContinueEmailWithContext(input.Continuation, input.Account, input.Email, input.RecoveryCode, browserContext)
	} else {
		var result recovery.EmailVerificationResult
		result, err = runtime.recovery.VerifyEmailStepWithContext(input.ID, input.Account, input.Email, input.Code, remoteIP(req), browserContext)
		if err == nil {
			// 附加验证尚未完成，操作日志不能提前标记为身份验证成功。
			if observer, ok := w.(interface{ ObservePending(bool) }); ok {
				observer.ObservePending(result.RequiresRecoveryCode)
			}
			response.NewSuccessResponse(result).FPrint(w)
			return
		}
	}
	if err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"ticket": ticket}).FPrint(w)
}

func CompletePasswordRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if rejectUnavailableRecovery(w) {
		return
	}
	failureKey := emailRecoveryFailureIPAttempts.ipKey(req)
	if emailRecoveryFailureIPAttempts.blocked(failureKey) {
		writeSecurityError(w, response.ParamsError, "重置失败")
		return
	}
	var input recoveryCompleteRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || runtime.recovery == nil || strings.TrimSpace(input.Ticket) == "" {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "重置失败")
		return
	}
	browserContext, ok := recoveryBrowserContext(req, w, false)
	if !ok {
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if err := runtime.recovery.CompleteWithContext(input.Ticket, input.NewPassword, browserContext); err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		emailRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "重置失败")
		return
	}
	clearRecoveryBrowserContext(w, req)
	response.NewSuccessResponse(map[string]any{"reset": true}).FPrint(w)
}

// BeginPublicLinkPasswordRecovery 是 Web 域名 /rest/ 的无会话恢复入口。
// 所有申请结果统一返回 accepted，避免泄露账户、邮箱绑定和角色状态。
func BeginPublicLinkPasswordRecovery(w http.ResponseWriter, req *http.Request) {
	writePublicRecoveryJSON(w, http.StatusOK, map[string]bool{"accepted": true})
	if !PublicMailRecoveryEnabled() || req == nil {
		return
	}
	var input struct {
		Identifier string `json:"identifier"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return
	}
	runtime := currentSecurityRuntime()
	if runtime.recovery == nil {
		return
	}
	// 投递和账户状态异常均保持同样的受理响应，且恢复服务不记录令牌。
	_ = runtime.recovery.BeginPublicLink(input.Identifier)
}

// CompletePublicLinkPasswordRecovery 仅消费独立链接票据；不使用主站会话、CSRF 或恢复上下文。
func CompletePublicLinkPasswordRecovery(w http.ResponseWriter, req *http.Request) {
	if !PublicMailRecoveryEnabled() || req == nil {
		writePublicRecoveryJSON(w, http.StatusBadRequest, map[string]bool{"reset": false})
		return
	}
	var input struct {
		Token        string `json:"token"`
		NewPassword  string `json:"new_password"`
		RecoveryCode string `json:"recovery_code"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writePublicRecoveryJSON(w, http.StatusBadRequest, map[string]bool{"reset": false})
		return
	}
	runtime := currentSecurityRuntime()
	if runtime.recovery == nil {
		writePublicRecoveryJSON(w, http.StatusBadRequest, map[string]bool{"reset": false})
		return
	}
	if err := runtime.recovery.CompletePublicLink(input.Token, input.NewPassword, input.RecoveryCode); err != nil {
		if errors.Is(err, password.ErrBusy) {
			w.Header().Set("Retry-After", "1")
			writePublicRecoveryJSON(w, http.StatusServiceUnavailable, map[string]bool{"reset": false})
			return
		}
		writePublicRecoveryJSON(w, http.StatusBadRequest, map[string]bool{"reset": false})
		return
	}
	writePublicRecoveryJSON(w, http.StatusOK, map[string]bool{"reset": true})
}

// PublicLinkPasswordRecoveryRequirements 只返回恢复页渲染所需的最小信息。
// 一次性令牌不写入日志，也不返回账户、邮箱或角色名称。
func PublicLinkPasswordRecoveryRequirements(w http.ResponseWriter, req *http.Request) {
	if !PublicMailRecoveryEnabled() || req == nil {
		writePublicRecoveryJSON(w, http.StatusBadRequest, map[string]bool{"available": false})
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writePublicRecoveryJSON(w, http.StatusBadRequest, map[string]bool{"available": false})
		return
	}
	runtime := currentSecurityRuntime()
	available := false
	requiresRecoveryCode := false
	if runtime.recovery != nil {
		var err error
		requiresRecoveryCode, err = runtime.recovery.PublicLinkRequiresRecoveryCode(input.Token)
		if err == nil {
			available = true
		}
	}
	writePublicRecoveryJSON(w, http.StatusOK, map[string]bool{
		"available":              available,
		"requires_recovery_code": requiresRecoveryCode,
	})
}

func writePublicRecoveryJSON(w http.ResponseWriter, status int, value any) {
	if w == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
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
		Name: recoveryContextCookieName, Value: value, Path: "/api/",
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
		Name: recoveryContextCookieName, Value: "", Path: "/api/",
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
	runtime := currentSecurityRuntime()
	if ctx == nil || ctx.UserID <= 0 || runtime.external == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	items, err := runtime.external.List(ctx.UserID)
	if err != nil {
		log.WithError(err).Error("list external email failed")
		writeSecurityError(w, response.ServerError, "读取外部邮箱失败")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.Status == models.ExternalEmailStatusPending {
			continue
		}
		email, err := runtime.external.Decrypt(item)
		if err != nil {
			continue
		}
		result = append(result, map[string]any{"id": item.ID, "type": item.EmailType, "status": item.Status, "email": maskEmail(email), "created_at": item.CreatedAt, "updated_at": item.UpdatedAt})
	}
	response.NewSuccessResponse(result).FPrint(w)
}

func BindExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if ctx == nil || ctx.UserID <= 0 || runtime.external == nil || runtime.verification == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if !ctx.RecentAuth {
		writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "此操作需要最近十分钟内完成登录，请确认当前密码", string(access.CodeRecentAuthRequired))
		return
	}
	var input externalEmailRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	normalizedEmail, err := externalemail.NormalizeEmail(input.Email)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "请输入有效的外部邮箱地址")
		return
	}
	owned, err := currentUserPrimaryEmail(ctx.UserID, normalizedEmail)
	if err != nil {
		log.WithError(err).Error("check current user primary email failed")
		writeSecurityError(w, response.ServerError, "外部邮箱绑定失败")
		return
	}
	if owned {
		writeSecurityError(w, response.ParamsError, "不能将当前邮箱绑定为外部安全邮箱")
		return
	}
	input.Email = normalizedEmail
	item, err := runtime.external.Bind(externalemail.BindRequest{UserID: ctx.UserID, Email: input.Email, Type: input.Type})
	created := err == nil
	if errors.Is(err, externalemail.ErrBindingExists) {
		item, err = runtime.external.FindPending(ctx.UserID, input.Email, input.Type)
		if errors.Is(err, externalemail.ErrBindingNotFound) {
			writeSecurityError(w, response.ParamsError, "该外部邮箱已经绑定")
			return
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, externalemail.ErrPrimaryLimit):
			writeSecurityError(w, response.ParamsError, "主要外部邮箱已存在，请先停用后再绑定")
		case errors.Is(err, externalemail.ErrBackupLimit):
			writeSecurityError(w, response.ParamsError, "备用外部邮箱已存在，请先停用后再绑定")
		default:
			writeSecurityError(w, response.ParamsError, "外部邮箱绑定失败")
		}
		return
	}
	challenge, err := runtime.verification.Issue(verification.IssueRequest{Purpose: verification.PurposeExternalEmailBind, UserID: ctx.UserID, Account: ctx.UserAccount, Email: input.Email, ResourceID: item.ID}, remoteIP(req))
	if err != nil {
		log.WithError(err).Warn("issue external email verification challenge failed")
		if created {
			_ = runtime.external.AbandonPending(ctx.UserID, item.ID)
		}
		writeSecurityError(w, response.ParamsError, externalEmailVerificationIssueMessage(err))
		return
	}
	response.NewSuccessResponse(map[string]any{"id": item.ID, "challenge_id": challenge.ID, "status": item.Status, "email": maskEmail(input.Email), "expires_at": challenge.ExpiresAt.UTC()}).FPrint(w)
}

// ChangeOwnExternalEmail 发送当前用户外部邮箱变更验证码，旧邮箱会保留到验证成功。
func ChangeOwnExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if ctx == nil || ctx.UserID <= 0 || runtime.external == nil || runtime.verification == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	if !ctx.RecentAuth {
		writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "此操作需要最近十分钟内完成登录，请确认当前密码", string(access.CodeRecentAuthRequired))
		return
	}
	var input externalEmailRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	normalizedEmail, err := externalemail.NormalizeEmail(input.Email)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "请输入有效的外部邮箱地址")
		return
	}
	owned, err := currentUserPrimaryEmail(ctx.UserID, normalizedEmail)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("check current user primary email failed")
		writeSecurityError(w, response.ServerError, "外部邮箱变更失败")
		return
	}
	if owned {
		writeSecurityError(w, response.ParamsError, "不能将当前邮箱绑定为外部安全邮箱")
		return
	}
	emailType := strings.TrimSpace(input.Type)
	if emailType == "" {
		emailType = models.ExternalEmailTypePrimary
	}
	item, err := runtime.external.BeginReplacement(externalemail.BindRequest{
		UserID: ctx.UserID,
		Email:  normalizedEmail,
		Type:   emailType,
	})
	if err != nil {
		if errors.Is(err, externalemail.ErrBindingExists) {
			writeSecurityError(w, response.ParamsError, "该外部邮箱已经绑定或正在被其他账号使用")
			return
		}
		log.WithContext(ctx).WithError(err).Error("begin own external email replacement failed")
		writeSecurityError(w, response.ParamsError, "外部邮箱变更失败")
		return
	}
	if runtime.verification.Store != nil {
		runtime.verification.Store.InvalidateResource(item.ID, verification.PurposeExternalEmailChange)
		runtime.verification.Store.InvalidateResource(item.ID, verification.PurposeExternalEmailBind)
	}
	challenge, err := runtime.verification.Issue(verification.IssueRequest{
		Purpose:    verification.PurposeExternalEmailChange,
		UserID:     ctx.UserID,
		Account:    ctx.UserAccount,
		Email:      normalizedEmail,
		ResourceID: item.ID,
	}, remoteIP(req))
	if err != nil {
		log.WithContext(ctx).WithError(err).Warn("issue own external email verification failed")
		writeSecurityError(w, response.ParamsError, externalEmailVerificationIssueMessage(err))
		return
	}
	response.NewSuccessResponse(map[string]any{
		"binding_id": item.ID, "challenge_id": challenge.ID,
		"email": maskEmail(normalizedEmail), "expires_at": challenge.ExpiresAt.UTC(),
	}).FPrint(w)
}

func currentUserPrimaryEmail(userID int, email string) (bool, error) {
	if db.Instance == nil || userID <= 0 {
		return false, errors.New("database is unavailable")
	}
	normalizedEmail, err := externalemail.NormalizeEmail(email)
	if err != nil {
		return false, err
	}
	var binding models.UserEmail
	owned, err := db.Instance.Where("user_id = ? AND verified_at IS NOT NULL AND status != ? AND is_primary = 1 AND (address_canonical = ? OR LOWER(address) = ?)", userID, 3, normalizedEmail, normalizedEmail).Limit(1).Get(&binding)
	if err != nil || owned {
		return owned, err
	}

	// 本地收信按账号和域名路由，新建账号不一定存在独立的主邮箱绑定记录。
	parts := strings.SplitN(normalizedEmail, "@", 2)
	domains := config.CurrentMailDomains()
	localDomain := strings.EqualFold(parts[1], domains.Primary)
	for _, domain := range domains.Accepted {
		if strings.EqualFold(parts[1], domain) {
			localDomain = true
			break
		}
	}
	if !localDomain {
		return false, nil
	}
	var user models.User
	found, err := db.Instance.ID(userID).Get(&user)
	if err != nil {
		return false, err
	}
	if !found {
		return false, errors.New("user is unavailable")
	}
	return strings.EqualFold(parts[0], user.Account), nil
}

func VerifyExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if ctx == nil || ctx.UserID <= 0 || runtime.external == nil || runtime.verification == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input verificationCheckRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.BindingID <= 0 {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	// 确认时重新检查归属，拒绝修复前创建或域名配置变化后的自身邮箱挑战。
	owned, err := currentUserPrimaryEmail(ctx.UserID, input.Email)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if owned {
		writeSecurityError(w, response.ParamsError, "不能将当前邮箱绑定为外部安全邮箱")
		return
	}
	if err := runtime.verification.VerifyBound(input.ID, ctx.UserAccount, input.Email, input.Code, verification.PurposeExternalEmailBind, input.BindingID, remoteIP(req)); err != nil {
		writeSecurityError(w, response.ParamsError, externalEmailVerificationCheckMessage(err))
		return
	}
	if err := runtime.external.Verify(ctx.UserID, input.BindingID); err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"verified": true}).FPrint(w)
}

// VerifyOwnExternalEmail 验证成功后原子替换当前用户同用途的旧邮箱。
func VerifyOwnExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if ctx == nil || ctx.UserID <= 0 || runtime.external == nil || runtime.verification == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input verificationCheckRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.BindingID <= 0 {
		writeSecurityError(w, response.ParamsError, "验证请求无效")
		return
	}
	normalizedEmail, err := externalemail.NormalizeEmail(input.Email)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	owned, err := currentUserPrimaryEmail(ctx.UserID, normalizedEmail)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if owned {
		writeSecurityError(w, response.ParamsError, "不能将当前邮箱绑定为外部安全邮箱")
		return
	}
	if err := runtime.verification.VerifyBound(
		input.ID,
		ctx.UserAccount,
		normalizedEmail,
		input.Code,
		verification.PurposeExternalEmailChange,
		input.BindingID,
		remoteIP(req),
	); err != nil {
		writeSecurityError(w, response.ParamsError, externalEmailVerificationCheckMessage(err))
		return
	}
	if err := runtime.external.VerifyReplacement(ctx.UserID, input.BindingID); err != nil {
		log.WithContext(ctx).WithError(err).Error("verify own external email replacement failed")
		writeSecurityError(w, response.ParamsError, "外部邮箱替换失败")
		return
	}
	InvalidateSecurityRuntimeUser(ctx.UserID, ctx.UserAccount)
	response.NewSuccessResponse(map[string]any{"verified": true}).FPrint(w)
}

func DisableExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	if ctx == nil || ctx.UserID <= 0 || runtime.external == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	var input externalEmailRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ID <= 0 {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	if err := runtime.external.Disable(ctx.UserID, input.ID); err != nil {
		writeSecurityError(w, response.ParamsError, "外部邮箱停用失败")
		return
	}
	if runtime.verification != nil && runtime.verification.Store != nil {
		runtime.verification.Store.InvalidateResource(input.ID, verification.PurposeExternalEmailBind)
		runtime.verification.Store.InvalidateResource(input.ID, verification.PurposeExternalEmailChange)
		runtime.verification.Store.InvalidateAccount(ctx.UserAccount)
	}
	if runtime.recovery != nil {
		runtime.recovery.InvalidateUser(ctx.UserID)
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
	return clientip.Resolve(req, config.CurrentHTTPConfig().TrustedProxyCIDRs)
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

func writeSecurityErrorWithCode(w http.ResponseWriter, errorNo int, message, code string) {
	response.NewErrorResponse(errorNo, message, response.ConfigErrorData{Code: code}).FPrint(w)
}

func externalEmailVerificationIssueMessage(err error) string {
	switch {
	case errors.Is(err, verification.ErrRateLimited):
		return "验证码发送过于频繁，请稍后再试"
	case errors.Is(err, verification.ErrSMTPRelayUnavailable),
		errors.Is(err, verification.ErrInvalidSMTPConfig),
		errors.Is(err, verification.ErrUnsupportedSMTP):
		return "SMTP 中继配置不完整，请完善配置或清空中继后使用项目自身投递"
	case errors.Is(err, verification.ErrProjectMailUnavailable):
		return "项目邮件投递尚未就绪，请检查主域名和 DKIM 配置"
	case errors.Is(err, verification.ErrMailDeliveryFailed):
		return "验证码邮件投递失败，请确认收件地址和邮件服务状态"
	default:
		return "验证码发送失败，请检查发信服务后重试"
	}
}

func externalEmailVerificationCheckMessage(err error) string {
	switch {
	case errors.Is(err, verification.ErrExpiredChallenge):
		return "验证码已过期，请重新发送"
	case errors.Is(err, verification.ErrTooManyAttempts):
		return "验证码错误次数过多，请重新发送"
	default:
		return "验证码错误或验证请求已失效"
	}
}
