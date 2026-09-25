package validation

import "strings"

// 加号不属于可注册账号字符，保留地址不会占用普通用户的邮箱。
const NotificationLocalPart = "notification+system"

func NotificationAddress(domain string) string {
	if domain == "" {
		return ""
	}
	return NotificationLocalPart + "@" + domain
}

func IsNotificationAddress(address string, domains []string) bool {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(address)), "@")
	if len(parts) != 2 || parts[0] != NotificationLocalPart {
		return false
	}
	for _, domain := range domains {
		if strings.EqualFold(parts[1], domain) {
			return true
		}
	}
	return false
}
