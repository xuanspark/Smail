package config

import (
	"regexp"
	"strconv"
	"strings"

	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

var knownActionMap = map[string]string{
	"imap_server.(*serverSession).Login":  "IMAP 客户端登录",
	"imap_server.(*serverSession).Close":  "IMAP 断开连接",
	"imap_server.(*serverSession).Select": "IMAP 选择邮箱",
	"imap_server.(*serverSession).Fetch":  "IMAP 获取邮件",
	"imap_server.(*serverSession).Append": "IMAP 追加邮件",
	"imap_server.(*serverSession).Delete": "IMAP 删除邮箱",
	"pop3_server.action.Pass":            "POP3 客户端登录",
	"pop3_server.action.Retr":            "POP3 获取邮件",
	"smtp_server.(*Session).AuthPlain":   "SMTP 客户端认证",
	"send.doSend":                        "SMTP 邮件投递",
	"send.doSend.func2":                  "SMTP 邮件投递",
	"send.Send":                          "发送邮件",
}

var cleanFuncRe = regexp.MustCompile(`^([a-zA-Z0-9_]+)\.(?:\(\*?[a-zA-Z0-9_]+\)\.)?([a-zA-Z0-9_]+)(?:\.func\d+)?$`)

func friendlyActionFromCaller(function string) string {
	if action, ok := knownActionMap[function]; ok {
		return action
	}
	m := cleanFuncRe.FindStringSubmatch(function)
	if len(m) == 3 {
		pkg := m[1]
		fn := m[2]
		pkgLabel := map[string]string{
			"imap_server": "IMAP 服务",
			"pop3_server": "POP3 服务",
			"smtp_server": "SMTP 服务",
			"send":        "邮件投递",
			"controllers": "接口处理",
			"services":    "系统服务",
			"res_init":    "系统初始化",
			"hooks":       "插件钩子",
			"adminreset":  "超级管理",
		}[pkg]
		if pkgLabel == "" {
			pkgLabel = pkg
		}
		return pkgLabel + " · " + fn
	}
	return "运行事件 · " + function
}

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
			action = friendlyActionFromCaller(function)
		}
		fields["action"] = action
	}
	if result, ok := fields["result"].(string); !ok || strings.TrimSpace(result) == "" {
		fields["result"] = "recorded"
	}
	return fields
}