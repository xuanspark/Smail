package adminreset

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

type delayedEmailFlow struct {
	testResetFlow
	start func() error
}

func (f *delayedEmailFlow) Methods() []Method {
	return []Method{{ID: "email:1", Label: "发送验证码到 root@example.com"}}
}

func (f *delayedEmailFlow) Start(method string) error {
	if method != "email:1" {
		return ErrInvalidRequest
	}
	if err := f.start(); err != nil {
		return err
	}
	f.selected = true
	return nil
}

func TestEmailPromptDoesNotWaitForDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	inputShown := make(chan struct{})
	reset := false
	flow := &delayedEmailFlow{
		testResetFlow: testResetFlow{reset: func(string) error { reset = true; return nil }},
		start: func() error {
			select {
			case <-inputShown:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		handleResetConnection(server, server.SetDeadline, func() (ResetSession, error) { return flow, nil }, NewSessionGuard(time.Minute))
	}()
	steps := 0
	err := SendVerifiedResetWithInput(ctx, client, func(_ context.Context, prompt string) (string, error) {
		steps++
		if steps == 1 {
			return "1", nil
		}
		if !strings.Contains(prompt, "正在发送") || !strings.Contains(prompt, "请输入邮件验证码") {
			t.Errorf("缺少邮件发送状态或输入提示：%q", prompt)
		}
		close(inputShown)
		return "fixture-code", nil
	}, func() (string, error) { return "NewPass123", nil })
	<-done
	if err != nil || !reset {
		t.Fatalf("邮件输入与发送未正确衔接：reset=%v err=%v", reset, err)
	}
}

func TestEmailDeliveryFailureCancelsInputAndNeverAsksPassword(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	inputShown := make(chan struct{})
	flow := &delayedEmailFlow{start: func() error {
		select {
		case <-inputShown:
			return errors.New("mail unavailable")
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		handleResetConnection(server, server.SetDeadline, func() (ResetSession, error) { return flow, nil }, NewSessionGuard(time.Minute))
	}()
	steps := 0
	err := SendVerifiedResetWithInput(ctx, client, func(inputContext context.Context, _ string) (string, error) {
		steps++
		if steps == 1 {
			return "1", nil
		}
		close(inputShown)
		<-inputContext.Done()
		return "", inputContext.Err()
	}, func() (string, error) {
		t.Error("邮件发送失败后不应进入密码输入")
		return "NewPass123", nil
	})
	<-done
	if !errors.Is(err, ErrEmailDeliveryFailed) || ctx.Err() != nil {
		t.Fatalf("邮件发送失败未及时取消输入：%v", err)
	}
}
