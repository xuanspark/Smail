package access

import "strings"

type Endpoint struct {
	Method     string
	Path       string
	Capability Capability
}

var managementEndpoints = []Endpoint{
	{Method: "POST", Path: "/api/settings/modify_password", Capability: CapabilityPasswordChangeOwn},
	{Method: "POST", Path: "/api/user/create", Capability: CapabilityUserManage},
	{Method: "POST", Path: "/api/user/edit", Capability: CapabilityUserManage},
	{Method: "POST", Path: "/api/user/info", Capability: CapabilityUserRead},
	{Method: "POST", Path: "/api/user/list", Capability: CapabilityUserRead},
	{Method: "GET", Path: "/api/plugin/list", Capability: CapabilityPluginRead},
	{Method: "*", Path: "/api/plugin/settings/", Capability: CapabilityPluginWriteOwn},
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
	{Method: "POST", Path: "/api/v1/audits/config", Capability: CapabilityAuditExport},
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
