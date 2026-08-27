package controllers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	"git.pcux.cn/xuans/Smail/utils/context"
)

type webAuthnFinishRequest struct {
	SessionID   string          `json:"session_id"`
	Identifier  string          `json:"identifier"`
	DisplayName string          `json:"display_name"`
	Credential  json.RawMessage `json:"credential"`
}

type webAuthnLoginBeginRequest struct {
	Identifier string `json:"identifier"`
}

type webAuthnCredentialRequest struct {
	ID          int    `json:"id"`
	DisplayName string `json:"display_name"`
}

type webAuthnRecoveryBeginRequest struct {
	Identifier string `json:"identifier"`
}

type webAuthnRecoveryVerifyRequest struct {
	Identifier string          `json:"identifier"`
	SessionID  string          `json:"session_id"`
	Credential json.RawMessage `json:"credential"`
}

const (
	publicWebAuthnBeginLimit   = 30
	publicWebAuthnFailureLimit = 20
	publicRecoveryBeginLimit   = 30
	publicRecoveryFailureLimit = 20
)

var (
	webAuthnLoginBeginIPAttempts      = newLoginLimiter(publicWebAuthnBeginLimit)
	webAuthnLoginFailureIPAttempts    = newLoginLimiter(publicWebAuthnFailureLimit)
	webAuthnRecoveryBeginIPAttempts   = newLoginLimiter(publicRecoveryBeginLimit)
	webAuthnRecoveryFailureIPAttempts = newLoginLimiter(publicRecoveryFailureLimit)
	emailRecoveryBeginIPAttempts      = newLoginLimiter(publicRecoveryBeginLimit)
	emailRecoveryFailureIPAttempts    = newLoginLimiter(publicRecoveryFailureLimit)
)

func webAuthnRuntime() *webauthnservice.Service {
	return securityRuntime.webauthn
}

// BeginWebAuthnRegistration 开始当前用户的安全密钥注册流程。
func BeginWebAuthnRegistration(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	service := webAuthnRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "安全密钥功能未启用")
		return
	}
	options, sessionID, err := service.BeginRegistration(ctx.UserID)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "无法开始安全密钥注册")
		return
	}
	response.NewSuccessResponse(map[string]any{"session_id": sessionID, "options": options}).FPrint(w)
}

// FinishWebAuthnRegistration 完成注册并只返回凭据索引信息。
func FinishWebAuthnRegistration(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	service := webAuthnRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "安全密钥功能未启用")
		return
	}
	var input webAuthnFinishRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.SessionID) == "" || len(input.Credential) == 0 || !json.Valid(input.Credential) {
		writeSecurityError(w, response.ParamsError, "安全密钥注册请求无效")
		return
	}
	credentialRequest := cloneWebAuthnRequest(req, input.Credential)
	item, err := service.FinishRegistration(ctx.UserID, input.SessionID, credentialRequest, input.DisplayName)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "安全密钥注册失败")
		return
	}
	response.NewSuccessResponse(webAuthnCredentialView(item)).FPrint(w)
}

// ListWebAuthnCredentials 返回当前用户的安全密钥索引，不返回公钥材料。
func ListWebAuthnCredentials(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	service := webAuthnRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "安全密钥功能未启用")
		return
	}
	items, err := service.ListCredentials(ctx.UserID)
	if err != nil {
		writeSecurityError(w, response.ServerError, "读取安全密钥失败")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for index := range items {
		result = append(result, webAuthnCredentialView(&items[index]))
	}
	response.NewSuccessResponse(result).FPrint(w)
}

// RenameWebAuthnCredential 修改当前用户的安全密钥名称。
func RenameWebAuthnCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	service := webAuthnRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "安全密钥功能未启用")
		return
	}
	var input webAuthnCredentialRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ID <= 0 {
		writeSecurityError(w, response.ParamsError, "安全密钥请求无效")
		return
	}
	if err := service.RenameCredential(ctx.UserID, input.ID, input.DisplayName); err != nil {
		writeSecurityError(w, response.ParamsError, "安全密钥名称修改失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"updated": true}).FPrint(w)
}

// RevokeWebAuthnCredential 撤销当前用户的安全密钥。
func RevokeWebAuthnCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	service := webAuthnRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "安全密钥功能未启用")
		return
	}
	var input webAuthnCredentialRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ID <= 0 {
		writeSecurityError(w, response.ParamsError, "安全密钥请求无效")
		return
	}
	if err := service.RevokeCredential(ctx.UserID, input.ID); err != nil {
		writeSecurityError(w, response.ParamsError, "安全密钥撤销失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"revoked": true}).FPrint(w)
}

// BeginWebAuthnLogin 开始公开的安全密钥登录流程。
func BeginWebAuthnLogin(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service := webAuthnRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u4e0d\u53ef\u7528")
		return
	}
	beginKey := webAuthnLoginBeginIPAttempts.ipKey(req)
	failureKey := webAuthnLoginFailureIPAttempts.ipKey(req)
	if webAuthnLoginBeginIPAttempts.blocked(beginKey) || webAuthnLoginFailureIPAttempts.blocked(failureKey) || loginIPAttempts.blocked(loginIPAttempts.ipKey(req)) {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u5931\u8d25")
		return
	}
	var input webAuthnLoginBeginRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		webAuthnLoginFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u8bf7\u6c42\u65e0\u6548")
		return
	}
	options, sessionID, err := service.BeginDiscoverableLogin()
	if err != nil {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u5931\u8d25")
		return
	}
	webAuthnLoginBeginIPAttempts.recordFailure(beginKey)
	response.NewSuccessResponse(map[string]any{"session_id": sessionID, "options": options}).FPrint(w)
}

