package parsemail

import (
	"bytes"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-message/mail"
)

func TestForwardBuildBytesRemailsFromLocalUser(t *testing.T) {
	oldConfig := config.Instance
	oldDkim := instance
	defer func() {
		config.Instance = oldConfig
		instance = oldDkim
	}()

	config.Instance = &config.Config{Domain: "example.com", Domains: []string{"example.com"}}
	instance = nil

	email := &Email{
		From:    &User{Name: "Original Sender", EmailAddress: "sender@example.com"},
		To:      []*User{{EmailAddress: "forwarder@example.com"}},
		Subject: "hello",
		Text:    []byte("forward body"),
		MsgID:   "original-message@example.com",
	}

	data, err := email.ForwardBuildBytes(&context.Context{}, &models.User{Name: "forwarder", Account: "forwarder"}, "recipient@example.net")
	if err != nil {
		t.Fatal(err)
	}

	msg, err := mail.CreateReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer msg.Close()

	from, err := msg.Header.AddressList("From")
	if err != nil {
		t.Fatal(err)
	}
	if len(from) != 1 || from[0].Address != "forwarder@example.com" {
		t.Fatalf("expected forwarded From to be local user, got %+v", from)
	}

	to, err := msg.Header.AddressList("To")
	if err != nil {
		t.Fatal(err)
	}
	if len(to) != 1 || to[0].Address != "recipient@example.net" {
		t.Fatalf("expected forwarded To to be target address, got %+v", to)
	}

	replyTo, err := msg.Header.AddressList("Reply-To")
	if err != nil {
		t.Fatal(err)
	}
	if len(replyTo) != 1 || replyTo[0].Address != "sender@example.com" {
		t.Fatalf("expected Reply-To to preserve original sender, got %+v", replyTo)
	}

	if got := msg.Header.Get("X-Original-From"); !strings.Contains(got, "sender@example.com") {
		t.Fatalf("expected X-Original-From to include original sender, got %q", got)
	}
	if got := msg.Header.Get("X-Forwarded-By"); got != "forwarder@example.com" {
		t.Fatalf("expected X-Forwarded-By to be local user, got %q", got)
	}
	if got := msg.Header.Get("X-Forwarded-To"); got != "recipient@example.net" {
		t.Fatalf("expected X-Forwarded-To to be target, got %q", got)
	}
	if got := msg.Header.Get("Subject"); got != "hello" {
		t.Fatalf("expected original subject, got %q", got)
	}
}
