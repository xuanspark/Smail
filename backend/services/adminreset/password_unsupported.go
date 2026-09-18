//go:build !linux && !windows

package adminreset

func ReadProtectedPassword() (string, error)    { return "", ErrUnsupported }
func ReadProtectedInput(string) (string, error) { return "", ErrUnsupported }
