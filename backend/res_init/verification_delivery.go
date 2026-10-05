package res_init

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/verification"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/send"
	smailsmtp "git.pcux.cn/xuans/Smail/utils/smtp"
)

func newVerificationDelivery() verification.Delivery {
	return func(challenge verification.Challenge, code string) error {
		ctx, cancel := context.WithTimeout(context.Background(), verification.MailDeliveryTimeout)
		defer cancel()
		return newVerificationDeliveryWithDependencies(
			config.CurrentSMTPRuntimeSettings,
			func(settings config.SMTPRuntimeSettings, challenge verification.Challenge, code string) error {
				return deliverVerificationWithSMTPRelayContext(ctx, settings, challenge, code)
			},
			func(challenge verification.Challenge, code string) error {
				return deliverVerificationWithProjectMailContext(ctx, challenge, code)
			},
		)(challenge, code)
	}
}

func newVerificationDeliveryWithDependencies(
	loadSettings func() (config.SMTPRuntimeSettings, error),
	deliverRelay func(config.SMTPRuntimeSettings, verification.Challenge, string) error,
	deliverProject func(verification.Challenge, string) error,
) verification.Delivery {
	return func(challenge verification.Challenge, code string) error {
		if strings.TrimSpace(challenge.Email) == "" || strings.TrimSpace(code) == "" {
			return verification.ErrInvalidChallenge
		}
		if loadSettings == nil || deliverRelay == nil || deliverProject == nil {
			return verification.ErrProjectMailUnavailable
		}

		runtimeSettings, err := loadSettings()
		if err != nil {
			return fmt.Errorf("load verification mail settings: %w", err)
		}
		if smtpRelayRequested(runtimeSettings) {
			relayErr := deliverRelay(runtimeSettings, challenge, code)
			if relayErr == nil {
				return nil
			}
			if smailsmtp.IsDeliveryUncertain(relayErr) {
				return relayErr
			}
			if !smtpRelayComplete(runtimeSettings) {
				return relayErr
			}
			if projectErr := deliverProject(challenge, code); projectErr != nil {
				return fmt.Errorf("SMTP 中继投递失败且项目自身投递失败: %w", projectErr)
			}
			return nil
		}

		return deliverProject(challenge, code)
	}
}

func smtpRelayComplete(settings config.SMTPRuntimeSettings) bool {
	relay := settings.Relay
	if strings.TrimSpace(relay.RelayHost) == "" || relay.RelayPort < 1 || relay.RelayPort > 65535 ||
		strings.TrimSpace(relay.Security) == "" || strings.TrimSpace(relay.From) == "" {
		return false
	}
	return (strings.TrimSpace(relay.Username) == "") == (strings.TrimSpace(settings.Password) == "")
}

func smtpRelayRequested(settings config.SMTPRuntimeSettings) bool {
	return settings.Relay.RelayActive()
}

func verificationSMTPSettings(settings config.SMTPRuntimeSettings) (verification.SMTPSettings, error) {
	security, err := smtpSecurityMode(settings.Relay.Security)
	if err != nil {
		return verification.SMTPSettings{}, err
	}
	return verification.SMTPSettings{
		Host:       settings.Relay.RelayHost,
		Port:       settings.Relay.RelayPort,
		Username:   settings.Relay.Username,
		Password:   settings.Password,
		Security:   security,
		From:       settings.Relay.From,
		FromDomain: settings.Relay.FromDomain,
	}, nil
}

func deliverVerificationWithSMTPRelay(runtimeSettings config.SMTPRuntimeSettings, challenge verification.Challenge, code string) error {
	ctx, cancel := context.WithTimeout(context.Background(), verification.MailDeliveryTimeout)
	defer cancel()
	return deliverVerificationWithSMTPRelayContext(ctx, runtimeSettings, challenge, code)
}

func deliverVerificationWithSMTPRelayContext(ctx context.Context, runtimeSettings config.SMTPRuntimeSettings, challenge verification.Challenge, code string) error {
	settings, err := verificationSMTPSettings(runtimeSettings)
	if err != nil {
		return err
	}
	delivery, err := verification.NewSMTPDeliveryContext(ctx, settings)
	if err != nil {
		return fmt.Errorf("SMTP relay is unavailable: %w", err)
	}
	if err = delivery.Delivery()(challenge, code); err != nil {
		return fmt.Errorf("%w: %w", verification.ErrMailDeliveryFailed, err)
	}
	return nil
}

