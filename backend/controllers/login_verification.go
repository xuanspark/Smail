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
}

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
	if !loginVerificationEnabled() {
		return nil, nil
	}
	if user == nil || user.ID <= 0 || runtime.verification == nil || runtime.external == nil {
		return nil, errLoginVerificationUnavailable
	}
	binding, err := runtime.external.FindVerifiedPrimaryByUser(user.ID)
	if err != nil {
		if errors.Is(err, externalemail.ErrBindingNotFound) {
			return nil, nil
		}
		return nil, errLoginVerificationUnavailable
	}
	email, err := runtime.external.Decrypt(*binding)
	if err != nil {
		return nil, errLoginVerificationUnavailable
	}
	challenge, err := runtime.verification.Issue(verification.IssueRequest{
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
		runtime.verification.Store.InvalidateChallenge(challenge.ID)
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

// ResendLoginVerification 使旧登录验证码失效后重新发送新验证码。
func ResendLoginVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	var input resendLoginVerificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.ID) == "" {
		writeSecurityError(w, response.ParamsError, "验证码请求无效")
		return
	}

	loginVerificationState.Lock()
	cleanupLoginVerificationLocked(time.Now())
	item, ok := loginVerificationState.items[input.ID]
	if ok {
		delete(loginVerificationState.items, input.ID)
	}
	loginVerificationState.Unlock()
	if !ok || !strings.EqualFold(strings.TrimSpace(input.Account), item.Account) {
		writeSecurityError(w, response.ParamsError, "登录验证已失效，请重新登录")
		return
	}
	if runtime.verification == nil || runtime.verification.Store == nil {
		writeSecurityError(w, response.ParamsError, "验证码服务暂不可用")
		return
	}
	runtime.verification.Store.InvalidateChallenge(input.ID)
	challenge, err := runtime.verification.Issue(verification.IssueRequest{
		Purpose: verification.PurposeLogin,
		UserID:  item.UserID,
		Account: item.Account,
		Email:   item.Email,
		TTL:     loginVerificationTTL,
	}, remoteIP(req))
	if err != nil {
		writeSecurityError(w, response.ParamsError, "验证码发送失败，请稍后重试")
		return
	}

	loginVerificationState.Lock()
	cleanupLoginVerificationLocked(time.Now())
	loginVerificationState.items[challenge.ID] = loginVerificationItem{
		UserID: item.UserID, Account: item.Account, Email: item.Email,
		SessionVersion: item.SessionVersion, ExpiresAt: challenge.ExpiresAt,
	}
	loginVerificationState.Unlock()
	response.NewSuccessResponse(map[string]any{
		"pending":      true,
		"challenge_id": challenge.ID,
		"expires_at":   challenge.ExpiresAt.UTC(),
		"email":        maskEmail(item.Email),
		"account":      item.Account,
	}).FPrint(w)
}

// CompleteLoginVerification 只有验证码成功后才创建完整 Session。
func CompleteLoginVerification(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	runtime := currentSecurityRuntime()
	var input loginVerificationRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || runtime.verification == nil {
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
	if err := runtime.verification.Verify(input.ID, item.Account, item.Email, input.Code, verification.PurposeLogin, remoteIP(req)); err != nil {
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
	if !ok || item.UserID != expected.UserID || item.SessionVersion != expected.SessionVersion || !strings.EqualFold(item.Account, expected.Account) {
		return false
	}
	delete(loginVerificationState.items, strings.TrimSpace(challengeID))
	return true
}
