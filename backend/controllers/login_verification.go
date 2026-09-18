package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/externalemail"
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
	TOTPEnabled    bool
	EmailChallenge string
	DeliveryStatus string
	DeliveryError  string
	RetryAt        time.Time
	Attempts       int
	Verifying      bool
}

var loginDeliverySlots = make(chan struct{}, 16)
var loginSecondFactorAttempts = newLoginLimiter(20)

type resendLoginVerificationRequest struct {
	ID      string `json:"id"`
	Account string `json:"account"`
}
type loginVerificationRequest struct {
	ID         string `json:"id"`
	Account    string `json:"account"`
	Identifier string `json:"identifier"`
	Email      string `json:"email"`
	Code       string `json:"code"`
	Method     string `json:"method"`
}

func loginVerificationEnabled() bool {
	if manager := config.CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		if runtime.SetupState == config.SetupStateReady {
			return loginVerificationPolicyEnabled(runtime.SMTP.Relay.VerificationEnabled, runtime.SMTP.Relay.LoginPolicy)
		}
	}
	current := config.CurrentConfig()
	if current == nil {
		return false
	}
	return loginVerificationPolicyEnabled(current.VerificationEnabled, current.LoginPolicy)
}

func loginVerificationPolicyEnabled(enabled bool, policy string) bool {
	if !enabled {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "", "off":
		return false
	case "always", "new_device":
		return true
	default:
		return true
	}
}

func beginLoginVerification(user *models.User, remoteIP string) (map[string]any, error) {
	runtime := currentSecurityRuntime()
	if user == nil || user.ID <= 0 {
		return nil, errLoginVerificationUnavailable
	}
	item := loginVerificationItem{
		UserID: user.ID, Account: user.Account, SessionVersion: user.SessionVersion,
		ExpiresAt: time.Now().Add(loginVerificationTTL), DeliveryStatus: "idle",
	}
	if runtime.totp != nil {
		var err error
		item.TOTPEnabled, err = runtime.totp.IsEnabled(user.ID)
		if err != nil {
			return nil, errLoginVerificationUnavailable
		}
	}
	if loginVerificationEnabled() || item.TOTPEnabled {
		if runtime.external == nil || runtime.verification == nil {
			return nil, errLoginVerificationUnavailable
		}
		binding, err := runtime.external.FindVerifiedPrimaryByUser(user.ID)
		if err != nil && !errors.Is(err, externalemail.ErrBindingNotFound) {
			return nil, errLoginVerificationUnavailable
		}
		if err == nil {
			item.Email, err = runtime.external.Decrypt(*binding)
			if err != nil {
				return nil, errLoginVerificationUnavailable
			}
		}
	}
	if item.Email == "" && !item.TOTPEnabled {
		return nil, nil
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, errLoginVerificationUnavailable
	}
	id := hex.EncodeToString(token[:])
	loginVerificationState.Lock()
	defer loginVerificationState.Unlock()
	cleanupLoginVerificationLocked(time.Now())
	if len(loginVerificationState.items) >= loginVerificationMaxEntries {
		return nil, errLoginVerificationUnavailable
	}
	loginVerificationState.items[id] = item
	return loginVerificationResponse(id, item), nil
}

func loginVerificationResponse(id string, item loginVerificationItem) map[string]any {
	methods := []string{}
	if item.Email != "" {
		methods = append(methods, "email")
	}
	if item.TOTPEnabled {
		methods = append(methods, "totp")
	}
	return map[string]any{
		"pending": true, "challenge_id": id, "account": item.Account,
		"expires_at": item.ExpiresAt.UTC(), "email": maskEmail(item.Email),
		"methods": methods, "delivery_status": item.DeliveryStatus,
		"delivery_error": item.DeliveryError, "retry_at": item.RetryAt.UTC(),
	}
}

