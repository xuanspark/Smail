package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/verification"
	"git.pcux.cn/xuans/Smail/utils/password"
)

// 新恢复入口不依赖登录 Cookie，每一步由短期一次性令牌关联。
func PublicPasswordRecoveryFlow(w http.ResponseWriter, req *http.Request) {
	fail := func(status int, message string) {
		writePublicRecoveryJSON(w, status, map[string]string{"message": message})
	}
	if !PublicMailRecoveryEnabled() || req == nil || req.Method != http.MethodPost {
		fail(http.StatusBadRequest, "密码重置功能未启用")
		return
	}
	limiter := emailRecoveryBeginIPAttempts
	key := limiter.ipKey(req)
	if limiter.blocked(key) || emailRecoveryFailureIPAttempts.blocked(emailRecoveryFailureIPAttempts.ipKey(req)) {
		fail(http.StatusTooManyRequests, "请求过于频繁，请稍后重试")
		return
	}
	limiter.recordFailure(key)
	var input struct {
		Identifier  string `json:"identifier"`
		Token       string `json:"token"`
		Method      string `json:"method"`
		Code        string `json:"code"`
		NewPassword string `json:"new_password"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		fail(http.StatusBadRequest, "请求格式无效")
		return
	}
	service := currentSecurityRuntime().recovery
	if service == nil {
		fail(http.StatusServiceUnavailable, "重置服务暂时不可用")
		return
	}
	var result any
	var err error
	switch strings.TrimSuffix(req.URL.Path, "/") {
	case "/rest/api/recovery/begin":
		result, err = service.IdentifyPublicFlow(input.Identifier)
	case "/rest/api/recovery/send-code":
		err = service.SendPublicFlowCode(input.Token, remoteIP(req))
		result = map[string]any{"sent": true, "retry_after": 60}
	case "/rest/api/recovery/verify":
		var token string
		token, err = service.VerifyPublicFlow(input.Token, input.Method, input.Code, remoteIP(req))
		result = map[string]string{"token": token}
	case "/rest/api/recovery/reset":
		err = service.CompletePublicFlow(input.Token, input.NewPassword)
		result = map[string]bool{"reset": true}
	default:
		http.NotFound(w, req)
		return
	}
	if err != nil {
		if errors.Is(err, password.ErrBusy) || errors.Is(err, recovery.ErrUnavailable) {
			fail(http.StatusServiceUnavailable, "重置服务暂时不可用，请稍后重试")
		} else if errors.Is(err, verification.ErrRateLimited) {
			fail(http.StatusTooManyRequests, "验证码发送过于频繁，请稍后重试")
		} else if strings.HasSuffix(req.URL.Path, "/send-code") {
			fail(http.StatusBadRequest, "验证码未发送，请检查邮箱绑定或稍后重试")
		} else if strings.HasSuffix(req.URL.Path, "/begin") {
			fail(http.StatusBadRequest, "账号不存在、已停用或无法使用此通道")
		} else if errors.Is(err, recovery.ErrInvalidNewPassword) {
			fail(http.StatusBadRequest, "新密码长度必须为 8-12 字节")
		} else {
			emailRecoveryFailureIPAttempts.recordFailure(emailRecoveryFailureIPAttempts.ipKey(req))
			fail(http.StatusBadRequest, "验证未通过或流程已失效，请核对信息或重新开始")
		}
		return
	}
	writePublicRecoveryJSON(w, http.StatusOK, result)
}
