package res_init

import (
	"context"
	"fmt"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/services/verification"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/send"
)

func newVerificationDelivery() verification.Delivery {
	return newVerificationDeliveryWithDependencies(
		config.CurrentSMTPRuntimeSettings,
		deliverVerificationWithSMTPRelay,
		deliverVerificationWithProjectMail,
	)
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
			return deliverRelay(runtimeSettings, challenge, code)
		}

		return deliverProject(challenge, code)
	}
}

func smtpRelayRequested(settings config.SMTPRuntimeSettings) bool {
	relay := settings.Relay
	return strings.TrimSpace(relay.RelayHost) != "" ||
		strings.TrimSpace(relay.Username) != "" ||
		strings.TrimSpace(settings.Password) != "" ||
		strings.TrimSpace(relay.From) != ""
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
	settings, err := verificationSMTPSettings(runtimeSettings)
	if err != nil {
		return err
	}
	delivery, err := verification.NewSMTPDelivery(settings)
	if err != nil {
		return fmt.Errorf("SMTP relay is unavailable: %w", err)
	}
	if err = delivery.Delivery()(challenge, code); err != nil {
		return fmt.Errorf("%w: SMTP relay rejected delivery", verification.ErrMailDeliveryFailed)
	}
	return nil
}

func deliverVerificationWithProjectMail(challenge verification.Challenge, code string) error {
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
		Context: context.Background(),
		Values:  make(map[string]any),
	}
	if deliveryErr, _ := send.Send(ctx, message); deliveryErr != nil {
		return fmt.Errorf("%w: project mail delivery failed", verification.ErrMailDeliveryFailed)
	}
	return nil
}
