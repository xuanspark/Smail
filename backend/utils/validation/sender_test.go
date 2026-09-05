package validation

import "testing"

func TestIsAuthorizedSender(t *testing.T) {
	domains := []string{"example.com", "mail.example.net"}
	tests := []struct {
		name    string
		address string
		account string
		admin   bool
		want    bool
	}{
		{name: "matching account", address: "alice@example.com", account: "alice", want: true},
		{name: "case insensitive account", address: "Alice@EXAMPLE.COM", account: "alice", want: true},
		{name: "external domain", address: "alice@external.example", account: "alice", want: false},
		{name: "different account", address: "bob@example.com", account: "alice", want: false},
		{name: "admin local alias", address: "alias@mail.example.net", account: "admin", admin: true, want: true},
		{name: "admin external domain", address: "alias@external.example", account: "admin", admin: true, want: false},
		{name: "display name rejected", address: "Alice <alice@example.com>", account: "alice", want: false},
		{name: "missing local part", address: "@example.com", account: "alice", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuthorizedSender(tt.address, tt.account, tt.admin, domains); got != tt.want {
				t.Fatalf("IsAuthorizedSender(%q) = %v, want %v", tt.address, got, tt.want)
			}
		})
	}
}
