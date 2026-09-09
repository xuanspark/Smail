package config

import (
	"context"
	"strconv"

	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"

	log "github.com/sirupsen/logrus"
)

// RuntimeConfigLogWriter 复用运行日志记录配置结果，不生成独立审计文件或数据库记录。
// 只保留定位操作需要的元数据，不输出配置值、凭据、用户名称或完整变更载荷。
type RuntimeConfigLogWriter struct{}

func (RuntimeConfigLogWriter) WriteConfigAudit(ctx context.Context, event ConfigAuditEvent) error {
	event, err := normalizeAuditEvent(event)
	if err != nil {
		return err
	}
	action := map[ConfigOperation]string{
		ConfigOperationUpdate:    "保存配置",
		ConfigOperationRollback:  "回滚配置",
		ConfigOperationMigration: "迁移配置",
	}[event.Operation]
	// 操作者来自配置管理器已授权的事件，不从客户端载荷或任意日志字段恢复归属。
	actorID, _ := strconv.Atoi(event.ActorID)
	actorContext := &appcontext.Context{Context: ctx, UserID: actorID, Role: access.Role(event.ActorRole)}
	entry := log.WithContext(actorContext).WithFields(log.Fields{
		"action":         action,
		"object":         event.Section,
		"operation":      event.Operation,
		"section":        event.Section,
		"actorId":        event.ActorID,
		"fromGeneration": event.FromGeneration,
		"toGeneration":   event.ToGeneration,
		"result":         event.Result,
	})
	if event.Result == "failure" {
		entry.Warn(action + "失败")
	} else {
		entry.Info(action + "成功")
	}
	return nil
}
