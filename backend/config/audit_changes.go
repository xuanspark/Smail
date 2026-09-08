package config

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// ConfigAuditChange 只保存安全的显示值，不保存密文或请求原文。
type ConfigAuditChange struct {
	Field string `json:"field"`
	Before string `json:"before"`
	After string `json:"after"`
	Sensitive bool `json:"sensitive,omitempty"`
}

// AuditChanges 对比业务字段，排除代次、摘要和内部持久化元数据。
func AuditChanges(before, after RuntimeConfig) []ConfigAuditChange {
	left := auditFields(before)
	right := auditFields(after)
	keys := make(map[string]bool)
	for key := range left { keys[key] = true }
	for key := range right { keys[key] = true }
	ordered := make([]string, 0, len(keys))
	for key := range keys { ordered = append(ordered, key) }
	sort.Strings(ordered)
	changes := make([]ConfigAuditChange, 0)
	for _, key := range ordered {
		if reflect.DeepEqual(left[key], right[key]) { continue }
		change := ConfigAuditChange{Field: key, Before: auditDisplay(left[key]), After: auditDisplay(right[key])}
		if auditSensitive(key) {
			change.Sensitive = true
			change.Before, change.After = "未配置", "未配置"
			if auditConfigured(left[key]) { change.Before = "已配置（隐藏）" }
			if auditConfigured(right[key]) { change.After = "已配置（隐藏，已更新）" }
		}
		changes = append(changes, change)
	}
	return changes
}

func auditFields(runtime RuntimeConfig) map[string]any {
	result := map[string]any{}
	for key, value := range map[string]any{
		"root": runtime.Root, "smtp": runtime.SMTP, "email": runtime.Email,
		"ssl": runtime.SSL, "plugins": runtime.Plugins,
	} {
		data, _ := json.Marshal(value)
		var object any
		_ = json.Unmarshal(data, &object)
		flattenAudit(result, key, object)
	}
	return result
}

func flattenAudit(result map[string]any, path string, value any) {
	name := path[strings.LastIndex(path, ".")+1:]
	switch name {
	case "meta", "sections", "generation", "updatedAt", "schemaVersion", "configVersion", "version":
		return
	}
	if path == "root.smtp" || path == "root.email" || path == "root.ssl" || path == "root.plugins" { return }
	if auditSensitive(path) { result[path] = value; return }
	if object, ok := value.(map[string]any); ok {
		for key, item := range object { flattenAudit(result, path+"."+key, item) }
		return
	}
	result[path] = value
}

func auditSensitive(path string) bool {
	name := strings.ToLower(path[strings.LastIndex(path, ".")+1:])
	for _, word := range []string{"password", "secret", "token", "apikey", "api_key", "credential", "ciphertext", "authorization", "cookie"} {
		if strings.Contains(name, word) { return true }
	}
	// 插件设置是自由结构，未知键也不能作为明文审计输出。
	return strings.HasSuffix(path, ".settings") || strings.HasSuffix(path, ".encryptionKeyRef")
}

func auditConfigured(value any) bool {
	if value == nil || value == "" { return false }
	if object, ok := value.(map[string]any); ok {
		if ciphertext, exists := object["ciphertext"]; exists { return ciphertext != "" }
		return len(object) > 0
	}
	return true
}

func auditDisplay(value any) string {
	if value == nil { return "未设置" }
	if text, ok := value.(string); ok {
		if len([]rune(text)) > 512 { return string([]rune(text)[:512])+"…" }
		return text
	}
	if flag, ok := value.(bool); ok { if flag { return "开启" }; return "关闭" }
	data, _ := json.Marshal(value)
	text := []rune(string(data))
	if len(text) > 512 { return string(text[:512])+"…" }
	return string(text)
}
