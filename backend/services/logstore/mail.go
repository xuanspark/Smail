package logstore

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

// MailResult 记录投递尝试元数据，不复制正文、附件、认证信息或服务器错误原文。
func MailResult(userID int, direction string, sender string, recipientCount, size int, result string) {
	if len(sender) > 320 {
		sender = "地址超出显示长度"
	}
	payload, _ := json.Marshal(struct {
		Direction      string `json:"direction"`
		Sender         string `json:"sender"`
		RecipientCount int    `json:"recipientCount"`
		Bytes          int    `json:"bytes"`
	}{direction, sender, recipientCount, size})
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
