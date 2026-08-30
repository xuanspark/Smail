package restart

import (
	"context"
	"errors"
	"testing"
)

func TestSignalTriggerRequest(t *testing.T) {
	channel := make(chan bool, 1)
	trigger := NewSignalTrigger(channel)
	if err := trigger.Request(context.Background()); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	select {
	case value := <-channel:
		if !value {
			t.Fatal("restart signal was false")
		}
	default:
		t.Fatal("restart signal was not sent")
	}
}

func TestSignalTriggerUnavailable(t *testing.T) {
	if err := (*SignalTrigger)(nil).Request(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
	if err := NewSignalTrigger(nil).Request(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable error for nil channel, got %v", err)
	}
}

func TestSignalTriggerCanceled(t *testing.T) {
	channel := make(chan bool)
	trigger := NewSignalTrigger(channel)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := trigger.Request(ctx); !errors.Is(err, ErrCanceled) {
		t.Fatalf("expected canceled error, got %v", err)
	}
}

func TestCurrentTriggerRegistry(t *testing.T) {
	SetCurrent(nil)
	if Current() != nil {
		t.Fatal("expected empty current trigger")
	}
	first := NewSignalTrigger(make(chan bool, 1))
	second := NewSignalTrigger(make(chan bool, 1))
	SetCurrent(first)
	if Current() != first {
		t.Fatal("first trigger was not registered")
	}
	SetCurrent(second)
	if Current() != second {
		t.Fatal("current trigger was not replaced")
	}
	SetCurrent(nil)
	if Current() != nil {
		t.Fatal("current trigger was not cleared")
	}
}

func TestBuildPreviewUsesFixedRestartTarget(t *testing.T) {
	pending := []string{"smtp", "imap"}
	preview := BuildPreview(9, pending, false)
	if preview.Target != "smail" || preview.Action != "restart" || preview.ActiveGeneration != 9 || preview.ServiceManager != "固定服务管理器" {
		t.Fatalf("unexpected restart preview: %+v", preview)
	}
	if preview.TriggerAvailable || len(preview.PendingServices) != 2 {
		t.Fatalf("unexpected restart preview availability: %+v", preview)
	}
	preview.PendingServices[0] = "changed"
	if pending[0] == "changed" {
		t.Fatal("preview shares pending service slice")
	}
}
