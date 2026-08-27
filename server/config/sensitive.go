package config

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

type PersistencePolicy string

const (
	PersistenceEncryptedOnly PersistencePolicy = "encrypted_only"
	PersistenceReferenceOnly PersistencePolicy = "reference_only"
	PersistenceMemoryOnly    PersistencePolicy = "memory_only"
	PersistenceHashedOnly    PersistencePolicy = "hashed_only"
)

const RedactedValue = "[REDACTED]"

type SensitiveField struct {
	Path        string
	Policy      PersistencePolicy
	Description string
}

var sensitiveFields = []SensitiveField{
	{Path: "database.password", Policy: PersistenceEncryptedOnly, Description: "数据库认证密码"},
	{Path: "smtp.relay.password", Policy: PersistenceEncryptedOnly, Description: "SMTP Relay 认证密码"},
	{Path: "plugins.*.token", Policy: PersistenceEncryptedOnly, Description: "插件访问令牌"},
	{Path: "plugins.*.secret", Policy: PersistenceEncryptedOnly, Description: "插件 Secret"},
	{Path: "plugins.*.api_key", Policy: PersistenceEncryptedOnly, Description: "插件 API 密钥"},
	{Path: "ssl.private_key", Policy: PersistenceReferenceOnly, Description: "SSL 私钥材料"},
	{Path: "security.encryption_master_key", Policy: PersistenceReferenceOnly, Description: "配置加密主密钥"},
	{Path: "security.question_answer", Policy: PersistenceEncryptedOnly, Description: "密保答案原文"},
	{Path: "security.offline_recovery_code", Policy: PersistenceMemoryOnly, Description: "离线恢复码原文"},
	{Path: "security.maintenance_credential", Policy: PersistenceMemoryOnly, Description: "维护凭据"},
	{Path: "verification.code", Policy: PersistenceMemoryOnly, Description: "邮箱验证码原文"},
	{Path: "verification.code_hmac", Policy: PersistenceMemoryOnly, Description: "验证码 HMAC 校验值"},
	{Path: "verification.process_key", Policy: PersistenceMemoryOnly, Description: "验证码进程临时密钥"},
	{Path: "verification.challenge", Policy: PersistenceMemoryOnly, Description: "验证码挑战上下文"},
	{Path: "verification.account_fingerprint", Policy: PersistenceHashedOnly, Description: "验证码账号运营指纹"},
	{Path: "verification.email_fingerprint", Policy: PersistenceHashedOnly, Description: "验证码邮箱运营指纹"},
	{Path: "verification.ip_fingerprint", Policy: PersistenceHashedOnly, Description: "验证码 IP 运营指纹"},
	{Path: "verification.rate_limit_fingerprint", Policy: PersistenceHashedOnly, Description: "验证码限流指纹"},
	{Path: "recovery.authorization", Policy: PersistenceMemoryOnly, Description: "恢复授权上下文"},
	{Path: "recovery.ticket", Policy: PersistenceMemoryOnly, Description: "密码恢复票据"},
	{Path: "recovery.code", Policy: PersistenceMemoryOnly, Description: "恢复码原文"},
	{Path: "external_email.email", Policy: PersistenceEncryptedOnly, Description: "完整外部安全邮箱"},
	{Path: "external_email.address", Policy: PersistenceEncryptedOnly, Description: "完整外部邮箱地址"},
	{Path: "external_email.credential", Policy: PersistenceEncryptedOnly, Description: "外部邮箱凭据材料"},
	{Path: "webauthn.credential_data", Policy: PersistenceEncryptedOnly, Description: "WebAuthn 凭据私密材料"},
	{Path: "webauthn.private_key", Policy: PersistenceMemoryOnly, Description: "WebAuthn 私钥材料"},
	{Path: "ipc.nonce", Policy: PersistenceMemoryOnly, Description: "本机 IPC 一次性 nonce"},
	{Path: "ipc.maintenance_credential", Policy: PersistenceMemoryOnly, Description: "本机维护通道凭据"},
	{Path: "session.token", Policy: PersistenceReferenceOnly, Description: "会话令牌"},
	{Path: "session.data", Policy: PersistenceEncryptedOnly, Description: "会话数据"},
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
	case PersistenceHashedOnly:
		return false
	default:
		return false
	}
}

// CanPersistRepresentation 判断敏感数据只能以哪一种表示形式进入持久化介质。
// verification.code 等内存字段即使经过哈希或加密也不得落盘。
func CanPersistRepresentation(path string, representation string) bool {
	field, sensitive := ClassifySensitive(path)
	if !sensitive {
		return true
	}
	switch field.Policy {
	case PersistenceEncryptedOnly:
		return representation == "encrypted"
	case PersistenceReferenceOnly:
		return representation == "reference"
	case PersistenceHashedOnly:
		return representation == "hash"
	case PersistenceMemoryOnly:
		return representation == "memory"
	default:
		return false
	}
}

// RedactSensitiveValue 为公共输出提供统一的敏感值占位符。
func RedactSensitiveValue(path string, value any) any {
	if _, ok := ClassifySensitive(path); ok {
		return RedactedValue
	}
	return value
}

// RedactPublicValue 递归生成只读公共视图，避免把 Secret 结构直接交给 JSON 反射器。
// 持久化配置必须继续使用原始结构和专用存储路径，不能把该函数用于写盘。
func RedactPublicValue(value any) any {
	return redactPublicValue(reflect.ValueOf(value), "")
}

func redactPublicValue(value reflect.Value, path string) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return redactPublicValue(value.Elem(), path)
	}
	if path != "" {
		if _, ok := ClassifySensitive(path); ok {
			return RedactedValue
		}
	}
	if value.Type() == reflect.TypeOf(time.Time{}) {
		return value.Interface()
	}
	switch value.Kind() {
	case reflect.Struct:
		result := make(map[string]any)
		typ := value.Type()
		for index := 0; index < value.NumField(); index++ {
			field := typ.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := jsonFieldName(field.Tag.Get("json"), field.Name)
			if name == "" || name == "-" {
				continue
			}
			fieldPath := joinSensitivePath(path, name)
			result[name] = redactPublicValue(value.Field(index), fieldPath)
		}
		return result
	case reflect.Map:
		result := make(map[string]any)
		iter := value.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			result[key] = redactPublicValue(iter.Value(), joinSensitivePath(path, key))
		}
		return result
	case reflect.Slice, reflect.Array:
		result := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			result[index] = redactPublicValue(value.Index(index), path)
		}
		return result
	default:
		return value.Interface()
	}
}

func jsonFieldName(tag, fallback string) string {
	if tag == "" {
		return fallback
	}
	return strings.Split(tag, ",")[0]
}

func joinSensitivePath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
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
