package restart

import (
	"context"
	"errors"
)

var (
	ErrUnavailable = errors.New("restart service unavailable")
	ErrCanceled    = errors.New("restart request canceled")
)

// Trigger 接收固定的 Smail 重启动作，不接受服务名、命令或参数。
type Trigger interface {
	Request(context.Context) error
}

// Preview 描述固定的 Smail 重启目标，但不执行重启动作。
type Preview struct {
	Target           string   `json:"target"`
	Action           string   `json:"action"`
	ActiveGeneration uint64   `json:"activeGeneration"`
	PendingServices  []string `json:"pendingServices,omitempty"`
	ServiceManager   string   `json:"serviceManager"`
	TriggerAvailable bool     `json:"triggerAvailable"`
}

func BuildPreview(activeGeneration uint64, pendingServices []string, triggerAvailable bool) Preview {
	return Preview{
		Target:           "smail",
		Action:           "restart",
		ActiveGeneration: activeGeneration,
		PendingServices:  append([]string(nil), pendingServices...),
		ServiceManager:   "固定服务管理器",
		TriggerAvailable: triggerAvailable,
	}
}

type SignalTrigger struct {
	channel chan<- bool
}

func NewSignalTrigger(channel chan<- bool) *SignalTrigger {
	return &SignalTrigger{channel: channel}
}

func (t *SignalTrigger) Request(ctx context.Context) error {
	if t == nil || t.channel == nil {
		return ErrUnavailable
	}
	select {
	case t.channel <- true:
		return nil
	case <-ctx.Done():
		return ErrCanceled
	}
}
