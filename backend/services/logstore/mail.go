package logstore

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/textproto"
	"strconv"
	"time"

	"git.pcux.cn/xuans/Smail/utils/validation"
)

type MailDetails struct {
	Trigger       string `json:"trigger"`
	Stage         string `json:"stage"`
	Outcome       string `json:"outcome"`
	AcceptedCount int    `json:"acceptedCount"`
}

// MailFailureReason 使用错误类别给出原因，不保存可能携带凭据的远端响应原文。
func MailFailureReason(err error) string {
	var certificate *tls.CertificateVerificationError
	var dns *net.DNSError
	var protocol *textproto.Error
	var network net.Error
	switch {
	case errors.Is(err, validation.ErrNotificationRecipient):
		return "系统通知地址仅用于发信，不接受收信，请改用普通邮箱地址"
	case errors.As(err, &certificate):
		return "对端证书验证失败"
	case errors.As(err, &dns):
		return "收件域名解析失败"
	case errors.As(err, &protocol):
		return "对端 SMTP 拒绝，状态码 " + strconv.Itoa(protocol.Code)
	case errors.As(err, &network):
		if network.Timeout() {
			return "投递连接超时"
		}
		return "投递网络连接失败"
	default:
		return "投递未完成，请结合运行日志排查"
	}
}

// MailResult 记录投递尝试元数据，不复制正文、附件、认证信息或服务器错误原文。
func MailResult(userID int, direction string, sender string, recipientCount, size int, result string, details ...MailDetails) {
	if len(sender) > 320 {
		sender = "地址超出显示长度"
	}
	payload, _ := json.Marshal(struct {
		Direction      string `json:"direction"`
		Sender         string `json:"sender"`
		RecipientCount int    `json:"recipientCount"`
		Bytes          int    `json:"bytes"`
		MailDetails
	}{direction, sender, recipientCount, size, mailDetails(direction, result, details)})
	action := "接收邮件"
	if direction == "outbound" {
		action = "投递邮件"
	}
	if direction == "submission" {
		action = "客户端提交邮件"
	}
	Record(context.Background(), Entry{
		Kind: "mail", ActorID: strconv.Itoa(userID), CreatedAt: time.Now(), Action: action, Result: result, Payload: payload,
	})
}

func mailDetails(direction, result string, details []MailDetails) MailDetails {
	value := MailDetails{Trigger: "SMTP 收信", Stage: "接收处理"}
	if direction == "outbound" {
		value.Trigger, value.Stage = "邮件发送", "对外投递"
	} else if direction == "submission" {
		value.Trigger, value.Stage = "SMTP 客户端提交", "提交处理"
	}
	if len(details) > 0 {
		value = details[0]
	}
	if value.Outcome == "" {
		value.Outcome = map[string]string{
			"success": "当前处理阶段已完成", "accepted": "对端服务器已接收，不代表收件人已阅读或最终入箱",
			"failure": "当前处理阶段失败", "partial": "部分收件人已被对端接收，其余投递失败",
			"discarded": "已按收信策略丢弃，未写入邮箱", "unknown": "处理未正常完成，结果待确认",
		}[result]
	}
	return value
}
