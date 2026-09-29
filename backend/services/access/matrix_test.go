package access

import "testing"

func TestCapabilityMatrix(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		allowed bool
	}{
		{name: "super admin reads config", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityConfigRead}, allowed: true},
		{name: "admin cannot read config", req: Request{ActorRole: RoleAdmin, Capability: CapabilityConfigRead}},
		{name: "user cannot read config", req: Request{ActorRole: RoleUser, Capability: CapabilityConfigRead}},
		{name: "super admin controls plugin", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPluginControl, RecentAuth: true, RecoveryCodeConfigured: true}, allowed: true},
		{name: "super admin controls plugin without recovery code", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPluginControl, RecentAuth: true}, allowed: true},
		{name: "admin cannot control plugin", req: Request{ActorRole: RoleAdmin, Capability: CapabilityPluginControl, RecentAuth: true, RecoveryCodeConfigured: true}},
		{name: "super admin writes global plugin settings", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPluginWriteGlobal, RecentAuth: true, RecoveryCodeConfigured: true}, allowed: true},
		{name: "admin writes global plugin settings", req: Request{ActorRole: RoleAdmin, Capability: CapabilityPluginWriteGlobal, RecentAuth: true, RecoveryCodeConfigured: true}, allowed: true},
		{name: "admin cannot create user", req: Request{ActorRole: RoleAdmin, Capability: CapabilityUserCreate, TargetRole: RoleUser}},
		{name: "user edits own profile", req: Request{ActorRole: RoleUser, Capability: CapabilityProfileWriteOwn, OwnResource: true}, allowed: true},
		{name: "user cannot edit another profile", req: Request{ActorRole: RoleUser, Capability: CapabilityProfileWriteOwn}},
		{name: "admin writes user plugin settings", req: Request{ActorRole: RoleAdmin, Capability: CapabilityPluginWriteAnyUser, TargetRole: RoleUser}, allowed: true},
		{name: "admin cannot write admin plugin settings", req: Request{ActorRole: RoleAdmin, Capability: CapabilityPluginWriteAnyUser, TargetRole: RoleAdmin}},
		{name: "super admin writes all user plugin settings", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPluginWriteAnyUser, TargetRole: RoleSuperAdmin}, allowed: true},
		{name: "super admin writes own plugin settings", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPluginWriteOwn, OwnResource: true}, allowed: true},
		{name: "user writes own plugin settings", req: Request{ActorRole: RoleUser, Capability: CapabilityPluginWriteOwn, OwnResource: true}, allowed: true},
		{name: "user cannot write another plugin settings", req: Request{ActorRole: RoleUser, Capability: CapabilityPluginWriteOwn}},
		{name: "authenticated super admin changes own password", req: Request{ActorRole: RoleSuperAdmin, Capability: CapabilityPasswordChangeOwn, OwnResource: true}, allowed: true},
		{name: "admin changes own password", req: Request{ActorRole: RoleAdmin, Capability: CapabilityPasswordChangeOwn, OwnResource: true}, allowed: true},
		{name: "user reads own audit", req: Request{ActorRole: RoleUser, Capability: CapabilityAuditRead, OwnResource: true}, allowed: true},
		{name: "user cannot read another audit", req: Request{ActorRole: RoleUser, Capability: CapabilityAuditRead}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Authorize(test.req)
			if test.allowed && err != nil {
				t.Fatalf("expected allow, got %v", err)
			}
			if !test.allowed && err == nil {
				t.Fatal("expected denial")
			}
		})
	}
}
