package controllers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	log "github.com/sirupsen/logrus"
)

type loginRequest struct {
	Identifier string `json:"identifier"`
	Account    string `json:"account"`
	Password   string `json:"password"`
}

type loginAttempt struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

type loginLimiter struct {
	mu           sync.Mutex
	attempts     map[string]loginAttempt
	now          func() time.Time
	failureLimit int
}

const (
	loginFailureLimit        = 10
	loginAccountFailureLimit = 20
	loginIPFailureLimit      = 100
	loginWindow              = 5 * time.Minute
	loginBlockTime           = 15 * time.Minute
	loginMaxEntries          = 10000
)

var (
	loginPairAttempts    = newLoginLimiter(loginFailureLimit)
	loginAccountAttempts = newLoginLimiter(loginAccountFailureLimit)
	loginIPAttempts      = newLoginLimiter(loginIPFailureLimit)
)

func newLoginLimiter(failureLimits ...int) *loginLimiter {
	failureLimit := loginFailureLimit
	if len(failureLimits) > 0 && failureLimits[0] > 0 {
		failureLimit = failureLimits[0]
	}
	return &loginLimiter{attempts: make(map[string]loginAttempt), now: time.Now, failureLimit: failureLimit}
}

func (limiter *loginLimiter) key(req *http.Request, account string) string {
	return hashedLoginKey("pair", loginRemoteHost(req), normalizedLoginAccount(account))
}

func (limiter *loginLimiter) accountKey(account string) string {
	return hashedLoginKey("account", normalizedLoginAccount(account))
}

func (limiter *loginLimiter) ipKey(req *http.Request) string {
	return hashedLoginKey("ip", loginRemoteHost(req))
}

func normalizedLoginAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}

func loginRemoteHost(req *http.Request) string {
	if req == nil {
		return "unknown"
	}
	host := remoteIP(req)
	if host == "" {
		host = "unknown"
	}
	return host
}

func hashedLoginKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// loginSecurityFields 返回登录后安全引导所需的非敏感状态。
func loginSecurityFields(user *models.User) map[string]any {
	fields := map[string]any{
		"recoveryCodesConfigured": false,
		"showRecoverySetupPrompt": false,
		"showSecuritySetupPrompt": user != nil && user.SecuritySetupAcknowledged == 0,
		"webAuthnEnabled":         webAuthnRuntime() != nil,
	}
	if user == nil || user.EffectiveRole() == access.RoleSuperAdmin {
		return fields
	}

	configured := false
	if service := credentialRuntime(); service != nil {
		configured, _ = service.HasValidRecoveryCode(user.ID)
	}
	fields["recoveryCodesConfigured"] = configured
	fields["showRecoverySetupPrompt"] = !configured
	return fields
}

func (limiter *loginLimiter) blocked(key string) bool {
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.cleanupLocked(now)
	attempt, exists := limiter.attempts[key]
	return exists && attempt.blockedUntil.After(now)
}

func (limiter *loginLimiter) recordFailure(key string) {
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.cleanupLocked(now)
	attempt := limiter.attempts[key]
	if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) > loginWindow {
		attempt = loginAttempt{windowStart: now}
	}
	attempt.failures++
	attempt.lastSeen = now
	if attempt.failures >= limiter.failureLimit {
		attempt.blockedUntil = now.Add(loginBlockTime)
	}
	if _, exists := limiter.attempts[key]; !exists && len(limiter.attempts) >= loginMaxEntries {
		limiter.evictOldestLocked()
	}
	limiter.attempts[key] = attempt
}

func (limiter *loginLimiter) clear(key string) {
	limiter.mu.Lock()
	delete(limiter.attempts, key)
	limiter.mu.Unlock()
}

func (limiter *loginLimiter) cleanupLocked(now time.Time) {
	for key, attempt := range limiter.attempts {
		windowExpired := !attempt.windowStart.IsZero() && now.Sub(attempt.windowStart) > loginWindow
		blockExpired := attempt.blockedUntil.IsZero() || !attempt.blockedUntil.After(now)
		if windowExpired && blockExpired {
			delete(limiter.attempts, key)
		}
	}
}

func (limiter *loginLimiter) evictOldestLocked() {
	oldestKey := ""
	var oldestTime time.Time
	for key, attempt := range limiter.attempts {
		if oldestKey == "" || attempt.lastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = attempt.lastSeen
		}
	}
	if oldestKey != "" {
		delete(limiter.attempts, oldestKey)
	}
}

