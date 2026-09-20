package http_server

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const webShutdownTimeout = 10 * time.Second

// 先停止监听并等待在途响应及会话提交，超时才强制断开，避免重启接口丢失确认响应。
func shutdownWebServer(server *http.Server, timeout time.Duration) error {
	if server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return errors.Join(err, server.Close())
	}
	return nil
}