// ResendLoginVerification 只启动有并发上限的后台投递，HTTP 请求不等待邮件服务器。
func ResendLoginVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	var input resendLoginVerificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.ID) == "" {
		writeSecurityError(w, response.ParamsError, "验证码请求无效")
		return
	}

	item, ok := pendingLoginVerification(input.ID, input.Account)
	if !ok || !loginVerificationUserValid(item) {
		writeSecurityError(w, response.ParamsError, "登录验证已失效，请重新登录")
		return
	}
	if runtime.verification == nil || runtime.verification.Store == nil || item.Email == "" {
		writeSecurityError(w, response.ParamsError, "验证码服务暂不可用")
		return
	}
	loginVerificationState.Lock()
	item, ok = loginVerificationState.items[input.ID]
	if !ok || !time.Now().Before(item.ExpiresAt) {
		loginVerificationState.Unlock()
		writeSecurityError(w, response.ParamsError, "登录验证已失效，请重新登录")
		return
	}
	if item.DeliveryStatus == "sending" || time.Now().Before(item.RetryAt) {
		loginVerificationState.Unlock()
		response.NewSuccessResponse(loginVerificationResponse(input.ID, item)).FPrint(w)
		return
	}
	select {
	case loginDeliverySlots <- struct{}{}:
	default:
		loginVerificationState.Unlock()
		writeSecurityError(w, response.ParamsError, "邮件发送繁忙，请稍后重试或使用动态码")
		return
	}
	previousChallenge := item.EmailChallenge
	item.EmailChallenge = ""
	item.DeliveryStatus, item.DeliveryError = "sending", ""
	item.RetryAt = time.Now().Add(time.Minute)
	loginVerificationState.items[input.ID] = item
	loginVerificationState.Unlock()
	ip := remoteIP(req)
	go func() {
		defer func() { <-loginDeliverySlots }()
		if previousChallenge != "" {
			runtime.verification.Store.InvalidateChallenge(previousChallenge)
		}
		challenge, err := runtime.verification.Issue(verification.IssueRequest{
			Purpose: verification.PurposeLogin, UserID: item.UserID, Account: item.Account,
			Email: item.Email, TTL: time.Until(item.ExpiresAt),
		}, ip)
		loginVerificationState.Lock()
		current, exists := loginVerificationState.items[input.ID]
		if exists && time.Now().Before(current.ExpiresAt) {
			if err != nil {
				current.DeliveryStatus = "failed"
				current.DeliveryError = externalEmailVerificationIssueMessage(err)
			} else {
				current.DeliveryStatus, current.EmailChallenge = "sent", challenge.ID
			}
			loginVerificationState.items[input.ID] = current
		}
		loginVerificationState.Unlock()
		if !exists || !time.Now().Before(current.ExpiresAt) {
			runtime.verification.Store.InvalidateChallenge(challenge.ID)
		}
	}()
	response.NewSuccessResponse(loginVerificationResponse(input.ID, item)).FPrint(w)
}

func LoginVerificationStatus(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var input resendLoginVerificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "登录验证请求无效")
		return
	}
	item, ok := pendingLoginVerification(input.ID, input.Account)
	if !ok || !loginVerificationUserValid(item) {
		writeSecurityError(w, response.ParamsError, "登录验证已失效，请重新登录")
		return
	}
	response.NewSuccessResponse(loginVerificationResponse(input.ID, item)).FPrint(w)
}

func loginVerificationUserValid(item loginVerificationItem) bool {
	if db.Instance == nil {
		return false
	}
	var user models.User
	has, err := db.Instance.ID(item.UserID).Get(&user)
	return err == nil && has && user.Disabled == 0 && user.Account == item.Account && user.SessionVersion == item.SessionVersion
}

