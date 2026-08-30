package response

// ConfigErrorData 是配置 API 的统一错误载荷，不包含底层错误、路径、密文或第三方响应正文。
type ConfigErrorData struct {
	Code      string            `json:"code"`
	RequestID string            `json:"requestId"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// ConfigErrorNumber 保留旧 errorNo 外壳，同时让 HTTP 状态承担标准语义。
func ConfigErrorNumber(httpStatus int) int {
	switch {
	case httpStatus >= 500:
		return ServerError
	case httpStatus == 401:
		return NeedLogin
	case httpStatus == 403:
		return NoAccessPrivileges
	default:
		return ParamsError
	}
}
