package validation

import (
	"net/mail"
	"strings"
)

func IsAuthorizedSender(address, account string, allowLocalAlias bool, domains []string) bool {
	if IsNotificationAddress(address, domains) {
		return false
	}
	address = strings.TrimSpace(address)
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address {
		return false
	}

	at := strings.LastIndexByte(address, '@')
	if at <= 0 || at == len(address)-1 || strings.Contains(address[:at], "@") {
		return false
	}

	domain, err := NormalizeDomain(address[at+1:])
	if err != nil {
		return false
	}
	configured := false
	for _, candidate := range domains {
		normalized, normalizeErr := NormalizeDomain(candidate)
		if normalizeErr == nil && domain == normalized {
			configured = true
			break
		}
	}
	if !configured {
		return false
	}

	return allowLocalAlias || strings.EqualFold(address[:at], strings.TrimSpace(account))
}
