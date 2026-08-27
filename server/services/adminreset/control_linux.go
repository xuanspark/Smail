//go:build linux

package adminreset

// ListenControl 创建仅允许受信任 UID 连接的 Unix Domain Socket。
func ListenControl(path string, allowedUIDs []uint32) (ControlServer, error) {
	return ListenUnix(path, allowedUIDs)
}
