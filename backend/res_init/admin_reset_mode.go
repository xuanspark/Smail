package res_init

const developmentDisableAdminResetFlag = "--dev-disable-admin-reset"

// adminResetDisabledForDevelopment 仅停用开发环境的重置通道，不放宽通道鉴权。
// 独立参数不改变配置文件、数据库和 Cookie 行为，避免切换开发配置时误用其他数据。
func adminResetDisabledForDevelopment(development bool, args []string) bool {
	if development {
		return true
	}
	for _, arg := range args {
		if arg == developmentDisableAdminResetFlag {
			return true
		}
	}
	return false
}
