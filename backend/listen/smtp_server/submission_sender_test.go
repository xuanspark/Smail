package smtp_server

import (
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func TestAuthorizedSubmissionEmailRejectsSenderSpoofing(t *testing.T) {
	oldConfig := config.Instance
	t.Cleanup(func() { config.Instance = oldConfig })
	config.Instance = &config.Config{Domains: []string{"example.com"}}
	ctx := &context.Context{UserID: 1, UserAccount: "alice"}

	valid := &parsemail.Email{
		From:   &parsemail.User{EmailAddress: "alice@example.com"},
		Sender: &parsemail.User{EmailAddress: "alice@example.com"},
	}
	if !authorizedSubmissionEmail(valid, "alice@example.com", ctx) {
		t.Fatal("valid submission sender was rejected")
	}

	tests := []struct {
		name         string
		from         string
		sender       string
		envelopeFrom string
	}{
		{name: "external header from", from: "alice@external.example", sender: "alice@example.com", envelopeFrom: "alice@example.com"},
		{name: "external sender", from: "alice@example.com", sender: "alice@external.example", envelopeFrom: "alice@example.com"},
		{name: "external envelope", from: "alice@example.com", sender: "alice@example.com", envelopeFrom: "alice@external.example"},
		{name: "different local account", from: "bob@example.com", sender: "bob@example.com", envelopeFrom: "bob@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			email := &parsemail.Email{
				From:   &parsemail.User{EmailAddress: tt.from},
				Sender: &parsemail.User{EmailAddress: tt.sender},
			}
			if authorizedSubmissionEmail(email, tt.envelopeFrom, ctx) {
				t.Fatal("spoofed submission sender was accepted")
			}
		})
	}
}

func TestAuthorizedSubmissionEmailLimitsAdminToLocalDomains(t *testing.T) {
	oldConfig := config.Instance
	t.Cleanup(func() { config.Instance = oldConfig })
	config.Instance = &config.Config{Domains: []string{"example.com"}}
	ctx := &context.Context{UserID: 1, UserAccount: "admin", Role: access.RoleAdmin}

	local := &parsemail.Email{
		From:   &parsemail.User{EmailAddress: "admin@example.com"},
		Sender: &parsemail.User{EmailAddress: "admin@example.com"},
	}
	if !authorizedSubmissionEmail(local, "admin@example.com", ctx) {
		t.Fatal("admin own sender was rejected")
	}
	local.From.EmailAddress = "alias@example.com"
	local.Sender.EmailAddress = "alias@example.com"
	if authorizedSubmissionEmail(local, "alias@example.com", ctx) {
		t.Fatal("admin local alias was accepted")
	}
	local.From.EmailAddress = "alias@external.example"
	local.Sender.EmailAddress = "alias@external.example"
	if authorizedSubmissionEmail(local, "alias@external.example", ctx) {
		t.Fatal("admin external sender domain was accepted")
	}

	superAdmin := &context.Context{UserID: 2, UserAccount: "root", Role: access.RoleSuperAdmin}
	local.From.EmailAddress = "alias@example.com"
	local.Sender.EmailAddress = "alias@example.com"
	if !authorizedSubmissionEmail(local, "alias@example.com", superAdmin) {
		t.Fatal("super admin local alias was rejected")
	}
	local.From.EmailAddress = "alias@external.example"
	local.Sender.EmailAddress = "alias@external.example"
	if authorizedSubmissionEmail(local, "alias@external.example", superAdmin) {
		t.Fatal("super admin external sender domain was accepted")
	}
}
