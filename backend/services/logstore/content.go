package logstore

import (
	"encoding/json"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

// emptyEntry 只丢弃没有事件、对象、结果或有效载荷的空记录。
// 已知业务动作（包括无字段变化的配置审计）、失败和未知结果均不去重或省略。
func emptyEntry(entry Entry) bool {
	if !logpolicy.EmptyMessage(entry.Action) || strings.TrimSpace(entry.ActorID) != "" || strings.TrimSpace(entry.Section) != "" || strings.TrimSpace(entry.Result) != "" {
		return false
	}
	var payload any
	if json.Unmarshal(entry.Payload, &payload) != nil {
		return false
	}
	switch value := payload.(type) {
	case nil:
		return true
	case string:
		return logpolicy.EmptyMessage(value)
	case []any:
		return len(value) == 0
	case map[string]any:
		if len(value) == 0 {
			return true
		}
		// 只理解单一消息字段，其他键即使值为空也可能是状态证据。
		if len(value) == 1 {
			message, ok := value["message"].(string)
			return ok && logpolicy.EmptyMessage(message)
		}
	}
	return false
}
