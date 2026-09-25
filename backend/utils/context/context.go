package context

import (
	"context"

	"git.pcux.cn/xuans/Smail/services/access"
)

const (
	LogID = "LogID"
)

type Context struct {
	context.Context           `json:"-"`
	UserID                    int
	SessionVersion            int
	SecurityVersion           int
	UserAccount               string
	UserName                  string
	Role                      access.Role
	Values                    map[string]any
	Lang                      string
	IsAdmin                   bool
	MustChangePassword        int
	SecuritySetupAcknowledged int
	RecentAuth                bool
	RecoveryCodeConfigured    bool
	TargetRecoveryConfigured  bool
	PluginInstanceAuthorized  bool
	AuditScopeAuthorized      bool
	DeploymentChannel         bool
}

// HasRole 判断当前请求是否属于指定角色。
func (c *Context) HasRole(role access.Role) bool {
	return c != nil && c.Role == role
}

// EffectiveRole 兼容旧调用方，确保旧会话仍能得到明确角色。
func (c *Context) EffectiveRole() access.Role {
	if c == nil {
		return ""
	}
	if c.Role.Valid() {
		return c.Role
	}
	if c.IsAdmin {
		return access.RoleSuperAdmin
	}
	return access.RoleUser
}

func (c *Context) SetValue(key string, value any) {
	if c.Values == nil {
		c.Values = map[string]any{}
	}
	c.Values[key] = value

}

func (c *Context) GetValue(key string) any {
	if c.Values == nil {
		return nil
	}
	return c.Values[key]
}
