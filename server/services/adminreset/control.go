package adminreset

import "context"

// ControlServer 是部署端重置通道的最小生命周期接口。
type ControlServer interface {
	Serve(context.Context, ResetHandler, *SessionGuard) error
	Close() error
}
