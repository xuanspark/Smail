//go:build !linux && !windows

package adminreset

func ReadProtectedPassword() (string, error) { return "", ErrUnsupported }
