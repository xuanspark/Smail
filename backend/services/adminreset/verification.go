package adminreset

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/totp"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/password"
	"xorm.io/xorm"
)

// VerificationService 的限流状态跨连接保留，不能通过重新启动客户端刷新尝试次数。
type VerificationService struct {
	engine     *xorm.Engine
	email      *externalemail.Repository
	codes      *credentials.Service
	totp       *totp.Service
	verify     *verification.Service
	invalidate func(int, string)
	mu         sync.Mutex
	attempts   int
	window     time.Time
}

func NewVerificationService(engine *xorm.Engine, email *externalemail.Repository, codes *credentials.Service, authenticator *totp.Service, verify *verification.Service, invalidate func(int, string)) *VerificationService {
	return &VerificationService{engine: engine, email: email, codes: codes, totp: authenticator, verify: verify, invalidate: invalidate}
}

type verifiedReset struct {
	service       *VerificationService
	user          models.User
	methods       []Method
	emails        map[string]models.ExternalEmail
	authenticator models.TOTPAuthenticator
	selected      string
	challenge     verification.Challenge
	context       string
	email         string
	recovery      models.RecoveryCode
	recoveryCode  string
	verified      bool
	expires       time.Time
}

func (s *VerificationService) Begin() (ResetSession, error) {
	if s == nil || s.engine == nil || s.email == nil || s.codes == nil || s.totp == nil || s.verify == nil {
		return nil, ErrInvalidRequest
	}
	var users []models.User
	if err := s.engine.Find(&users); err != nil {
		return nil, err
	}
	var targets []models.User
	for _, user := range users {
		if user.EffectiveRole() == access.RoleSuperAdmin {
			targets = append(targets, user)
		}
	}
	if len(targets) != 1 || targets[0].Disabled != 0 {
		return nil, ErrSuperAdminNotFound
	}
	flow := &verifiedReset{service: s, user: targets[0], emails: make(map[string]models.ExternalEmail), expires: time.Now().Add(controlRequestTimeout)}
	items, err := s.email.List(flow.user.ID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.Status != models.ExternalEmailStatusVerified {
			continue
		}
		address, err := s.email.Decrypt(item)
		if err != nil {
			return nil, err
		}
		id := fmt.Sprintf("email:%d", item.ID)
		flow.emails[id] = item
		flow.methods = append(flow.methods, Method{ID: id, Label: "发送验证码到 " + address})
	}
	has, err := s.engine.Where("user_id = ? AND enabled = ? AND revoked_at IS NULL", flow.user.ID, true).Get(&flow.authenticator)
	if err != nil {
		return nil, err
	}
	if has {
		flow.methods = append(flow.methods, Method{ID: "totp", Label: "手机验证器动态码"})
	}
	has, err = s.codes.HasValidRecoveryCode(flow.user.ID)
	if err != nil {
		return nil, err
	}
	if has {
		flow.methods = append(flow.methods, Method{ID: "recovery", Label: "安全码"})
	}
	if len(flow.methods) == 0 {
		return nil, ErrInvalidRequest
	}
	flow.context, err = NewNonce()
	return flow, err
}

func (f *verifiedReset) Methods() []Method { return f.methods }

func (f *verifiedReset) Start(method string) error {
	if f.selected != "" || !time.Now().Before(f.expires) {
		return ErrInvalidRequest
	}
	available := false
	for _, item := range f.methods {
		if item.ID == method {
			available = true
		}
	}
	if !available {
		return ErrInvalidRequest
	}
	f.selected = method
	if item, ok := f.emails[method]; ok {
		if err := f.checkCurrent(nil); err != nil {
			return err
		}
		address, err := f.service.email.Decrypt(item)
		if err != nil {
			return err
		}
		f.email = address
		f.challenge, err = f.service.verify.Issue(verification.IssueRequest{
			Purpose: verification.PurposeAdminLocalReset, Account: f.user.Account, UserID: f.user.ID,
			Email: address, ResourceID: item.ID, CredentialVersion: item.CredentialVersion,
			BrowserTransaction: f.context, Environment: "local-ipc", TTL: controlRequestTimeout,
		}, "local-ipc")
		return err
	}
	return nil
}

func (s *VerificationService) allowAttempt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.window) >= 15*time.Minute {
		s.window = time.Now()
		s.attempts = 0
	}
	if s.attempts >= 5 {
		return false
	}
	s.attempts++
	return true
}

