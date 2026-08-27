//go:build windows

package adminreset

// AuthorizeCurrentProcess 只允许已提升的本机管理员或 SYSTEM 启动正式重置流程。
func AuthorizeCurrentProcess() error {
	return authorizeCurrentWindowsProcess()
}

// CheckEnvironment 读取 Windows 管理程序的非敏感运行状态。
func CheckEnvironment() EnvironmentReport {
	return EnvironmentReport{
		Platform:               "windows",
		Supported:              true,
		Privileged:             authorizeCurrentWindowsProcess() == nil,
		ServiceManaged:         true,
		PasswordAgentAvailable: true,
		ControlPath:            DefaultControlPath,
	}
}
