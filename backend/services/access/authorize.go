package access

import "errors"

type DenialCode string

const (
	CodeAccessDenied         DenialCode = "ACCESS_DENIED"
	CodeInvalidRole          DenialCode = "INVALID_ROLE"
	CodeUnknownCapability    DenialCode = "UNKNOWN_CAPABILITY"
	CodeRecentAuthRequired   DenialCode = "RECENT_AUTH_REQUIRED"
	CodeRecoveryCodeRequired DenialCode = "RECOVERY_CODE_REQUIRED"
	CodeTargetRequired       DenialCode = "TARGET_REQUIRED"
	CodeTargetRoleDenied     DenialCode = "TARGET_ROLE_DENIED"
	CodePluginScopeDenied    DenialCode = "PLUGIN_SCOPE_DENIED"
)

type Denial struct{ Code DenialCode }

func (e *Denial) Error() string { return string(e.Code) }

func IsDenied(err error, code DenialCode) bool {
	var denial *Denial
	return errors.As(err, &denial) && denial.Code == code
}

type Request struct {
	ActorRole                     Role
	Capability                    Capability
	TargetRole                    Role
	NewRole                       Role
	OwnResource                   bool
	RecentAuth                    bool
	RecoveryCodeConfigured        bool
	ExternalEmailConfigured       bool
	TargetRecoveryConfigured      bool
	TargetExternalEmailConfigured bool
	PluginInstanceAuthorized      bool
	AuditScopeAuthorized          bool
	DeploymentChannel             bool
}

// HasRecoveryOption 判断账号是否至少配置了一种可用恢复方式。
func HasRecoveryOption(recoveryCodeConfigured, externalEmailConfigured bool) bool {
	return recoveryCodeConfigured || externalEmailConfigured
}

func Authorize(request Request) error {
	if !request.ActorRole.Valid() {
		return deny(CodeInvalidRole)
	}
	policy, ok := GetPolicy(request.Capability)
	if !ok {
		return deny(CodeUnknownCapability)
	}
	if _, ok := grants[request.ActorRole][request.Capability]; !ok {
		return deny(CodeAccessDenied)
	}
	if policy.RequiresTarget && !request.TargetRole.Valid() {
		return deny(CodeTargetRequired)
	}
	if policy.RequiresRecentAuth && !request.RecentAuth {
		return deny(CodeRecentAuthRequired)
	}
	// 保留原策略与错误码名称；普通角色的恢复保障允许安全码或已验证外部邮箱任选其一。
	if policy.RequiresRecoveryCode && request.ActorRole != RoleSuperAdmin && !HasRecoveryOption(request.RecoveryCodeConfigured, request.ExternalEmailConfigured) && !request.DeploymentChannel {
		return deny(CodeRecoveryCodeRequired)
	}
	return authorizeResource(request)
}

func authorizeResource(request Request) error {
	switch request.Capability {
	case CapabilityNotificationReveal:
		if !request.OwnResource && !CanManageNotification(request.ActorRole, request.TargetRole, false) {
			return deny(CodeAccessDenied)
		}
	case CapabilityNotificationManage:
		if !CanManageNotification(request.ActorRole, request.TargetRole, request.OwnResource) {
			return deny(CodeTargetRoleDenied)
		}
	case CapabilityPasswordChangeOwn, CapabilityPluginWriteOwn, CapabilityProfileWriteOwn:
		if !request.OwnResource {
			return deny(CodeAccessDenied)
		}
	case CapabilityUserRead:
		if request.ActorRole == RoleUser && !request.OwnResource {
			return deny(CodeAccessDenied)
		}
	case CapabilityAuditRead:
		if request.ActorRole == RoleUser && !request.OwnResource {
			return deny(CodeAccessDenied)
		}
		if request.ActorRole == RoleAdmin && !request.OwnResource && !request.AuditScopeAuthorized {
			return deny(CodeAccessDenied)
		}
	case CapabilityUserCreate, CapabilityUserManage, CapabilityUserSecurityManage, CapabilityPasswordReset:
		if !canManageAccountTarget(request.ActorRole, request.TargetRole) {
			return deny(CodeTargetRoleDenied)
		}
	case CapabilityPluginWriteAnyUser:
		if !canManagePluginTarget(request.ActorRole, request.TargetRole) {
			return deny(CodeTargetRoleDenied)
		}
	case CapabilityRoleManage:
		if !canManageAccountTarget(request.ActorRole, request.TargetRole) || !request.NewRole.Valid() || request.NewRole == RoleSuperAdmin {
			return deny(CodeTargetRoleDenied)
		}
		if !HasRecoveryOption(request.TargetRecoveryConfigured, request.TargetExternalEmailConfigured) {
			return deny(CodeRecoveryCodeRequired)
		}
	case CapabilityPluginRestart:
		if request.ActorRole == RoleAdmin && !request.PluginInstanceAuthorized {
			return deny(CodePluginScopeDenied)
		}
	}
	return nil
}

func canManageAccountTarget(actor Role, target Role) bool {
	switch actor {
	case RoleSuperAdmin:
		return target == RoleAdmin || target == RoleUser
	case RoleAdmin:
		return target == RoleUser
	default:
		return false
	}
}

// 通知授权不改变账号角色；管理员只管理本人和普通用户，不能操作其他管理员。
func CanManageNotification(actor, target Role, own bool) bool {
	return target.Valid() && (actor == RoleSuperAdmin || actor == RoleAdmin && (target == RoleUser || own && target == RoleAdmin))
}

func canManagePluginTarget(actor Role, target Role) bool {
	if actor == RoleSuperAdmin {
		return target.Valid()
	}
	if actor == RoleAdmin {
		return target == RoleUser
	}
	return false
}

func deny(code DenialCode) error { return &Denial{Code: code} }
