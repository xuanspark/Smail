package access

import "strings"

type Endpoint struct {
	Method     string
	Path       string
	Capability Capability
}

var managementEndpoints = []Endpoint{
	{Method: "POST", Path: "/api/system/notification/credential/reveal", Capability: CapabilityNotificationManage},
	{Method: "POST", Path: "/api/v1/system/notification/credential/reveal", Capability: CapabilityNotificationManage},
	{Method: "POST", Path: "/api/system/notification/authorization", Capability: CapabilityNotificationManage},
	{Method: "POST", Path: "/api/system/notification/credential", Capability: CapabilityNotificationManage},
	{Method: "POST", Path: "/api/v1/user/groups/save", Capability: CapabilityAccountGroupManage},
	{Method: "POST", Path: "/api/v1/user/groups/membership", Capability: CapabilityAccountGroupManage},
	{Method: "POST", Path: "/api/user/groups/save", Capability: CapabilityAccountGroupManage},
	{Method: "POST", Path: "/api/user/groups/membership", Capability: CapabilityAccountGroupManage},
	{Method: "POST", Path: "/api/v1/system/notification/authorization", Capability: CapabilityNotificationManage},
	{Method: "POST", Path: "/api/v1/system/notification/credential", Capability: CapabilityNotificationManage},
	{Method: "POST", Path: "/api/settings/modify_password", Capability: CapabilityPasswordChangeOwn},
	{Method: "POST", Path: "/api/user/create", Capability: CapabilityUserCreate},
	{Method: "POST", Path: "/api/user/profile", Capability: CapabilityProfileWriteOwn},
	{Method: "POST", Path: "/api/user/edit", Capability: CapabilityUserManage},
	{Method: "*", Path: "/api/user/security/", Capability: CapabilityUserSecurityManage},
	{Method: "POST", Path: "/api/user/info", Capability: CapabilityUserRead},
	{Method: "POST", Path: "/api/user/list", Capability: CapabilityUserRead},
	{Method: "GET", Path: "/api/plugin/list", Capability: CapabilityPluginReadGlobal},
	{Method: "*", Path: "/api/plugin/settings/", Capability: CapabilityPluginReadGlobal},
	{Method: "GET", Path: "/api/v1/config/status", Capability: CapabilityConfigRead},
	{Method: "GET", Path: "/api/v1/config/backups", Capability: CapabilityConfigRollback},
	{Method: "POST", Path: "/api/v1/config/rollback/preview", Capability: CapabilityConfigRollback},
	{Method: "POST", Path: "/api/v1/config/rollback/ticket", Capability: CapabilityConfigRollback},
	{Method: "POST", Path: "/api/v1/config/rollback", Capability: CapabilityConfigRollback},
	{Method: "POST", Path: "/api/v1/config/:section/validate", Capability: CapabilityConfigWriteGlobal},
	{Method: "POST", Path: "/api/v1/config/:section/apply", Capability: CapabilityConfigWriteGlobal},
	{Method: "GET", Path: "/api/v1/config/:section", Capability: CapabilityConfigRead},
	{Method: "PUT", Path: "/api/v1/config/:section", Capability: CapabilityConfigWriteGlobal},
	{Method: "GET", Path: "/api/v1/audits/config", Capability: CapabilityAuditRead},
	{Method: "GET", Path: "/api/v1/logs", Capability: CapabilityAuditRead},
	{Method: "GET", Path: "/api/v1/audits/config/:id", Capability: CapabilityAuditRead},
	{Method: "POST", Path: "/api/v1/audits/config", Capability: CapabilityAuditExport},
	{Method: "POST", Path: "/api/v1/audits/config/export", Capability: CapabilityAuditExport},
	{Method: "POST", Path: "/api/v1/settings/modify_password", Capability: CapabilityPasswordChangeOwn},
	{Method: "POST", Path: "/api/v1/system/restart", Capability: CapabilityServiceRestart},
	{Method: "POST", Path: "/api/v1/system/restart/preview", Capability: CapabilityServiceRestart},
	{Method: "POST", Path: "/api/v1/database/migrations/preview", Capability: CapabilityDatabaseMigrate},
	{Method: "POST", Path: "/api/v1/database/migrations", Capability: CapabilityDatabaseMigrate},
	{Method: "GET", Path: "/api/v1/database/migrations/:id", Capability: CapabilityDatabaseMigrate},
	{Method: "POST", Path: "/api/v1/user/create", Capability: CapabilityUserCreate},
	{Method: "POST", Path: "/api/v1/user/profile", Capability: CapabilityProfileWriteOwn},
	{Method: "POST", Path: "/api/v1/user/edit", Capability: CapabilityUserManage},
	{Method: "*", Path: "/api/v1/user/security/", Capability: CapabilityUserSecurityManage},
	{Method: "POST", Path: "/api/v1/user/info", Capability: CapabilityUserRead},
	{Method: "POST", Path: "/api/v1/user/list", Capability: CapabilityUserRead},
	{Method: "GET", Path: "/api/v1/plugin/list", Capability: CapabilityPluginReadGlobal},
	{Method: "*", Path: "/api/v1/plugin/settings/", Capability: CapabilityPluginReadGlobal},
}

func ManagementEndpoints() []Endpoint {
	result := make([]Endpoint, len(managementEndpoints))
	copy(result, managementEndpoints)
	return result
}

func LookupEndpoint(method string, path string) (Endpoint, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	for _, endpoint := range managementEndpoints {
		methodMatches := endpoint.Method == "*" || endpoint.Method == method
		pathMatches := matchEndpointPath(endpoint.Path, path)
		if methodMatches && pathMatches {
			return endpoint, true
		}
	}
	return Endpoint{}, false
}

func matchEndpointPath(pattern, path string) bool {
	if pattern == path {
		return true
	}
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(path, pattern)
	}
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(patternParts) != len(pathParts) {
		return false
	}
	for index := range patternParts {
		if strings.HasPrefix(patternParts[index], ":") && patternParts[index] != ":" {
			if pathParts[index] == "" {
				return false
			}
			continue
		}
		if patternParts[index] != pathParts[index] {
			return false
		}
	}
	return true
}
