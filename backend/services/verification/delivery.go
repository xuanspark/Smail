package verification

import (
	"context"
	"errors"
	"fmt"
	"html"
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
		message := buildCodeMessage(d.settings.From, challenge, code)
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

func buildCodeMessage(from string, challenge Challenge, code string) []byte {
	title, text := CodeMessage(challenge.Purpose, code)
	boundary := "=_SmailVerification_"
	htmlBody := CodeHTML(challenge, code)

	return []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n--%s\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n--%s\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s\r\n--%s--\r\n",
		from,
		challenge.Email,
		mime.QEncoding.Encode("UTF-8", title),
		boundary,
		boundary,
		text,
		boundary,
		htmlBody,
		boundary,
	))
}

// CodeHTML 返回验证码邮件的 HTML 正文。
func CodeHTML(challenge Challenge, code string) string {
	recipientName := challenge.Account
	if strings.TrimSpace(recipientName) == "" {
		recipientName = challenge.Email
	}
	return buildCodeHTML(recipientName, code, verificationExpiryMinutes(challenge))
}

func verificationExpiryMinutes(challenge Challenge) int {
	if challenge.ExpiresAt.After(challenge.IssuedAt) && !challenge.IssuedAt.IsZero() {
		minutes := int((challenge.ExpiresAt.Sub(challenge.IssuedAt) + time.Minute - 1) / time.Minute)
		if minutes > 0 {
			return minutes
		}
	}
	return int(DefaultTTL / time.Minute)
}

func buildCodeHTML(recipientName, code string, expiresInMinutes int) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <style>
    body { margin: 0; padding: 24px; background: #f4f4f5; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #18181b; }
    .container { max-width: 640px; margin: 0 auto; background: #ffffff; border-radius: 12px; overflow: hidden; box-shadow: 0 8px 30px rgba(15, 23, 42, 0.10); }
    .header { background: #4f46e5; color: #ffffff; padding: 28px 32px; }
    .header h1 { margin: 0; font-size: 24px; line-height: 1.25; }
    .content { padding: 32px; font-size: 15px; line-height: 1.7; }
    .code { margin: 0; font-size: 28px; font-weight: 700; letter-spacing: 4px; text-align: center; white-space: nowrap; word-break: keep-all; overflow-wrap: normal; }
    .muted { color: #71717a; font-size: 13px; }
    .footer { padding: 18px 32px; background: #fafafa; color: #a1a1aa; font-size: 12px; }
  </style>
</head>
<body>
  <div class="container">
    <div class="header"><h1>邮箱验证码</h1></div>
    <div class="content">
      <p>%s，您好：</p>
      <p>您的验证码是：</p>
      <p class="code" style="margin: 16px 0; font-size: 24px; font-weight: 700; letter-spacing: 2px; text-align: center; white-space: nowrap; word-break: keep-all; overflow-wrap: normal; font-family: Consolas, Monaco, monospace;">%s</p>
      <p>验证码将在 <strong>%d</strong> 分钟后失效。</p>
      <p class="muted">如果不是您本人操作，请忽略此邮件。</p>
    </div>
    <div class="footer">本邮件由 Smail 发送，请勿直接回复。</div>
  </div>
</body>
</html>`,
		html.EscapeString(recipientName),
		html.EscapeString(code),
		expiresInMinutes,
	)
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
	case PurposeAdminLocalReset:
		purposeText = "完成超级管理员本机密码重置验证"
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