// CompleteLoginVerification 只有验证码成功后才创建完整 Session。
func CompleteLoginVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	var input loginVerificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	if input.Account == "" {
		input.Account = input.Identifier
	}
	item, ok := pendingLoginVerification(input.ID, input.Account)
	if !ok || !loginVerificationUserValid(item) {
		writeSecurityError(w, response.ParamsError, "验证失败")
		return
	}
	limitKey := loginSecondFactorAttempts.accountKey(item.Account)
	if loginSecondFactorAttempts.blocked(limitKey) {
		writeSecurityError(w, response.ParamsError, "验证失败次数过多，请十五分钟后重试")
		return
	}
	loginVerificationState.Lock()
	item, ok = loginVerificationState.items[input.ID]
	if !ok || item.Verifying || item.Attempts >= 5 || !time.Now().Before(item.ExpiresAt) {
		loginVerificationState.Unlock()
		writeSecurityError(w, response.ParamsError, "登录验证已失效或正在验证，请重新登录")
		return
	}
	item.Attempts++
	item.Verifying = true
	loginVerificationState.items[input.ID] = item
	loginVerificationState.Unlock()
	var verifyErr error
	switch input.Method {
	case "totp":
		if !item.TOTPEnabled || runtime.totp == nil {
			verifyErr = errLoginVerificationUnavailable
		} else {
			verifyErr = runtime.totp.Verify(item.UserID, input.Code)
		}
	case "", "email":
		if item.EmailChallenge == "" || runtime.verification == nil {
			verifyErr = errLoginVerificationUnavailable
		} else {
			verifyErr = runtime.verification.Verify(item.EmailChallenge, item.Account, item.Email, input.Code, verification.PurposeLogin, remoteIP(req))
		}
	default:
		verifyErr = errLoginVerificationUnavailable
	}
	if verifyErr != nil {
		loginSecondFactorAttempts.recordFailure(limitKey)
		loginVerificationState.Lock()
		if current, exists := loginVerificationState.items[input.ID]; exists {
			current.Verifying = false
			if current.Attempts >= 5 {
				delete(loginVerificationState.items, input.ID)
			} else {
				loginVerificationState.items[input.ID] = current
			}
		}
		loginVerificationState.Unlock()
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
	if !consumePendingLoginVerification(input.ID, item) {
		writeSecurityError(w, response.ParamsError, "登录验证已失效，请重新登录")
		return
	}
	loginSecondFactorAttempts.clear(limitKey)
	if runtime.verification != nil && item.EmailChallenge != "" {
		runtime.verification.Store.InvalidateChallenge(item.EmailChallenge)
	}
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

func loginDomainSettings() (string, []string, bool) {
	if manager := config.CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		primary := strings.TrimSpace(runtime.Root.Domains.Primary)
		if primary == "" {
			return "", nil, false
		}
		domains := append([]string(nil), runtime.Root.Domains.Accepted...)
		domains = array.Difference(domains, []string{primary})
		return primary, domains, true
	}
	current := config.CurrentConfig()
	if current == nil || strings.TrimSpace(current.Domain) == "" {
		return "", nil, false
	}
	domains := array.Difference(append([]string(nil), current.Domains...), []string{current.Domain})
	return current.Domain, domains, true
}
func pendingLoginVerification(challengeID, account string) (loginVerificationItem, bool) {
	loginVerificationState.Lock()
	defer loginVerificationState.Unlock()
	cleanupLoginVerificationLocked(time.Now())
	item, ok := loginVerificationState.items[strings.TrimSpace(challengeID)]
	if !ok || !strings.EqualFold(strings.TrimSpace(account), item.Account) {
		return loginVerificationItem{}, false
	}
	return item, true
}

func consumePendingLoginVerification(challengeID string, expected loginVerificationItem) bool {
	loginVerificationState.Lock()
	defer loginVerificationState.Unlock()
	item, ok := loginVerificationState.items[strings.TrimSpace(challengeID)]
	if !ok || !time.Now().Before(item.ExpiresAt) || item.UserID != expected.UserID || item.SessionVersion != expected.SessionVersion || !strings.EqualFold(item.Account, expected.Account) {
		return false
	}
	delete(loginVerificationState.items, strings.TrimSpace(challengeID))
	return true
}
