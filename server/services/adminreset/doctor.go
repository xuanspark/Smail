package adminreset

// EnvironmentReport 描述管理程序运行所需的非敏感环境状态。
type EnvironmentReport struct {
	Platform               string
	Supported              bool
	Privileged             bool
	ServiceManaged         bool
	PasswordAgentAvailable bool
	ControlPath            string
}
