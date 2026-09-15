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
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/context"
)

var errPasswordChangeVerification = errors.New("password change verification unavailable")

// 从数据库读取当前身份和主要安全邮箱，不允许客户端指定验证码接收人。
func passwordChangeIdentity(ctx *context.Context, runtime securityRuntimeState) (*models.User, *models.ExternalEmail, string, error) {
	if ctx == nil || db.Instance == nil || runtime.external == nil || runtime.verification == nil {
		return nil, nil, "", errPasswordChangeVerification
	}
	user := &models.User{}
	found, err := db.Instance.ID(ctx.UserID).Get(user)
	if err != nil || !found || user.Disabled != 0 || user.EffectiveRole() != access.RoleSuperAdmin || user.Account != ctx.UserAccount {
		return nil, nil, "", errPasswordChangeVerification
	}
	binding, err := runtime.external.FindVerifiedPrimaryByUser(user.ID)
	if err != nil {
		return nil, nil, "", errPasswordChangeVerification
	}
	email, err := runtime.external.Decrypt(*binding)
	if err != nil || strings.TrimSpace(email) == "" {
		return nil, nil, "", errPasswordChangeVerification
	}
	return user, binding, email, nil
}

func requestPasswordChangeVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request, input verificationRequest, runtime securityRuntimeState) {
	user, binding, email, err := passwordChangeIdentity(ctx, runtime)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "请确认超级管理员已绑定并验证主要外部安全邮箱")
		return
	}
	if input.Email != "" && !strings.EqualFold(strings.TrimSpace(input.Email), email) {
		writeSecurityError(w, response.ParamsError, "邮箱地址与绑定的主安全邮箱不匹配")
		return
	}
	challenge, err := runtime.verification.Issue(verification.IssueRequest{
		Purpose: verification.PurposePasswordChange, UserID: user.ID,
		Account: user.Account, Email: email, ResourceID: binding.ID,
		BrowserTransaction: strconv.Itoa(user.SecurityVersion),
	}, remoteIP(req))
	if err != nil {
		writeSecurityError(w, response.ParamsError, "验证码发送失败或请求过于频繁，请稍后重试")
		return
	}
	response.NewSuccessResponse(map[string]any{
		"id": challenge.ID, "expires_at": challenge.ExpiresAt.UTC(), "email": maskEmail(email),
	}).FPrint(w)
}

func verifyPasswordChange(ctx *context.Context, req *http.Request, input modifyPasswordRequest) (*models.User, error) {
	runtime := currentSecurityRuntime()
	user, binding, email, err := passwordChangeIdentity(ctx, runtime)
	if err != nil || strings.TrimSpace(input.Code) == "" || strings.TrimSpace(input.ChallengeID) == "" {
		return nil, errPasswordChangeVerification
	}
	if input.Email != "" && !strings.EqualFold(strings.TrimSpace(input.Email), email) {
		return nil, errPasswordChangeVerification
	}
	// 验证码同时绑定用途、账号、邮箱绑定记录及安全版本，不能跨流程复用。
	if err := runtime.verification.VerifyBoundWithContext(input.ChallengeID, user.Account, email,
		strings.TrimSpace(input.Code), verification.PurposePasswordChange, binding.ID,
		remoteIP(req), strconv.Itoa(user.SecurityVersion)); err != nil {
		return nil, err
	}
	return user, nil
}