func deliverVerificationWithProjectMail(challenge verification.Challenge, code string) error {
	ctx, cancel := context.WithTimeout(context.Background(), verification.MailDeliveryTimeout)
	defer cancel()
	return deliverVerificationWithProjectMailContext(ctx, challenge, code)
}

func deliverVerificationWithProjectMailContext(deliveryContext context.Context, challenge verification.Challenge, code string) error {
	if err := deliveryContext.Err(); err != nil {
		return err
	}
	domain := strings.TrimSpace(config.CurrentMailDomains().Primary)
	if domain == "" {
		return fmt.Errorf("%w: primary mail domain is not configured", verification.ErrProjectMailUnavailable)
	}
	messageID, err := parsemail.GenerateMsgID(domain)
	if err != nil {
		return fmt.Errorf("%w: generate message id", verification.ErrProjectMailUnavailable)
	}
	subject, text := verification.CodeMessage(challenge.Purpose, code)
	sender := &parsemail.User{
		Name:         "Smail 安全中心",
		EmailAddress: "no-reply@" + domain,
	}
	message := &parsemail.Email{
		From:   sender,
		Sender: sender,
		To: []*parsemail.User{
			{EmailAddress: challenge.Email},
		},
		Subject: subject,
		Text:    []byte(text),
		MsgID:   messageID,
	}
	ctx := &appcontext.Context{
		Context: deliveryContext,
		Values:  make(map[string]any),
	}
	if deliveryErr, _ := send.SendTriggered(ctx, message, "系统安全验证邮件"); deliveryErr != nil {
		return fmt.Errorf("%w: project mail delivery failed", verification.ErrMailDeliveryFailed)
	}
	return nil
}

// newPublicRecoveryLinkDelivery 固定使用项目自身邮件服务，不能走外部 SMTP 中继。
func newPublicRecoveryLinkDelivery() recovery.PublicLinkDelivery {
	return func(email, token string) error {
		mailDomain := strings.TrimSpace(config.CurrentMailDomains().Primary)
		webDomain := strings.TrimSpace(config.CurrentWebDomain())
		if mailDomain == "" || webDomain == "" || strings.TrimSpace(email) == "" || strings.TrimSpace(token) == "" {
			return verification.ErrProjectMailUnavailable
		}
		link, err := publicRecoveryLink(webDomain, token)
		if err != nil {
			return err
		}
		messageID, err := parsemail.GenerateMsgID(mailDomain)
		if err != nil {
			return fmt.Errorf("generate public recovery message id: %w", err)
		}
		sender := &parsemail.User{Name: "Smail 安全中心", EmailAddress: "no-reply@" + mailDomain}
		message := &parsemail.Email{
			From: sender, Sender: sender, To: []*parsemail.User{{EmailAddress: email}},
			Subject: "Smail 密码恢复链接",
			Text:    []byte("您好，您正在恢复 Smail 密码。请在五分钟内打开以下一次性链接完成重置：\r\n" + link + "\r\n\r\n如果并非本人操作，请忽略此邮件。"),
			MsgID:   messageID,
		}
		ctx := &appcontext.Context{Context: context.Background(), Values: make(map[string]any)}
		if deliveryErr, _ := send.SendTriggered(ctx, message, "系统密码恢复链接"); deliveryErr != nil {
			return fmt.Errorf("%w: project mail delivery failed", verification.ErrMailDeliveryFailed)
		}
		return nil
	}
}

func publicRecoveryLink(webDomain, token string) (string, error) {
	parsed, err := url.Parse("https://" + strings.TrimSpace(webDomain))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid web domain")
	}
	port := config.ResolvePublicHTTPSPort(config.CurrentHTTPConfig())
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("invalid public HTTPS port")
	}
	if port != 443 && parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(port))
	}
	parsed.Path = "/rest/"
	parsed.Fragment = "/reset-password/" + token
	return parsed.String(), nil
}
