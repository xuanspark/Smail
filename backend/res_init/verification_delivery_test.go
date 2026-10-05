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
			return config.SMTPRuntimeSettings{Relay: config.SMTPConfig{RelayEnabled: true, RelayHost: "smtp.example.com"}}, nil
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

func TestPublicRecoveryLinkUsesWebDomain(t *testing.T) {
	link, err := publicRecoveryLink("web.example.com", "one-time-token")
	if err != nil {
		t.Fatal(err)
	}
	if link != "https://web.example.com/rest/#/reset-password/one-time-token" {
		t.Fatalf("public recovery link = %q", link)
	}
	if _, err := publicRecoveryLink("https://web.example.com/path", "one-time-token"); err == nil {
		t.Fatal("invalid web domain was accepted")
	}
}

func TestPublicRecoveryMailLinkUsesPublicHTTPSPort(t *testing.T) {
	oldConfig, oldManager := config.Instance, config.CurrentManager()
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{HttpsPort: 8002}
	t.Cleanup(func() {
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
	})
	for _, test := range []struct {
		port int
		want string
	}{
		{443, "https://web.example.com/rest/#/reset-password/one-time-token"},
		{9443, "https://web.example.com:9443/rest/#/reset-password/one-time-token"},
		{0, "https://web.example.com:8002/rest/#/reset-password/one-time-token"},
	} {
		config.Instance.PublicHTTPSPort = test.port
		link, err := publicRecoveryLink("web.example.com", "one-time-token")
		if err != nil || link != test.want {
			t.Fatalf("恢复邮件公网链接不匹配：%q，%v", link, err)
		}
	}
	for _, port := range []int{-1, 65536} {
		config.Instance.PublicHTTPSPort = port
		if _, err := publicRecoveryLink("web.example.com", "one-time-token"); err == nil {
			t.Fatal("非法公网端口仍生成了邮件恢复链接")
		}
	}
}