func Login(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData loginRequest
	if err := decodeJSONRequest(w, req, &reqData); err != nil {
		log.WithContext(ctx).Debug("invalid login request")
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "aperror"), "").FPrint(w)
		return
	}
	identifier := strings.TrimSpace(reqData.Identifier)
	if identifier == "" {
		identifier = strings.TrimSpace(reqData.Account)
	}

	pairKey := loginPairAttempts.key(req, identifier)
	accountKey := loginAccountAttempts.accountKey(identifier)
	ipKey := loginIPAttempts.ipKey(req)
	if loginPairAttempts.blocked(pairKey) || loginAccountAttempts.blocked(accountKey) || loginIPAttempts.blocked(ipKey) {
		log.WithContext(ctx).Info("login rejected by rate limit")
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "aperror"), "").FPrint(w)
		return
	}

	user, err := auth.AuthenticateIdentifier(identifier, reqData.Password)
	if err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		loginPairAttempts.recordFailure(pairKey)
		loginAccountAttempts.recordFailure(accountKey)
		loginIPAttempts.recordFailure(ipKey)
		log.WithContext(ctx).Info("login failed")
		response.NewErrorResponse(response.ParamsError, i18n.GetText(ctx.Lang, "aperror"), "").FPrint(w)
		return
	}
	loginPairAttempts.clear(pairKey)
	loginAccountAttempts.clear(accountKey)

	if pending, err := beginLoginVerification(user, remoteIP(req)); err != nil {

		log.WithContext(ctx).WithError(err).Info("登录二次验证未就绪")

		response.NewErrorResponse(response.ParamsError, "登录验证服务暂不可用，请稍后重试", "").FPrint(w)

		return

	} else if pending != nil {

		response.NewSuccessResponse(pending).FPrint(w)

		return

	}

	if err := session.Instance.RenewToken(req.Context()); err != nil {
		log.WithContext(ctx).Info("session renewal failed")
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	if _, err := csrf.Issue(w, req, !config.DevelopmentMode); err != nil {
		if clearErr := session.Instance.Clear(req.Context()); clearErr != nil {
			log.WithContext(ctx).WithError(clearErr).Error("session cleanup after csrf failure failed")
		}
		log.WithContext(ctx).Info("csrf token issue failed")
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	if err := session.PutUser(req.Context(), user); err != nil {
		if destroyErr := session.Instance.Destroy(req.Context()); destroyErr != nil {
			log.WithContext(ctx).WithError(destroyErr).Error("session destruction after user persistence failure failed")
		}
		csrf.Clear(w, req, !config.DevelopmentMode)
		log.WithContext(ctx).WithError(err).Error("session user persistence failed")
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}

	domain, domains, ok := loginDomainSettings()
	if !ok {
		response.NewErrorResponse(response.ServerError, "登录失败", "").FPrint(w)
		return
	}
	domains = append([]string{domain}, domains...)

	result := map[string]any{
		"account":              user.Account,
		"name":                 user.Name,
		"role":                 user.EffectiveRole(),
		"capabilities":         access.Capabilities(user.EffectiveRole()),
		"is_admin":             user.IsAdmin,
		"domains":              domains,
		"must_change_password": user.MustChangePassword != 0,
	}
	for key, value := range loginSecurityFields(user) {
		result[key] = value
	}
	setLoginAuditActor(ctx, user)
	response.NewSuccessResponse(result).FPrint(w)
}

// setLoginAuditActor 只在完整登录成功后补充审计身份，不将待二次验证视为登录完成。
func setLoginAuditActor(ctx *context.Context, user *models.User) {
	ctx.UserID = user.ID
	ctx.UserAccount = user.Account
	ctx.UserName = user.Name
	ctx.Role = user.EffectiveRole()
}

func Logout(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	csrf.Clear(w, req, !config.DevelopmentMode)
	if err := session.Instance.Destroy(ctx.Context); err != nil {
		log.WithContext(ctx).WithError(err).Error("session cleanup during logout failed")
		response.NewErrorResponse(response.ServerError, "退出失败", "").FPrint(w)
		return
	}
	response.NewSuccessResponse("Success").FPrint(w)
}
