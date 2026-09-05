package controllers

import (
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// authorizeCapability 统一把 HTTP 请求上下文映射到能力授权器。
func authorizeCapability(ctx *context.Context, capability access.Capability, targetRole access.Role, ownResource bool) error {
	return authorizeCapabilityWithNewRole(ctx, capability, targetRole, "", ownResource)
}

// authorizeCapabilityWithNewRole 在角色变更时同时把目标新角色传给统一授权器。
func authorizeCapabilityWithNewRole(ctx *context.Context, capability access.Capability, targetRole, newRole access.Role, ownResource bool) error {
	if ctx == nil {
		return &access.Denial{Code: access.CodeAccessDenied}
	}
	return access.Authorize(access.Request{
		ActorRole:                ctx.EffectiveRole(),
		Capability:               capability,
		TargetRole:               targetRole,
		OwnResource:              ownResource,
		RecentAuth:               ctx.RecentAuth,
		RecoveryCodeConfigured:   ctx.RecoveryCodeConfigured,
		TargetRecoveryConfigured: ctx.TargetRecoveryConfigured,
		NewRole:                  newRole,
		PluginInstanceAuthorized: ctx.PluginInstanceAuthorized,
		AuditScopeAuthorized:     ctx.AuditScopeAuthorized,
		DeploymentChannel:        ctx.DeploymentChannel,
	})
}
