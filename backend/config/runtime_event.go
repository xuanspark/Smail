package config

import (
	"strconv"
	"strings"

	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

// runtimeEventFields 为文件日志提供稳定的事件边界；没有完成信号时只记为状态记录。
// 不从任意消息中的“成功”或日志级别猜测业务结果，也不改变调用方的字段。
func runtimeEventFields(entry *log.Entry) map[string]any {
	fields := make(map[string]any, len(entry.Data)+4)
	for key, value := range entry.Data {
		fields[key] = value
	}
	fields["logSchema"] = 1
	// 归属只来自服务端认证上下文，调用点不能用任意字段冒充其他账号。
	delete(fields, "actorId")
	delete(fields, "actorRole")
	if ctx, ok := entry.Context.(*appcontext.Context); ok && ctx != nil && ctx.UserID > 0 {
		fields["actorId"] = strconv.Itoa(ctx.UserID)
		fields["actorRole"] = string(ctx.EffectiveRole())
	}
	if action, ok := fields["action"].(string); !ok || strings.TrimSpace(action) == "" {
		action = "运行事件"
		if entry.Caller != nil && entry.Caller.Function != "" {
			function := entry.Caller.Function
			if index := strings.LastIndex(function, "/"); index >= 0 {
				function = function[index+1:]
			}
			action += " · " + function
		}
		fields["action"] = action
	}
	if result, ok := fields["result"].(string); !ok || strings.TrimSpace(result) == "" {
		fields["result"] = "recorded"
	}
	return fields
}
