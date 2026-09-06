package controllers

import (
	"encoding/json"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestPublicMailPortsMatchListenersWithoutChangingSnapshot(t *testing.T) {
	for _, custom := range []bool{false, true} {
		runtime := config.RuntimeConfig{
			SMTP:  config.SMTPSectionConfig{Relay: config.SMTPConfig{RelayPort: 9999}},
			Email: config.EmailSectionConfig{Policy: config.EmailConfig{SMTPPort: 2525, IMAPPort: 1143, POP3Port: 1110}},
		}
		if custom {
			runtime.SMTP.Listener = config.SMTPListenerConfig{Port: 3025, STARTTLSPort: 3587, TLSPort: 3465}
			runtime.Email.IMAP.TLSPort = 3993
			runtime.Email.POP3 = config.POP3ListenerConfig{Port: 3110, TLSPort: 3995}
		}
		before := runtime.Email
		want := config.ResolveMailPorts(runtime.SMTP, runtime.Email)
		smtpPublic, err := publicConfigSection(runtime, config.SectionSMTP)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(smtpPublic)
		if err != nil {
			t.Fatal(err)
		}
		var smtp struct {
			Listener config.SMTPListenerConfig `json:"listener"`
		}
		if err := json.Unmarshal(data, &smtp); err != nil {
			t.Fatal(err)
		}
		emailPublic, err := publicConfigSection(runtime, config.SectionEmail)
		if err != nil {
			t.Fatal(err)
		}
		email := emailPublic.(config.EmailSectionConfig)
		if smtp.Listener.Port != want.SMTP || smtp.Listener.STARTTLSPort != want.SMTPSTARTTLS || smtp.Listener.TLSPort != want.SMTPTLS || email.IMAP.TLSPort != want.IMAP || email.POP3.Port != want.POP3 || email.POP3.TLSPort != want.POP3TLS {
			t.Fatal("public ports differ from listener configuration")
		}
		if before != runtime.Email {
			t.Fatal("public view changed runtime configuration")
		}
	}
}
