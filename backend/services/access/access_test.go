package access

import "testing"

func TestDefaultDeny(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		code DenialCode
	}{
		{name: "invalid role", req: Request{ActorRole: Role("root"), Capability: CapabilityConfigRead}, code: CodeInvalidRole},
		{name: "unknown capability", req: Request{ActorRole: RoleSuperAdmin, Capability: Capability("config.delete")}, code: CodeUnknownCapability},
		{name: "missing grant", req: Request{ActorRole: RoleUser, Capability: CapabilityConfigRollback}, code: CodeAccessDenied},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := Authorize(test.req); !IsDenied(err, test.code) {
				t.Fatalf("expected %s, got %v", test.code, err)
			}
		})
	}
}

func TestRoleBoundaries(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		code DenialCode
	}{
		{name: "super admin resets admin", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPasswordReset, TargetRole: RoleAdmin, RecentAuth: true, RecoveryCodeConfigured: true}},
		{name: "super admin cannot be web reset target", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPasswordReset, TargetRole: RoleSuperAdmin, RecentAuth: true, RecoveryCodeConfigured: true}, code: CodeTargetRoleDenied},
		{name: "admin resets user", req: Request{ActorRole: RoleAdmin, Capability: CapabilityPasswordReset, TargetRole: RoleUser, RecentAuth: true, RecoveryCodeConfigured: true}},
		{name: "admin cannot reset admin", req: Request{ActorRole: RoleAdmin, Capability: CapabilityPasswordReset, TargetRole: RoleAdmin, RecentAuth: true, RecoveryCodeConfigured: true}, code: CodeTargetRoleDenied},
		{name: "user cannot reset another user", req: Request{ActorRole: RoleUser, Capability: CapabilityPasswordReset, TargetRole: RoleUser, RecentAuth: true, RecoveryCodeConfigured: true}, code: CodeAccessDenied},
		{name: "super admin manages user security", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityUserSecurityManage, TargetRole: RoleUser, RecentAuth: true}},
		{name: "admin manages user security", req: Request{ActorRole: RoleAdmin, Capability: CapabilityUserSecurityManage, TargetRole: RoleUser, RecentAuth: true, RecoveryCodeConfigured: true}},
		{name: "admin cannot manage admin security", req: Request{ActorRole: RoleAdmin, Capability: CapabilityUserSecurityManage, TargetRole: RoleAdmin, RecentAuth: true, RecoveryCodeConfigured: true}, code: CodeTargetRoleDenied},
		{name: "web cannot manage super admin security", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityUserSecurityManage, TargetRole: RoleSuperAdmin, RecentAuth: true}, code: CodeTargetRoleDenied},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Authorize(test.req)
			if test.code == "" && err != nil {
				t.Fatalf("expected allow, got %v", err)
			}
			if test.code != "" && !IsDenied(err, test.code) {
				t.Fatalf("expected %s, got %v", test.code, err)
			}
		})
	}
}

func TestRecoveryAndRecentAuthentication(t *testing.T) {
	admin := Request{ActorRole: RoleAdmin, Capability: CapabilityPluginWriteGlobal, RecentAuth: true}
	if err := Authorize(admin); !IsDenied(err, CodeRecoveryCodeRequired) {
		t.Fatalf("expected recovery code requirement, got %v", err)
	}
	admin.RecoveryCodeConfigured = true
	admin.RecentAuth = false
	if err := Authorize(admin); !IsDenied(err, CodeRecentAuthRequired) {
		t.Fatalf("expected recent authentication requirement, got %v", err)
	}
	superAdmin := Request{ActorRole: RoleSuperAdmin, Capability: CapabilityConfigRollback, RecentAuth: true}
	if err := Authorize(superAdmin); err != nil {
		t.Fatalf("super admin should be exempt from recovery code precondition: %v", err)
	}
	adminRollback := Request{ActorRole: RoleAdmin, Capability: CapabilityConfigRollback, RecentAuth: true}
	if err := Authorize(adminRollback); !IsDenied(err, CodeAccessDenied) {
		t.Fatalf("admin must not receive a recovery-code bypass for a super-admin capability: %v", err)
	}
}

