package controllers

import (
	"testing"

	"git.pcux.cn/xuans/Smail/models"
)

func TestLoginVerificationPolicyEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		policy  string
		want    bool
	}{
		{name: "disabled", enabled: false, policy: "always", want: false},
		{name: "empty policy is disabled", enabled: true, policy: "", want: false},
		{name: "off policy is disabled", enabled: true, policy: "off", want: false},
		{name: "always policy is enabled", enabled: true, policy: "always", want: true},
		{name: "new device policy is enabled", enabled: true, policy: "new_device", want: true},
		{name: "policy is case insensitive", enabled: true, policy: "ALWAYS", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := loginVerificationPolicyEnabled(test.enabled, test.policy); got != test.want {
				t.Fatalf("login verification policy result = %v, want %v", got, test.want)
			}
		})
	}
}

func TestBeginLoginVerification_NoExternalEmail(t *testing.T) {
	user := &models.User{
		ID: 99999,
		Account: "test_no_email",
	}
	res, err := beginLoginVerification(user, "127.0.0.1")
	if err != nil {
		t.Fatalf("expected nil error for user without external email, got: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil challenge result for user without external email, got: %v", res)
	}
}
