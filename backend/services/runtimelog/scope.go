package runtimelog

import (
	"encoding/json"
	"regexp"
	"strings"
)

var scopedHeader = regexp.MustCompile(`^(\[[^\]\r\n]+\]\s+\[(?:trace|debug|info|warn|warning|error|fatal|panic)\]\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\])`)

// 低权限视图不返回任意消息、源码路径、请求标识或异常原文。
// 公共事件仅开放固定生命周期文案，无法确认归属的历史日志默认隐藏。
func presentScopedLine(line string, actors map[string]bool) (string, bool) {
	if actors == nil {
		return presentLine(line)
	}
	header := scopedHeader.FindStringSubmatch(line)
	boundary := strings.LastIndex(line, " @event ")
	if header == nil || boundary < len(header[0]) {
		return "", false
	}
	var event struct {
		Schema  int    `json:"logSchema"`
		ActorID string `json:"actorId"`
		ActorRole string `json:"actorRole"`
		Action  string `json:"action"`
		Result  string `json:"result"`
		Object  string `json:"object"`
	}
	if json.Unmarshal([]byte(line[boundary+8:]), &event) != nil || event.Schema != 1 {
		return "", false
	}
	action := "账号相关运行事件"
	if own, allowed := actors[event.ActorID]; allowed {
		if !own && event.ActorRole != "user" {
			return "", false
		}
		// 已知动作保留业务含义，未知动作不能携带任意错误原文或账号信息。
		switch event.Action {
		case "执行收信规则", "读取收信规则", "通知 IMAP 客户端新邮件", "接收邮件", "投递邮件", "客户端提交邮件", "更新个人资料", "登录验证", "退出登录", "保存配置", "回滚配置", "迁移配置":
			action = event.Action
		}
	} else {
		if event.ActorID != "" || event.Object != "Smail" {
			return "", false
		}
		switch event.Action {
		case "启动主服务", "停止主服务", "运行主服务":
			action = event.Action
		default:
			return "", false
		}
	}
	outcome := map[string]string{"success": "处理已完成", "failure": "处理失败", "started": "已开始处理", "recorded": "已记录运行状态", "accepted": "已接收", "partial": "部分完成", "unknown": "结果待确认"}[event.Result]
	if outcome == "" {
		event.Result, outcome = "recorded", "已记录运行状态"
	}
	payload, _ := json.Marshal(map[string]any{"logSchema": 1, "action": action, "result": event.Result, "outcome": outcome})
	return header[1] + action + " @event " + string(payload), true
}
