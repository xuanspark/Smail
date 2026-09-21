package controllers

import (
	"context"
	"net/http"
)

// recoveryRequestContext 保留请求的取消信号；兼容无 HTTP 请求的内部调用。
func recoveryRequestContext(req *http.Request) context.Context {
	if req == nil {
		return context.Background()
	}
	return req.Context()
}
