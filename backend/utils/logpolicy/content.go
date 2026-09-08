package logpolicy

import "strings"

// EmptyMessage 仅识别空白和纯装饰线，不猜测业务文案、状态或未知格式的价值。
func EmptyMessage(message string) bool {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\x00", ""))
	if message == "" {
		return true
	}
	return len([]rune(message)) >= 3 && strings.Trim(message, "*-=─━_~ \t\r\n") == ""
}
