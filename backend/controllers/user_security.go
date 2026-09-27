package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/accountgroup"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

type managedUserSecurityRequest struct {
	UserID      int    `json:"user_id"`
	Count       int    `json:"count"`
	Email       string `json:"email"`
	Type        string `json:"type"`
	ChallengeID string `json:"challenge_id"`
	BindingID   int    `json:"binding_id"`
	Code        string `json:"code"`
}

func loadManagedSecurityUser(ctx *context.Context, w http.ResponseWriter, userID int) (*models.User, bool) {
	if !requireAccountScope(ctx, w, userID) {
		return nil, false
	}
	if ctx == nil || ctx.UserID <= 0 {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return nil, false
	}
	if db.Instance == nil || userID <= 0 {
		writeSecurityError(w, response.ParamsError, "目标用户无效")
		return nil, false
	}
	var target models.User
	found, err := db.Instance.ID(userID).Get(&target)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("load managed security user failed")
		writeSecurityError(w, response.ServerError, "读取目标用户失败")
		return nil, false
	}
	if !found || target.ID <= 0 {
		writeSecurityError(w, response.ParamsError, "目标用户不存在")
		return nil, false
	}
	if err := authorizeCapability(ctx, access.CapabilityUserSecurityManage, target.EffectiveRole(), false); err != nil {
		if access.IsDenied(err, access.CodeRecentAuthRequired) {
			writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "此操作需要最近十分钟内完成登录，请确认当前密码", string(access.CodeRecentAuthRequired))
			return nil, false
		}
		if access.IsDenied(err, access.CodeRecoveryCodeRequired) {
			writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "请先为当前管理员配置有效安全码", string(access.CodeRecoveryCodeRequired))
			return nil, false
		}
		writeSecurityError(w, response.NoAccessPrivileges, "无权管理该用户的安全信息")
		return nil, false
	}
	return &target, true
}

func writeManagedScopeError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, accountgroup.ErrDenied) || errors.Is(err, accountgroup.ErrConflict) {
		writeAccountGroupError(w, err)
		return true
	}
	return false
}

func managedUserIDFromQuery(req *http.Request) int {
	if req == nil || req.URL == nil {
		return 0
	}
	userID, _ := strconv.Atoi(strings.TrimSpace(req.URL.Query().Get("id")))
	return userID
}

// GetManagedRecoveryCodeStatus 返回目标用户安全码状态，不返回任何明文或哈希。
func GetManagedRecoveryCodeStatus(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	target, ok := loadManagedSecurityUser(ctx, w, managedUserIDFromQuery(req))
	if !ok {
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	configured, err := service.HasValidRecoveryCode(target.ID)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("load managed recovery code status failed")
		writeSecurityError(w, response.ServerError, "读取安全码状态失败")
		return
	}
	response.NewSuccessResponse(map[string]any{
		"user_id":    target.ID,
		"account":    target.Account,
		"configured": configured,
	}).FPrint(w)
}

// RevealManagedRecoveryCodes 在管理员完成近期安全验证后返回目标用户有效安全码。
func RevealManagedRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input managedUserSecurityRequest
	if err := decodeUserRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	target, ok := loadManagedSecurityUser(ctx, w, input.UserID)
	if !ok {
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	codes, err := service.RevealRecoveryCodes(target.ID)
	if err != nil {
		if errors.Is(err, credentials.ErrNoCiphertext) {
			writeSecurityError(w, response.ParamsError, "现有安全码无法重新查看，请重新生成安全码")
			return
		}
		if errors.Is(err, credentials.ErrCodeNotFound) {
			writeSecurityError(w, response.ParamsError, "当前没有有效安全码")
			return
		}
		log.WithContext(ctx).WithError(err).Error("reveal managed recovery codes failed")
		writeSecurityError(w, response.ServerError, "读取安全码失败")
		return
	}
	response.NewSuccessResponse(map[string]any{"user_id": target.ID, "codes": codes, "revealed": true}).FPrint(w)
}

