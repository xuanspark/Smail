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
