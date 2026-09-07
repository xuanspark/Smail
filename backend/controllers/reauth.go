package controllers

import (
	"net/http"
	"strconv"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

const (
	reauthUserFailureLimit = 10
	reauthIPFailureLimit   = 50
)

var (
	reauthUserAttempts = newLoginLimiter(reauthUserFailureLimit)
	reauthIPAttempts   = newLoginLimiter(reauthIPFailureLimit)
)

type reauthRequest struct {
	Password string `json:"password"`
}

// ReauthenticateCurrentUser 在不离开当前页面的情况下重新确认当前会话用户密码。
func ReauthenticateCurrentUser(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 || session.Instance == nil {
		writeReauthError(w, http.StatusUnauthorized, "REAUTH_SESSION_REQUIRED", "登录状态已失效，请重新登录")
		return
	}

	var input reauthRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.Password == "" {
		writeReauthError(w, http.StatusBadRequest, "REAUTH_REQUEST_INVALID", "请输入当前密码")
		return
	}
	defer func() { input.Password = "" }()

	userKey := reauthUserAttempts.accountKey(strconv.Itoa(ctx.UserID))
	ipKey := reauthIPAttempts.ipKey(req)
	if reauthUserAttempts.blocked(userKey) || reauthIPAttempts.blocked(ipKey) {
		log.WithContext(ctx).Info("reauthentication rejected by rate limit")
		writeReauthError(w, http.StatusTooManyRequests, "REAUTH_RATE_LIMITED", "尝试次数过多，请稍后再试")
		return
	}

	user, err := auth.AuthenticateUserByID(ctx.UserID, input.Password)
	if err != nil {
		reauthUserAttempts.recordFailure(userKey)
		reauthIPAttempts.recordFailure(ipKey)
		log.WithContext(ctx).Info("reauthentication failed")
		writeReauthError(w, http.StatusUnauthorized, "REAUTH_FAILED", "当前密码不正确")
		return
	}
	if err := session.RefreshRecentAuthentication(req.Context(), user); err != nil {
		log.WithContext(ctx).WithError(err).Warn("refresh recent authentication failed")
		writeReauthError(w, http.StatusUnauthorized, "REAUTH_SESSION_CHANGED", "登录状态已变化，请重新登录")
		return
	}

	reauthUserAttempts.clear(userKey)
	response.NewSuccessResponse(map[string]any{"reauthenticated": true}).FPrint(w)
}

func writeReauthError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	response.NewErrorResponse(response.ConfigErrorNumber(status), message, response.ConfigErrorData{Code: code}).FPrint(w)
}
