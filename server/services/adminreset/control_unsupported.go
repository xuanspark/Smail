//go:build !linux && !windows

package adminreset

func ListenControl(path string, allowedUIDs []uint32) (ControlServer, error) {
	return nil, ErrUnsupported
}
