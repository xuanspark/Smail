package controllers

import (
	"errors"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	"git.pcux.cn/xuans/Smail/utils/password"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
	"time"
)

type modifyPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	Password        string `json:"password"`
}

func ModifyPassword(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || strings.TrimSpace(ctx.UserAccount) == "" {
		response.NewErrorResponse(response.NoAccessPrivileges, "未登录用户不能修改密码", "").FPrint(w)
		return
	}
	if ctx.EffectiveRole() == access.RoleSuperAdmin {
		response.NewErrorResponse(response.NoAccessPrivileges, "超级管理员必须通过部署端专用通道重置密码", "").FPrint(w)
		return
	}
	var retData modifyPasswordRequest
	if err := decodeJSONRequest(w, req, &retData); err != nil {
		log.WithContext(ctx).Debug("invalid password change request")
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	if retData.CurrentPassword == "" || retData.Password == "" {
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	pairKey := loginPairAttempts.key(req, ctx.UserAccount)
	accountKey := loginAccountAttempts.accountKey(ctx.UserAccount)
	ipKey := loginIPAttempts.ipKey(req)
	if loginPairAttempts.blocked(pairKey) || loginAccountAttempts.blocked(accountKey) || loginIPAttempts.blocked(ipKey) {
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "aperror"), "").FPrint(w)
		return
	}
	currentUser, err := auth.Authenticate(ctx.UserAccount, retData.CurrentPassword)
	if err != nil || currentUser.ID != ctx.UserID {
		loginPairAttempts.recordFailure(pairKey)
		loginAccountAttempts.recordFailure(accountKey)
		loginIPAttempts.recordFailure(ipKey)
		log.WithContext(ctx).Info("password change authentication failed")
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "aperror"), "").FPrint(w)
		return
	}
	loginPairAttempts.clear(pairKey)
	loginAccountAttempts.clear(accountKey)

	securityVersion := currentUser.SecurityVersion + 1
	if securityVersion <= 0 {
		securityVersion = 1
	}
	now := time.Now().UTC()
	user := models.User{
		ID:                 ctx.UserID,
		SessionVersion:     currentUser.SessionVersion + 1,
		SecurityVersion:    securityVersion,
		MustChangePassword: 0,
		PasswordChangedAt:  &now,
	}
	if err := auth.SetPassword(&user, retData.Password); err != nil {
		if errors.Is(err, password.ErrInvalidLength) {
			response.NewErrorResponse(response.ParamsError, "密码长度必须为 8-12 字节", "").FPrint(w)
			return
		}
		log.WithContext(ctx).WithError(err).Error("password hash failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	credentialService := credentialRuntime()
	if credentialService == nil {
		log.WithContext(ctx).Error("credential service unavailable during password change")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	tx := db.Instance.NewSession()
	if tx == nil {
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		log.WithContext(ctx).WithError(err).Error("begin password change transaction failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	affected, err := tx.Where("id = ? and password = ? and session_version = ?", ctx.UserID, currentUser.Password, currentUser.SessionVersion).Cols("password", "session_version", "security_version", "must_change_password", "password_changed_at").Update(&user)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("update password failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	if affected != 1 {
		log.WithContext(ctx).Warn("password changed concurrently")
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	if err := credentialService.RevokeAllWithSession(tx, ctx.UserID); err != nil {
		log.WithContext(ctx).WithError(err).Error("revoke credentials after password change failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	if err := tx.Commit(); err != nil {
		log.WithContext(ctx).WithError(err).Error("commit password change failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	committed = true
	if err := session.Instance.Destroy(req.Context()); err != nil {
		log.WithContext(ctx).WithError(err).Error("session destruction after password change failed")
		response.NewErrorResponse(response.ServerError, i18n.GetText(ctx.Lang, "unknowError"), "").FPrint(w)
		return
	}
	csrf.Clear(w, req, !config.DevelopmentMode)

	response.NewSuccessResponse(map[string]any{
		"message":         i18n.GetText(ctx.Lang, "succ"),
		"reauth_required": true,
	}).FPrint(w)
}