// GenerateManagedRecoveryCodes 为目标用户生成安全码，有管理权限的管理员可在安全验证后再次查看有效安全码。
func GenerateManagedRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input managedUserSecurityRequest
	if err := decodeUserRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	target, ok := loadManagedSecurityUser(ctx, w, input.UserID)
	if !ok {
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	codes, err := service.GenerateRecoveryCodesContext(recoveryRequestContext(req), target.ID, input.Count, accountWriteGuard(ctx, *target))
	if err != nil {
		if writeManagedScopeError(w, err) {
			return
		}
		if writePasswordBusy(w, err) {
			return
		}
		log.WithContext(ctx).WithError(err).Error("generate managed recovery codes failed")
		writeSecurityError(w, response.ParamsError, "安全码生成失败")
		return
	}
	InvalidateSecurityRuntimeUser(target.ID, target.Account)
	response.NewSuccessResponse(map[string]any{
		"user_id":    target.ID,
		"codes":      codes,
		"shown_once": true,
	}).FPrint(w)
}

// RevokeManagedRecoveryCodes 撤销目标用户现有的全部安全码。
func RevokeManagedRecoveryCodes(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input managedUserSecurityRequest
	if err := decodeUserRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	target, ok := loadManagedSecurityUser(ctx, w, input.UserID)
	if !ok {
		return
	}
	service := credentialRuntime()
	if service == nil {
		writeSecurityError(w, response.ServerError, "安全凭据服务不可用")
		return
	}
	if err := service.RevokeRecoveryCodes(target.ID, accountWriteGuard(ctx, *target)); err != nil {
		if writeManagedScopeError(w, err) {
			return
		}
		log.WithContext(ctx).WithError(err).Error("revoke managed recovery codes failed")
		writeSecurityError(w, response.ServerError, "撤销安全码失败")
		return
	}
	InvalidateSecurityRuntimeUser(target.ID, target.Account)
	response.NewSuccessResponse(map[string]any{"user_id": target.ID, "revoked": true}).FPrint(w)
}

// ListManagedExternalEmails 返回目标用户已验证的外部邮箱掩码。
func ListManagedExternalEmails(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	target, ok := loadManagedSecurityUser(ctx, w, managedUserIDFromQuery(req))
	if !ok {
		return
	}
	runtime := currentSecurityRuntime()
	if runtime.external == nil {
		writeSecurityError(w, response.ServerError, "外部邮箱服务不可用")
		return
	}
	items, err := runtime.external.List(target.ID)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("list managed external emails failed")
		writeSecurityError(w, response.ServerError, "读取外部邮箱失败")
		return
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if item.Status != models.ExternalEmailStatusVerified {
			continue
		}
		email, decryptErr := runtime.external.Decrypt(item)
		if decryptErr != nil {
			continue
		}
		result = append(result, map[string]any{
			"id": item.ID, "type": item.EmailType, "status": item.Status, "email": maskEmail(email),
		})
	}
	response.NewSuccessResponse(map[string]any{
		"user_id": target.ID,
		"account": target.Account,
		"items":   result,
	}).FPrint(w)
}

