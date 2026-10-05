package config

import (
	"context"
	"encoding/json"
	"testing"
)

func TestConfigManagerRequiresExplicitIndependentRelaySecurity(t *testing.T) {
	for _, section := range []Section{SectionSMTP, SectionMail} {
		for _, listenerSecurity := range []string{MailSecurityBoth, MailSecuritySTARTTLS, MailSecurityImplicitTLS} {
			for _, relaySecurity := range []string{"", MailSecuritySTARTTLS, MailSecurityImplicitTLS} {
				t.Run(string(section)+"_"+listenerSecurity+"_"+relaySecurity, func(t *testing.T) {
					root := t.TempDir()
					writeRuntimeFixture(t, root, validRuntimeConfig())
					options := ConfigLoaderOptions{Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: true}}
					manager, err := NewConfigManager(root, options)
					if err != nil {
						t.Fatal(err)
					}
					mail := mailSectionFromRuntime(manager.Snapshot())
					mail.Listener.Security = listenerSecurity
					mail.Relay = SMTPConfig{
						RelayEnabled: true,
						RelayHost:    "smtp.example.com",
						RelayPort:    2525,
						Security:     relaySecurity,
						From:         "sender@example.com",
						FromDomain:   "example.com",
					}
					var value any = mail
					if section == SectionSMTP {
						value = SMTPSectionConfig{Meta: mail.Meta, Listener: mail.Listener, Relay: mail.Relay}
					}
					payload, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					updated, err := manager.Update(context.Background(), UpdateRequest{
						Section: section, ExpectedGeneration: 1, Payload: payload, Actor: authorizedConfigActor(),
					})
					if relaySecurity == "" {
						if err == nil {
							t.Fatal("enabled relay without an explicit security mode must be rejected")
						}
						if manager.Snapshot().SMTP.Relay.RelayEnabled {
							t.Fatal("invalid update changed active relay")
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if _, err := manager.ApplyPendingAuthorized(context.Background(), updated.Generation, updated.CandidateID, section, authorizedConfigActor()); err != nil {
						t.Fatal(err)
					}
					reloaded, err := NewConfigLoader(root, options).Load()
					if err != nil {
						t.Fatal(err)
					}
					if reloaded.SMTP.Relay != mail.Relay || reloaded.SMTP.Listener.Security != listenerSecurity {
						t.Fatal("independent relay or listener configuration changed after reload")
					}
				})
			}
		}
	}
}
