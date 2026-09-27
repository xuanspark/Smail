package credentials

import (
	"crypto/sha256"
	"encoding/hex"

	"git.pcux.cn/xuans/Smail/models"
	"xorm.io/xorm"
)

// ProtocolGrant 绑定认证时匹配的单个凭据；命令复核不重复执行密码哈希。
// 字段不导出，避免授权码哈希进入日志、接口响应或持久化会话。
type ProtocolGrant struct {
	engine   *xorm.Engine
	userID   int
	protocol string
	client   bool
	id       int
	version  int
	hash     string
}

func (g *ProtocolGrant) QueueBinding() (string, int, int, string) {
	if g == nil || g.protocol != "smtp" {
		return "", 0, 0, ""
	}
	kind := "smtp"
	if g.client {
		kind = "client"
	}
	hash := sha256.Sum256([]byte(g.hash))
	return kind, g.id, g.version, hex.EncodeToString(hash[:])
}

func (g *ProtocolGrant) Valid(engine *xorm.Engine) bool {
	if g == nil || engine == nil || engine != g.engine || g.id <= 0 {
		return false
	}
	query := engine.ID(g.id).Where("user_id = ? AND revoked_at IS NULL AND code_version = ? AND code_hash = ?", g.userID, g.version, g.hash)
	var valid bool
	var err error
	if g.client {
		valid, err = query.Exist(&models.ClientCredential{})
	} else {
		valid, err = query.Where("protocol = ? AND enabled = ?", g.protocol, true).Exist(&models.ProtocolCredential{})
	}
	return err == nil && valid
}
