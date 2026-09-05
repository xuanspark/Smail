package address

import (
	"net/mail"
	"strings"
)

// IsValidEmailAddress 检查是否是有效的邮箱地址
func IsValidEmailAddress(str string) bool {
	parsed, err := mail.ParseAddress(str)
	if err != nil || parsed.Address != str {
		return false
	}

	separator := strings.LastIndexByte(str, '@')
	if separator <= 0 || separator == len(str)-1 {
		return false
	}

	localPart := str[:separator]
	domain := str[separator+1:]
	if strings.Contains(localPart, "..") || strings.HasPrefix(localPart, ".") || strings.HasSuffix(localPart, ".") {
		return false
	}
	if strings.Contains(domain, "..") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	return strings.Contains(domain, ".")
}
