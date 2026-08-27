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
		pathMatches := endpoint.Path == path
		if strings.HasSuffix(endpoint.Path, "/") {
			pathMatches = strings.HasPrefix(path, endpoint.Path)
		}
		if methodMatches && pathMatches {
			return endpoint, true
		}
	}
	return Endpoint{}, false
}
