package controllers

import (
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// 分区权限必须在接口和配置管理器两层检查，不能信任前端菜单。
func authorizeConfigSection(ctx *appcontext.Context, section config.Section, write bool) error {
	capability := access.CapabilityConfigRead
	if write {
		capability = access.CapabilityConfigWriteGlobal
	}
	if section == config.SectionPlugins {
		capability = access.CapabilityPluginReadGlobal
		if write {
			capability = access.CapabilityPluginWriteGlobal
		}
	}
	return authorizeCapability(ctx, capability, "", false)
}

func authorizeConfigOverview(ctx *appcontext.Context, write bool) error {
	if ctx != nil && ctx.EffectiveRole() == access.RoleAdmin {
		return authorizeConfigSection(ctx, config.SectionPlugins, write)
	}
	return authorizeConfigSection(ctx, config.SectionRoot, write)
}