// ChangeManagedExternalEmail 发送目标用户外部邮箱变更验证码。
func ChangeManagedExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input managedUserSecurityRequest
	if err := decodeUserRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "请求无效")
		return
	}
	target, ok := loadManagedSecurityUser(ctx, w, input.UserID)
	if !ok {
		return
	}
	runtime := currentSecurityRuntime()
	if runtime.external == nil || runtime.verification == nil {
		writeSecurityError(w, response.ServerError, "外部邮箱验证服务不可用")
		return
	}
	normalizedEmail, err := externalemail.NormalizeEmail(input.Email)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "请输入有效的外部邮箱地址")
		return
	}
	owned, err := currentUserPrimaryEmail(target.ID, normalizedEmail)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("check managed user primary email failed")
		writeSecurityError(w, response.ServerError, "外部邮箱变更失败")
		return
	}
	if owned {
		writeSecurityError(w, response.ParamsError, "不能将该用户自己的邮箱绑定为外部安全邮箱")
		return
	}
	emailType := strings.TrimSpace(input.Type)
	if emailType == "" {
		emailType = models.ExternalEmailTypePrimary
	}
	item, err := runtime.external.BeginReplacement(externalemail.BindRequest{
		UserID: target.ID,
		Email:  normalizedEmail,
		Type:   emailType,
	}, accountWriteGuard(ctx, *target))
	if err != nil {
		if writeManagedScopeError(w, err) {
			return
		}
		if errors.Is(err, externalemail.ErrBindingExists) {
			writeSecurityError(w, response.ParamsError, "该外部邮箱已经绑定或正在被其他账号使用")
			return
		}
		log.WithContext(ctx).WithError(err).Error("begin managed external email replacement failed")
		writeSecurityError(w, response.ParamsError, "外部邮箱变更失败")
		return
	}
	if runtime.verification.Store != nil {
		runtime.verification.Store.InvalidateResource(item.ID, verification.PurposeExternalEmailChange)
		runtime.verification.Store.InvalidateResource(item.ID, verification.PurposeExternalEmailBind)
	}
	challenge, err := runtime.verification.Issue(verification.IssueRequest{
		Purpose:    verification.PurposeExternalEmailChange,
		UserID:     target.ID,
		Account:    target.Account,
		Email:      normalizedEmail,
		ResourceID: item.ID,
	}, remoteIP(req))
	if err != nil {
		log.WithContext(ctx).WithError(err).Warn("issue managed external email verification failed")
		writeSecurityError(w, response.ParamsError, externalEmailVerificationIssueMessage(err))
		return
	}
	response.NewSuccessResponse(map[string]any{
		"user_id": target.ID, "binding_id": item.ID, "challenge_id": challenge.ID,
		"email": maskEmail(normalizedEmail), "expires_at": challenge.ExpiresAt.UTC(),
	}).FPrint(w)
}

// VerifyManagedExternalEmail 验证成功后原子替换目标用户同用途的旧邮箱。
func VerifyManagedExternalEmail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input managedUserSecurityRequest
	if err := decodeUserRequest(w, req, &input); err != nil || input.BindingID <= 0 {
		writeSecurityError(w, response.ParamsError, "验证请求无效")
		return
	}
	target, ok := loadManagedSecurityUser(ctx, w, input.UserID)
	if !ok {
		return
	}
	runtime := currentSecurityRuntime()
	if runtime.external == nil || runtime.verification == nil {
		writeSecurityError(w, response.ServerError, "外部邮箱验证服务不可用")
		return
	}
	normalizedEmail, err := externalemail.NormalizeEmail(input.Email)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	owned, err := currentUserPrimaryEmail(target.ID, normalizedEmail)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if owned {
		writeSecurityError(w, response.ParamsError, "不能将当前邮箱绑定为外部安全邮箱")
		return
	}
	if err := runtime.verification.VerifyBound(
		input.ChallengeID,
		target.Account,
		normalizedEmail,
		input.Code,
		verification.PurposeExternalEmailChange,
		input.BindingID,
		remoteIP(req),
	); err != nil {
		writeSecurityError(w, response.ParamsError, externalEmailVerificationCheckMessage(err))
		return
	}
	if err := runtime.external.VerifyReplacement(target.ID, input.BindingID, accountWriteGuard(ctx, *target)); err != nil {
		if writeManagedScopeError(w, err) {
			return
		}
		log.WithContext(ctx).WithError(err).Error("verify managed external email replacement failed")
		writeSecurityError(w, response.ParamsError, "外部邮箱替换失败")
		return
	}
	InvalidateSecurityRuntimeUser(target.ID, target.Account)
	response.NewSuccessResponse(map[string]any{"user_id": target.ID, "verified": true}).FPrint(w)
}
