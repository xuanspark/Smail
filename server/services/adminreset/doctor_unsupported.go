//go:build !linux && !windows

package adminreset

// AuthorizeCurrentProcess 在不支持的平台上始终拒绝正式重置。
func AuthorizeCurrentProcess() error { return ErrUnsupported }

// CheckEnvironment 返回不支持平台状态。
func CheckEnvironment() EnvironmentReport {
	return EnvironmentReport{Platform: "unsupported", Supported: false}
}