func (f *verifiedReset) Verify(code string) error {
	if f.verified || f.selected == "" || !f.service.allowAttempt() {
		return ErrInvalidRequest
	}
	if err := f.checkCurrent(nil); err != nil {
		return err
	}
	var err error
	switch f.selected {
	case "totp":
		err = f.service.totp.Verify(f.user.ID, strings.TrimSpace(code))
	case "recovery":
		code = strings.ToUpper(strings.TrimSpace(code))
		if len(code) < 12 || len(code) > 64 {
			return ErrInvalidRequest
		}
		var items []models.RecoveryCode
		if err := f.service.engine.Where("user_id = ? AND used_at IS NULL", f.user.ID).Find(&items); err != nil {
			return err
		}
		err = ErrInvalidRequest
		for _, item := range items {
			matched, checkErr := password.Check(item.CodeHash, code)
			if checkErr != nil {
				return checkErr
			}
			if matched {
				f.recovery = item
				f.recoveryCode = code
				err = nil
				break
			}
		}
	default:
		item, ok := f.emails[f.selected]
		if !ok {
			return ErrInvalidRequest
		}
		err = f.service.verify.VerifyBoundWithContext(f.challenge.ID, f.user.Account, f.email, strings.TrimSpace(code), verification.PurposeAdminLocalReset, item.ID, "local-ipc", f.context)
	}
	if err != nil {
		f.Close()
		return err
	}
	f.verified = true
	return nil
}

// checkCurrent 同时绑定账号版本与所选凭据，验证后更换邮箱或撤销凭据不能继续重置。
func (f *verifiedReset) checkCurrent(tx *xorm.Session) error {
	if !time.Now().Before(f.expires) {
		return ErrExpiredSession
	}
	if tx == nil {
		tx = f.service.engine.NewSession()
		defer tx.Close()
	}
	var user models.User
	found, err := tx.ID(f.user.ID).Get(&user)
	if err != nil {
		return err
	}
	if !found || user.Disabled != 0 || user.EffectiveRole() != access.RoleSuperAdmin || user.Account != f.user.Account || user.Password != f.user.Password || user.SessionVersion != f.user.SessionVersion || user.SecurityVersion != f.user.SecurityVersion {
		return ErrInvalidRequest
	}
	if item, ok := f.emails[f.selected]; ok {
		var current models.ExternalEmail
		found, err := tx.ID(item.ID).Get(&current)
		if err != nil {
			return err
		}
		if !found || current.UserID != user.ID || current.Status != models.ExternalEmailStatusVerified || current.CredentialVersion != item.CredentialVersion || current.EmailCiphertext != item.EmailCiphertext {
			return ErrInvalidRequest
		}
	} else if f.selected == "totp" {
		var current models.TOTPAuthenticator
		found, err := tx.ID(f.authenticator.ID).Get(&current)
		if err != nil {
			return err
		}
		if !found || current.UserID != user.ID || !current.Enabled || current.RevokedAt != nil || current.CredentialVersion != f.authenticator.CredentialVersion || current.SecretCiphertext != f.authenticator.SecretCiphertext {
			return ErrInvalidRequest
		}
	} else if f.selected == "recovery" && f.verified {
		var current models.RecoveryCode
		found, err := tx.ID(f.recovery.ID).Get(&current)
		if err != nil {
			return err
		}
		if !found || current.UserID != user.ID || current.UsedAt != nil || current.CodeHash != f.recovery.CodeHash {
			return ErrInvalidRequest
		}
	}
	return nil
}

func (f *verifiedReset) Reset(value string) error {
	if !f.verified {
		return ErrInvalidRequest
	}
	defer f.Close()
	return resetSuperAdmin(f.service.engine, value, func(tx *xorm.Session, target models.User) error {
		if target.ID != f.user.ID {
			return ErrInvalidRequest
		}
		if err := f.checkCurrent(tx); err != nil {
			return err
		}
		if f.selected == "recovery" {
			return f.service.codes.ConsumeRecoveryCodeWithSession(tx, target.ID, f.recoveryCode)
		}
		return nil
	}, f.service.invalidate)
}

func (f *verifiedReset) Close() {
	f.verified = false
	f.expires = time.Time{}
	clearPassword(&f.recoveryCode)
	if f.challenge.ID != "" {
		f.service.verify.Store.InvalidateChallenge(f.challenge.ID)
	}
}
