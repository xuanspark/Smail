package access

type Capability string

const (
	CapabilityConfigRead         Capability = "config.read"
	CapabilityConfigWriteGlobal  Capability = "config.write_global"
	CapabilityConfigRollback     Capability = "config.rollback"
	CapabilityDatabaseMigrate    Capability = "database.migrate"
	CapabilityPluginRead         Capability = "plugin.read"
	CapabilityPluginReadGlobal   Capability = "plugin.read_global"
	CapabilityPluginControl      Capability = "plugin.control"
	CapabilityPluginRestart      Capability = "plugin.restart"
	CapabilityServiceRestart     Capability = "service.restart"
	CapabilityPluginWriteGlobal  Capability = "plugin.write_global"
	CapabilityPluginWriteAnyUser Capability = "plugin.write_any_user"
	CapabilityPluginWriteOwn     Capability = "plugin.write_own"
	CapabilityUserRead           Capability = "user.read"
	CapabilityUserCreate         Capability = "user.create"
	CapabilityProfileWriteOwn    Capability = "profile.write_own"
	CapabilityUserManage         Capability = "user.manage"
	CapabilityUserSecurityManage Capability = "user.security_manage"
	CapabilityRoleManage         Capability = "role.manage"
	CapabilityPasswordChangeOwn  Capability = "password.change_own"
	CapabilityPasswordReset      Capability = "password.reset"
	CapabilityAuditRead          Capability = "audit.read"
	CapabilityAuditExport        Capability = "audit.export"
	CapabilityNotificationManage Capability = "notification.manage"
	CapabilityNotificationReveal Capability = "notification.reveal"
	CapabilityAccountGroupManage Capability = "account_group.manage"
)

type Policy struct {
	Capability           Capability
	HighRisk             bool
	RequiresRecentAuth   bool
	RequiresRecoveryCode bool
	RequiresTarget       bool
}

var policies = map[Capability]Policy{
	CapabilityNotificationReveal: {Capability: CapabilityNotificationReveal, HighRisk: true, RequiresTarget: true},
	CapabilityAccountGroupManage: {Capability: CapabilityAccountGroupManage, HighRisk: true, RequiresRecentAuth: true},
	CapabilityNotificationManage: {Capability: CapabilityNotificationManage, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true, RequiresTarget: true},
	CapabilityConfigRead:         {Capability: CapabilityConfigRead},
	CapabilityConfigWriteGlobal:  {Capability: CapabilityConfigWriteGlobal, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true},
	CapabilityConfigRollback:     {Capability: CapabilityConfigRollback, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true},
	CapabilityDatabaseMigrate:    {Capability: CapabilityDatabaseMigrate, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true},
	CapabilityPluginRead:         {Capability: CapabilityPluginRead},
	CapabilityPluginReadGlobal:   {Capability: CapabilityPluginReadGlobal},
	CapabilityPluginControl:      {Capability: CapabilityPluginControl, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true},
	CapabilityPluginRestart:      {Capability: CapabilityPluginRestart, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true},
	CapabilityServiceRestart:     {Capability: CapabilityServiceRestart, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true},
	CapabilityPluginWriteGlobal:  {Capability: CapabilityPluginWriteGlobal, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true},
	CapabilityPluginWriteAnyUser: {Capability: CapabilityPluginWriteAnyUser, RequiresTarget: true},
	CapabilityPluginWriteOwn:     {Capability: CapabilityPluginWriteOwn},
	CapabilityUserRead:           {Capability: CapabilityUserRead},
	CapabilityUserCreate:         {Capability: CapabilityUserCreate, RequiresTarget: true},
	CapabilityProfileWriteOwn:    {Capability: CapabilityProfileWriteOwn},
	CapabilityUserManage:         {Capability: CapabilityUserManage, RequiresTarget: true},
	CapabilityUserSecurityManage: {Capability: CapabilityUserSecurityManage, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true, RequiresTarget: true},
	CapabilityRoleManage:         {Capability: CapabilityRoleManage, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true, RequiresTarget: true},
	CapabilityPasswordChangeOwn:  {Capability: CapabilityPasswordChangeOwn},
	CapabilityPasswordReset:      {Capability: CapabilityPasswordReset, HighRisk: true, RequiresRecentAuth: true, RequiresRecoveryCode: true, RequiresTarget: true},
	CapabilityAuditRead:          {Capability: CapabilityAuditRead},
	CapabilityAuditExport:        {Capability: CapabilityAuditExport, HighRisk: true, RequiresRecentAuth: true},
}

var grants = map[Role]map[Capability]struct{}{
	RoleSuperAdmin: grant(
		CapabilityNotificationReveal,
		CapabilityAccountGroupManage,
		CapabilityNotificationManage,
		CapabilityPluginReadGlobal, CapabilityUserCreate, CapabilityProfileWriteOwn,
		CapabilityConfigRead, CapabilityConfigWriteGlobal, CapabilityConfigRollback,
		CapabilityDatabaseMigrate, CapabilityPluginRead, CapabilityPluginControl,
		CapabilityPluginRestart, CapabilityServiceRestart, CapabilityPluginWriteGlobal, CapabilityPluginWriteAnyUser,
		CapabilityPluginWriteOwn, CapabilityUserRead, CapabilityUserManage,
		CapabilityUserSecurityManage, CapabilityRoleManage, CapabilityPasswordChangeOwn, CapabilityPasswordReset,
		CapabilityAuditRead, CapabilityAuditExport,
	),
	RoleAdmin: grant(
		CapabilityNotificationReveal,
		CapabilityNotificationManage,
		CapabilityPluginReadGlobal, CapabilityPluginRead, CapabilityPluginWriteGlobal, CapabilityProfileWriteOwn,
		CapabilityPluginWriteAnyUser, CapabilityPluginWriteOwn, CapabilityUserRead,
		CapabilityUserManage, CapabilityUserSecurityManage, CapabilityPasswordChangeOwn, CapabilityPasswordReset,
		CapabilityAuditRead,
	),
	RoleUser: grant(
		CapabilityNotificationReveal,
		CapabilityProfileWriteOwn,
		CapabilityPluginRead, CapabilityPluginWriteOwn, CapabilityUserRead,
		CapabilityPasswordChangeOwn, CapabilityAuditRead,
	),
}

func grant(capabilities ...Capability) map[Capability]struct{} {
	result := make(map[Capability]struct{}, len(capabilities))
	for _, capability := range capabilities {
		result[capability] = struct{}{}
	}
	return result
}

func GetPolicy(capability Capability) (Policy, bool) {
	policy, ok := policies[capability]
	return policy, ok
}

func Capabilities(role Role) []Capability {
	roleGrants, ok := grants[role]
	if !ok {
		return nil
	}
	result := make([]Capability, 0, len(roleGrants))
	for capability := range roleGrants {
		result = append(result, capability)
	}
	return result
}
