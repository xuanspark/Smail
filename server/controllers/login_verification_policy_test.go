package controllers

import "testing"

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
