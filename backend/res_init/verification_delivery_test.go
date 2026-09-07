package res_init

import (
	"errors"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/verification"
)

func TestVerificationDeliveryUsesProjectMailWithoutRelay(t *testing.T) {
	settings := config.SMTPRuntimeSettings{
		Relay: config.SMTPConfig{
			RelayPort:  587,
			Security:   "starttls",
			FromDomain: "example.com",
		},
	}
	relayCalled := false
	projectCalled := false
	delivery := newVerificationDeliveryWithDependencies(
		func() (config.SMTPRuntimeSettings, error) { return settings, nil },
		func(config.SMTPRuntimeSettings, verification.Challenge, string) error {
			relayCalled = true
			return nil
		},
		func(challenge verification.Challenge, code string) error {
			projectCalled = challenge.Email == "user@example.net" && code == "12345678"
			return nil
		},
	)

	if err := delivery(verification.Challenge{Email: "user@example.net"}, "12345678"); err != nil {
		t.Fatal(err)
	}
	if relayCalled || !projectCalled {
		t.Fatalf("relayCalled=%v projectCalled=%v", relayCalled, projectCalled)
	}
}

func TestVerificationDeliveryDoesNotHidePartialRelayConfiguration(t *testing.T) {
	expected := verification.ErrInvalidSMTPConfig
	projectCalled := false
	delivery := newVerificationDeliveryWithDependencies(
		func() (config.SMTPRuntimeSettings, error) {
			return config.SMTPRuntimeSettings{Relay: config.SMTPConfig{RelayHost: "smtp.example.com"}}, nil
		},
		func(config.SMTPRuntimeSettings, verification.Challenge, string) error { return expected },
		func(verification.Challenge, string) error {
			projectCalled = true
			return nil
		},
	)

	err := delivery(verification.Challenge{Email: "user@example.net"}, "12345678")
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v, want %v", err, expected)
	}
	if projectCalled {
		t.Fatal("partial relay configuration unexpectedly fell back to project mail")
	}
}

func TestVerificationDeliveryPreservesSettingsError(t *testing.T) {
	expected := errors.New("settings unavailable")
	delivery := newVerificationDeliveryWithDependencies(
		func() (config.SMTPRuntimeSettings, error) { return config.SMTPRuntimeSettings{}, expected },
		func(config.SMTPRuntimeSettings, verification.Challenge, string) error { return nil },
		func(verification.Challenge, string) error { return nil },
	)

	err := delivery(verification.Challenge{Email: "user@example.net"}, "12345678")
	if !errors.Is(err, expected) {
		t.Fatalf("error = %v, want wrapped settings error", err)
	}
}
