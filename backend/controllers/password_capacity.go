package controllers

import (
	"errors"
	"net/http"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/utils/password"
)

// 计算繁忙不是凭据错误，不计入失败次数，也不触发会话失效。
func writePasswordBusy(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, password.ErrBusy) {
		return false
	}
	w.Header().Set("Retry-After", "1")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	response.NewErrorResponse(http.StatusServiceUnavailable, "认证服务繁忙，请稍后重试", response.ConfigErrorData{Code: "AUTH_BUSY"}).FPrint(w)
	return true
}
