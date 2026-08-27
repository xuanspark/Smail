package controllers

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	log "github.com/sirupsen/logrus"
)

const (
	loginVerificationTTL        = 10 * time.Minute
	loginVerificationMaxEntries = 10000
)

var (
	errLoginVerificationUnavailable = errors.New("login verification unavailable")
	loginVerificationState          = struct {
		sync.Mutex
		items map[string]loginVerificationItem
	}{items: make(map[string]loginVerificationItem)}
)

type loginVerificationItem struct {
	UserID         int
	Account        string
	Email          string
	SessionVersion int
	ExpiresAt      time.Time
}

type loginVerificationRequest struct {
	ID         string `json:"id"`
	Account    string `json:"account"`
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Code       string `json:"code"`
}

func loginVerificationEnabled() bool {
	current := config.CurrentConfig()
	if current == nil || !current.VerificationEnabled {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(current.LoginPolicy)) {
	case "", "off":
		return false
	case "always", "new_device":
		return true
	default:
		return true
	}
}

func beginLoginVerification(user *models.User, remoteIP string) (map[string]any, error) {
	if !loginVerificationEnabled() {
		return nil, nil
	}
	if user == nil || user.ID <= 0 || securityRuntime.verification == nil || securityRuntime.external == nil {
		return nil, errLoginVerificationUnavailable
	}
	binding, err := securityRuntime.external.FindVerifiedPrimaryByUser(user.ID)
	if err != nil {
		return nil, errLoginVerificationUnavailable
	}
	email, err := securityRuntime.external.Decrypt(*binding)
	if err != nil {
		return nil, errLoginVerificationUnavailable
	}
	challenge, err := securityRuntime.verification.Issue(verification.IssueRequest{
		Purpose: verification.PurposeLogin,
		Account: user.Account,
		Email:   email,
		TTL:     loginVerificationTTL,
	}, remoteIP)
	if err != nil {
		return nil, err
	}
	loginVerificationState.Lock()
	cleanupLoginVerificationLocked(time.Now())
	if len(loginVerificationState.items) >= loginVerificationMaxEntries {
		loginVerificationState.Unlock()
		securityRuntime.verification.Store.InvalidateChallenge(challenge.ID)
		return nil, errLoginVerificationUnavailable
	}
	loginVerificationState.items[challenge.ID] = loginVerificationItem{
		UserID: user.ID, Account: user.Account, Email: email,
		SessionVersion: user.SessionVersion, ExpiresAt: challenge.ExpiresAt,
	}
	loginVerificationState.Unlock()
	return map[string]any{
		"pending":      true,
		"challenge_id": challenge.ID,
		"expires_at":   challenge.ExpiresAt.UTC(),
		"email":        maskEmail(email),
		"account":      user.Account,
	}, nil
}

// CompleteLoginVerification 只有验证码成功后才创建完整 Session。
func CompleteLoginVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input loginVerificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || securityRuntime.verification == nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if input.Account == "" {
		input.Account = input.Identifier
	}
	loginVerificationState.Lock()
	cleanupLoginVerificationLocked(time.Now())
	item, ok := loginVerificationState.items[input.ID]
	loginVerificationState.Unlock()
	if !ok || !strings.EqualFold(strings.TrimSpace(input.Account), item.Account) {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if err := securityRuntime.verification.Verify(input.ID, item.Account, item.Email, input.Code, verification.PurposeLogin, remoteIP(req)); err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	var user models.User
	has, err := db.Instance.ID(item.UserID).Get(&user)
	if err != nil || !has || user.Disabled != 0 || user.Account != item.Account || user.SessionVersion != item.SessionVersion {
		loginVerificationState.Lock()
		delete(loginVerificationState.items, input.ID)
		loginVerificationState.Unlock()
		writeSecurityError(w, response.ParamsError, "登录验证已失效，请重新登录")
		return
	}
	loginVerificationState.Lock()
	delete(loginVerificationState.items, input.ID)
	loginVerificationState.Unlock()
	completeLoginSession(ctx, w, req, &user)
}

func cleanupLoginVerificationLocked(now time.Time) {
	for id, item := range loginVerificationState.items {
		if !now.Before(item.ExpiresAt) {
			delete(loginVerificationState.items, id)
		}
	}
}

func completeLoginSession(ctx *context.Context, w http.ResponseWriter, req *http.Request, user *models.User) {
	if session.Instance == nil {
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	if err := session.Instance.RenewToken(req.Context()); err != nil {
		log.WithContext(ctx).Info("session renewal failed")
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	if _, err := csrf.Issue(w, req, !config.DevelopmentMode); err != nil {
		_ = session.Instance.Clear(req.Context())
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	if err := session.PutUser(req.Context(), user); err != nil {
		_ = session.Instance.Destroy(req.Context())
		csrf.Clear(w, req, !config.DevelopmentMode)
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	cfg := config.CurrentConfig()
	if cfg == nil || cfg.Domain == "" {
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	domains := cfg.Domains
	domains = array.Difference(domains, []string{cfg.Domain})
	domains = append([]string{cfg.Domain}, domains...)
	response.NewSuccessResponse(map[string]any{
		"account": user.Account, "name": user.Name, "role": user.EffectiveRole(),
		"is_admin": user.IsAdmin, "domains": domains,
	}).FPrint(w)
}
