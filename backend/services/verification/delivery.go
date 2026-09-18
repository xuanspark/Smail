package verification

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/services/logstore"
	smailsmtp "git.pcux.cn/xuans/Smail/utils/smtp"
)

type SecurityMode string

const (
	MailDeliveryTimeout              = 45 * time.Second
	SecurityStartTLS    SecurityMode = "starttls"
	SecurityImplicitTLS SecurityMode = "tls"
	SecurityPlain       SecurityMode = "plain"
)

var (
	ErrInvalidSMTPConfig      = errors.New("invalid SMTP configuration")
	ErrUnsupportedSMTP        = errors.New("unsupported SMTP security mode")
	ErrSMTPRelayUnavailable   = errors.New("SMTP relay is unavailable")
	ErrProjectMailUnavailable = errors.New("project mail delivery is unavailable")
	ErrMailDeliveryFailed     = errors.New("verification mail delivery failed")
)

type SMTPSettings struct {
	Host       string
	Port       int
	Username   string
	Password   string
	Security   SecurityMode
	From       string
	FromDomain string
}

type SMTPDelivery struct {
	settings SMTPSettings
	send     func(SMTPSettings, string, []byte) error
}

func NewSMTPDelivery(settings SMTPSettings) (*SMTPDelivery, error) {
	if err := validateSMTPSettings(settings); err != nil {
		return nil, err
	}
	return &SMTPDelivery{settings: settings, send: sendSMTPMessage}, nil
}

// NewSMTPDeliveryContext 将中继投递纳入调用方的总时限。
func NewSMTPDeliveryContext(ctx context.Context, settings SMTPSettings) (*SMTPDelivery, error) {
	delivery, err := NewSMTPDelivery(settings)
	if err != nil {
		return nil, err
	}
	delivery.send = func(settings SMTPSettings, recipient string, message []byte) error {
		return SendSMTPMessageContext(ctx, settings, recipient, message)
	}
	return delivery, nil
}

func (d *SMTPDelivery) Delivery() Delivery {
	return func(challenge Challenge, code string) error {
		if d == nil || d.send == nil || challenge.Email == "" || code == "" {
			return ErrInvalidChallenge
		}
		message := buildCodeMessage(d.settings.From, challenge.Email, code, challenge.Purpose)
		defer clearBytes(message)
		err := d.send(d.settings, challenge.Email, message)
		result := "accepted"
		details := logstore.MailDetails{Trigger: "系统安全验证邮件", Stage: "SMTP 中继投递", AcceptedCount: 1}
		if err != nil {
			result = "failure"
			details.AcceptedCount = 0
			details.Outcome = logstore.MailFailureReason(err)
		}
		// 只记录发送元数据，不保存外部邮箱、验证码、邮件内容及中继凭据。
		logstore.MailResult(0, "outbound", d.settings.From, 1, len(message), result, details)
		return err
	}
}

// NewLazySMTPDelivery 延迟读取 SMTP 中继配置，避免中继未配置时阻塞本地邮件服务启动。
func NewLazySMTPDelivery(load func() (SMTPSettings, error)) Delivery {
	return func(challenge Challenge, code string) error {
		if load == nil || challenge.Email == "" || code == "" {
			return ErrSMTPRelayUnavailable
		}
		settings, err := load()
		if err != nil {
			return fmt.Errorf("load SMTP relay settings: %w", err)
		}
		delivery, err := NewSMTPDelivery(settings)
		if err != nil {
			return fmt.Errorf("SMTP relay is unavailable: %w", err)
		}
		return delivery.Delivery()(challenge, code)
	}
}

func IssueWithSMTP(store *Store, request IssueRequest, settings SMTPSettings) (Challenge, error) {
	if store == nil {
		return Challenge{}, ErrInvalidChallenge
	}
	delivery, err := NewSMTPDelivery(settings)
	if err != nil {
		return Challenge{}, err
	}
	return store.Issue(request, delivery.Delivery())
}