func TestRoleChangeRequiresTargetRecovery(t *testing.T) {
	req := Request{ActorRole: RoleSuperAdmin, Capability: CapabilityRoleManage, TargetRole: RoleUser, NewRole: RoleAdmin, RecentAuth: true, RecoveryCodeConfigured: true}
	if err := Authorize(req); !IsDenied(err, CodeRecoveryCodeRequired) {
		t.Fatalf("expected target recovery requirement, got %v", err)
	}
	req.TargetRecoveryConfigured = true
	if err := Authorize(req); err != nil {
		t.Fatalf("expected role change to be allowed, got %v", err)
	}
}

func TestAdminPluginRestartScope(t *testing.T) {
	req := Request{ActorRole: RoleAdmin, Capability: CapabilityPluginRestart, RecentAuth: true, RecoveryCodeConfigured: true}
	if err := Authorize(req); !IsDenied(err, CodeAccessDenied) {
		t.Fatalf("expected plugin scope denial, got %v", err)
	}
	req.PluginInstanceAuthorized = true
	if err := Authorize(req); !IsDenied(err, CodeAccessDenied) {
		t.Fatalf("expected plugin restart to remain denied, got %v", err)
	}
}

func TestManagementEndpointCatalog(t *testing.T) {
	endpoints := ManagementEndpoints()
	if len(endpoints) != 37 {
		t.Fatalf("unexpected endpoint count: %d", len(endpoints))
	}
	seen := map[string]struct{}{}
	for _, endpoint := range endpoints {
		key := endpoint.Method + " " + endpoint.Path
		if _, ok := seen[key]; ok {
			t.Fatalf("duplicate endpoint: %s", key)
		}
		seen[key] = struct{}{}
		if _, ok := GetPolicy(endpoint.Capability); !ok {
			t.Fatalf("endpoint %s references unknown capability", key)
		}
	}
	endpoint, ok := LookupEndpoint("POST", "/api/plugin/settings/example")
	if !ok || endpoint.Capability != CapabilityPluginReadGlobal {
		t.Fatal("plugin settings endpoint was not matched")
	}
	endpoint, ok = LookupEndpoint("GET", "/api/v1/logs")
	if !ok || endpoint.Capability != CapabilityAuditRead {
		t.Fatal("log endpoint was not matched")
	}
	endpoint, ok = LookupEndpoint("POST", "/api/v1/config/smtp/validate")
	if !ok || endpoint.Capability != CapabilityConfigWriteGlobal {
		t.Fatal("configuration validation endpoint was not matched")
	}
	endpoint, ok = LookupEndpoint("POST", "/api/v1/system/restart")
	if !ok || endpoint.Capability != CapabilityServiceRestart {
		t.Fatal("system restart endpoint was not matched")
	}
	endpoint, ok = LookupEndpoint("GET", "/api/v1/config/email")
	if !ok || endpoint.Capability != CapabilityConfigRead {
		t.Fatal("configuration section endpoint was not matched")
	}
	endpoint, ok = LookupEndpoint("PUT", "/api/v1/config/email")
	if !ok || endpoint.Capability != CapabilityConfigWriteGlobal {
		t.Fatal("configuration update endpoint was not matched")
	}
	endpoint, ok = LookupEndpoint("GET", "/api/v1/config/backups")
	if !ok || endpoint.Capability != CapabilityConfigRollback {
		t.Fatal("configuration backup endpoint did not require rollback capability")
	}
	endpoint, ok = LookupEndpoint("POST", "/api/v1/user/security/external-email/change")
	if !ok || endpoint.Capability != CapabilityUserSecurityManage {
		t.Fatal("managed user security endpoint was not matched")
	}
}

func TestEveryManagementEndpointHasAllowAndDeny(t *testing.T) {
	for _, endpoint := range ManagementEndpoints() {
		actorRole := RoleSuperAdmin
		allowed := Request{
			ActorRole:                actorRole,
			Capability:               endpoint.Capability,
			TargetRole:               RoleUser,
			OwnResource:              true,
			RecentAuth:               true,
			RecoveryCodeConfigured:   true,
			TargetRecoveryConfigured: true,
			PluginInstanceAuthorized: true,
		}
		if err := Authorize(allowed); err != nil {
			t.Fatalf("expected allow for %s %s: %v", endpoint.Method, endpoint.Path, err)
		}
		denied := allowed
		denied.ActorRole = Role("unknown")
		if err := Authorize(denied); !IsDenied(err, CodeInvalidRole) {
			t.Fatalf("expected deny for %s %s, got %v", endpoint.Method, endpoint.Path, err)
		}
	}
}
