package validation

import (
	"errors"
	"net"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxAccountBytes = 20
	MaxNameRunes    = 10
	MaxDomainCount  = 100
)

func NormalizeAccount(value string) (string, error) {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > MaxAccountBytes {
		return "", errors.New("invalid account length")
	}
	for index, char := range []byte(value) {
		isLetter := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
		isDigit := char >= '0' && char <= '9'
		if !isLetter && !isDigit && char != '.' && char != '_' && char != '-' {
			return "", errors.New("invalid account character")
		}
		if (index == 0 || index == len(value)-1) && !isLetter && !isDigit {
			return "", errors.New("invalid account boundary")
		}
	}
	return value, nil
}

func NormalizeDisplayName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxNameRunes {
		return "", errors.New("invalid display name length")
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return "", errors.New("invalid display name character")
		}
	}
	return value, nil
}

func NormalizeDomain(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil || strings.ContainsAny(value, ":/@\\?#") {
		return "", errors.New("invalid domain")
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid domain label")
		}
		for _, char := range []byte(label) {
			isLetter := char >= 'a' && char <= 'z'
			isDigit := char >= '0' && char <= '9'
			if !isLetter && !isDigit && char != '-' {
				return "", errors.New("invalid domain character")
			}
		}
	}
	return value, nil
}

func NormalizeDomainList(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > MaxDomainCount {
		return nil, errors.New("too many domains")
	}
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		domain, err := NormalizeDomain(part)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[domain]; exists {
			continue
		}
		seen[domain] = struct{}{}
		result = append(result, domain)
	}
	return result, nil
}