// FinishWebAuthnLogin 完成安全密钥登录并创建普通业务会话。
func FinishWebAuthnLogin(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service := webAuthnRuntime()
	if service == nil {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u4e0d\u53ef\u7528")
		return
	}
	failureKey := webAuthnLoginFailureIPAttempts.ipKey(req)
	if webAuthnLoginFailureIPAttempts.blocked(failureKey) || loginIPAttempts.blocked(loginIPAttempts.ipKey(req)) {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u5931\u8d25")
		return
	}
	var input webAuthnFinishRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.SessionID) == "" || len(input.Credential) == 0 || !json.Valid(input.Credential) {
		webAuthnLoginFailureIPAttempts.recordFailure(failureKey)
		loginIPAttempts.recordFailure(loginIPAttempts.ipKey(req))
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u8bf7\u6c42\u65e0\u6548")
		return
	}
	credentialRequest := cloneWebAuthnRequest(req, input.Credential)
	user, _, err := service.FinishDiscoverableLogin(input.SessionID, credentialRequest)
	if err != nil || user == nil {
		webAuthnLoginFailureIPAttempts.recordFailure(failureKey)
		loginIPAttempts.recordFailure(loginIPAttempts.ipKey(req))
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u767b\u5f55\u5931\u8d25")
		return
	}
	completeLoginSession(ctx, w, req, user)
}

func BeginWebAuthnRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if rejectUnavailableRecovery(w) {
		return
	}
	service := securityRuntime.recovery
	if service == nil {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u6062\u590d\u4e0d\u53ef\u7528")
		return
	}
	beginKey := webAuthnRecoveryBeginIPAttempts.ipKey(req)
	failureKey := webAuthnRecoveryFailureIPAttempts.ipKey(req)
	if webAuthnRecoveryBeginIPAttempts.blocked(beginKey) || webAuthnRecoveryFailureIPAttempts.blocked(failureKey) {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	var input webAuthnRecoveryBeginRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.Identifier) == "" {
		webAuthnRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u6062\u590d\u8bf7\u6c42\u65e0\u6548")
		return
	}
	if _, ok := recoveryBrowserContext(req, w, true); !ok {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u6062\u590d\u8bf7\u6c42\u65e0\u6548")
		return
	}
	options, sessionID, err := service.BeginWebAuthn(input.Identifier)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	webAuthnRecoveryBeginIPAttempts.recordFailure(beginKey)
	response.NewSuccessResponse(map[string]any{"session_id": sessionID, "options": options}).FPrint(w)
}

func VerifyWebAuthnRecovery(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if rejectUnavailableRecovery(w) {
		return
	}
	service := securityRuntime.recovery
	if service == nil {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u6062\u590d\u4e0d\u53ef\u7528")
		return
	}
	failureKey := webAuthnRecoveryFailureIPAttempts.ipKey(req)
	if webAuthnRecoveryFailureIPAttempts.blocked(failureKey) {
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	var input webAuthnRecoveryVerifyRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.Identifier) == "" || strings.TrimSpace(input.SessionID) == "" || len(input.Credential) == 0 || !json.Valid(input.Credential) {
		webAuthnRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u6062\u590d\u8bf7\u6c42\u65e0\u6548")
		return
	}
	browserContext, ok := recoveryBrowserContext(req, w, false)
	if !ok {
		webAuthnRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	credentialRequest := cloneWebAuthnRequest(req, input.Credential)
	ticket, err := service.VerifyWebAuthnWithContext(input.Identifier, input.SessionID, credentialRequest, browserContext)
	if err != nil {
		webAuthnRecoveryFailureIPAttempts.recordFailure(failureKey)
		writeSecurityError(w, response.ParamsError, "\u5b89\u5168\u5bc6\u94a5\u9a8c\u8bc1\u5931\u8d25")
		return
	}
	response.NewSuccessResponse(map[string]any{"ticket": ticket}).FPrint(w)
}

func cloneWebAuthnRequest(source *http.Request, payload []byte) *http.Request {
	clone := source.Clone(source.Context())
	clone.Body = io.NopCloser(bytes.NewReader(payload))
	clone.ContentLength = int64(len(payload))
	clone.Header = source.Header.Clone()
	clone.Header.Set("Content-Type", "application/json")
	return clone
}

func webAuthnCredentialView(item *models.WebAuthnCredential) map[string]any {
	if item == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                       item.ID,
		"display_name":             item.DisplayName,
		"discoverable":             item.Discoverable,
		"requires_re_registration": !item.Discoverable && item.RevokedAt == nil,
		"created_at":               item.CreatedAt,
		"last_used_at":             item.LastUsedAt,
		"revoked_at":               item.RevokedAt,
	}
}
