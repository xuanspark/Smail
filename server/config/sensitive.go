package config

import "strings"

type PersistencePolicy string

const (
	PersistenceEncryptedOnly PersistencePolicy = "encrypted_only"
	PersistenceReferenceOnly PersistencePolicy = "reference_only"
	PersistenceMemoryOnly    PersistencePolicy = "memory_only"
)

type SensitiveField struct {
	Path   string
	Policy PersistencePolicy
}

var sensitiveFields = []SensitiveField{
	{Path: "database.password", Policy: PersistenceEncryptedOnly},
	{Path: "smtp.relay.password", Policy: PersistenceEncryptedOnly},
	{Path: "plugins.*.token", Policy: PersistenceEncryptedOnly},
	{Path: "plugins.*.secret", Policy: PersistenceEncryptedOnly},
	{Path: "plugins.*.api_key", Policy: PersistenceEncryptedOnly},
	{Path: "ssl.private_key", Policy: PersistenceReferenceOnly},
	{Path: "security.encryption_master_key", Policy: PersistenceReferenceOnly},
	{Path: "security.question_answer", Policy: PersistenceEncryptedOnly},
	{Path: "security.offline_recovery_code", Policy: PersistenceEncryptedOnly},
	{Path: "security.maintenance_credential", Policy: PersistenceMemoryOnly},
	{Path: "verification.code", Policy: PersistenceMemoryOnly},
	{Path: "verification.code_hmac", Policy: PersistenceMemoryOnly},
	{Path: "verification.process_key", Policy: PersistenceMemoryOnly},
	{Path: "recovery.authorization", Policy: PersistenceMemoryOnly},
}

func SensitiveFields() []SensitiveField {
	result := make([]SensitiveField, len(sensitiveFields))
	copy(result, sensitiveFields)
	return result
}

func ClassifySensitive(path string) (SensitiveField, bool) {
	path = strings.TrimSpace(path)
	for _, field := range sensitiveFields {
		if matchSensitivePath(field.Path, path) {
			return field, true
		}
	}
	return SensitiveField{}, false
}

func CanGenericSerialize(path string) bool {
	_, sensitive := ClassifySensitive(path)
	return !sensitive
}

func CanPersist(path string, encrypted bool) bool {
	field, sensitive := ClassifySensitive(path)
	if !sensitive {
		return true
	}
	switch field.Policy {
	case PersistenceEncryptedOnly:
		return encrypted
	case PersistenceReferenceOnly, PersistenceMemoryOnly:
		return false
	default:
		return false
	}
}

func matchSensitivePath(pattern string, value string) bool {
	patternParts := strings.Split(pattern, ".")
	valueParts := strings.Split(value, ".")
	if len(patternParts) != len(valueParts) {
		return false
	}
	for i := range patternParts {
		if patternParts[i] != "*" && patternParts[i] != valueParts[i] {
			return false
		}
	}
	return true
}