func validateSMTPSettings(settings SMTPSettings) error {
	host := strings.TrimSpace(settings.Host)
	if !isValidSMTPHost(host) {
		return ErrInvalidSMTPConfig
	}
	from := strings.TrimSpace(settings.From)
	parsedFrom, err := mail.ParseAddress(from)
	if err != nil || parsedFrom.Address != from || strings.ContainsAny(from, "\r\n") {
		return ErrInvalidSMTPConfig
	}
	if settings.Port <= 0 || settings.Port > 65535 {
		return ErrInvalidSMTPConfig
	}
	switch settings.Security {
	case SecurityStartTLS, SecurityImplicitTLS:
	case SecurityPlain:
		if !isLocalSMTPHost(host) {
			return ErrInvalidSMTPConfig
		}
	default:
		return ErrUnsupportedSMTP
	}
	if settings.Username == "" && settings.Password != "" {
		return ErrInvalidSMTPConfig
	}
	return nil
}

func isLocalSMTPHost(host string) bool {
	normalized := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if normalized == "localhost" {
		return true
	}
	ip := net.ParseIP(normalized)
	return ip != nil && ip.IsLoopback()
}

func isValidSMTPHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "@/\\\r\n\t ") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if strings.Contains(host, ":") {
		return false
	}
	if len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func sendSMTPMessage(settings SMTPSettings, recipient string, message []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), MailDeliveryTimeout)
	defer cancel()
	return SendSMTPMessageContext(ctx, settings, recipient, message)
}

// SendSMTPMessageContext 允许验证码中继与后备投递共享同一总时限。
func SendSMTPMessageContext(ctx context.Context, settings SMTPSettings, recipient string, message []byte) error {
	addr := net.JoinHostPort(settings.Host, strconv.Itoa(settings.Port))
	var auth smtp.Auth
	if settings.Username != "" {
		auth = smtp.PlainAuth("", settings.Username, settings.Password, settings.Host)
	}
	fromDomain := settings.FromDomain
	if fromDomain == "" {
		parts := strings.SplitN(settings.From, "@", 2)
		if len(parts) == 2 {
			fromDomain = parts[1]
		}
	}
	switch settings.Security {
	case SecurityStartTLS:
		return smailsmtp.SendMailContext(ctx, settings.Host, addr, auth, settings.From, fromDomain, []string{recipient}, message)
	case SecurityImplicitTLS:
		return smailsmtp.SendMailWithTLSContext(ctx, settings.Host, addr, auth, settings.From, fromDomain, []string{recipient}, message)
	case SecurityPlain:
		return smailsmtp.SendMailUnsafeContext(ctx, settings.Host, addr, auth, settings.From, fromDomain, []string{recipient}, message)
	default:
		return ErrUnsupportedSMTP
	}
}

func buildCodeMessage(from, recipient string, code string, purpose Purpose) []byte {
	title, text := CodeMessage(purpose, code)
	return []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", from, recipient, mime.QEncoding.Encode("UTF-8", title), text))
}

// CodeMessage 返回验证码邮件的统一标题和正文，供中继与项目自身投递复用。
func CodeMessage(purpose Purpose, code string) (string, string) {
	title := "Smail 安全验证码"
	purposeText := "完成本次安全验证"
	switch purpose {
	case PurposeLogin:
		purposeText = "完成登录二次验证"
	case PurposePasswordRecover:
		purposeText = "完成密码恢复验证"
	case PurposePasswordChange:
		purposeText = "完成修改密码安全验证"
	case PurposeExternalEmailBind:
		purposeText = "完成外部邮箱绑定验证"
	case PurposeExternalEmailChange:
		purposeText = "完成外部邮箱变更验证"
	}
	text := fmt.Sprintf("您好，您正在%s。验证码：%s\r\n验证码有效期为十分钟，请勿向他人泄露。", purposeText, code)
	return title, text
}

func clearBytes(data []byte) {
	for i := range data {
		data[i] = 0
	}
}
