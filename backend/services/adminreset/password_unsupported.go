//go:build !linux && !windows

package adminreset

func ReadProtectedPassword() (string, error)  { return "", ErrUnsupported }
func ReadVisibleInput(string) (string, error) { return "", ErrUnsupported }
