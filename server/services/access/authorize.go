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
	ActorRole                Role
	Capability               Capability
	TargetRole               Role
	NewRole                  Role
	OwnResource              bool
	RecentAuth               bool
	RecoveryCodeConfigured   bool
	TargetRecoveryConfigured bool
	PluginInstanceAuthorized bool
	AuditScopeAuthorized     bool
	DeploymentChannel        bool
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
	if policy.RequiresRecoveryCode && !request.RecoveryCodeConfigured && !request.DeploymentChannel {
		return deny(CodeRecoveryCodeRequired)
	}
	return authorizeResource(request)
}

func authorizeResource(request Request) error {
	switch request.Capability {
	case CapabilityPasswordChangeOwn, CapabilityPluginWriteOwn:
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
	case CapabilityUserManage, CapabilityPasswordReset:
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
		if !request.TargetRecoveryConfigured {
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
