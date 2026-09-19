//go:build !linux && !windows

package adminreset

import "context"

func ReadProtectedPassword() (string, error)  { return "", ErrUnsupported }
func ReadVisibleInput(string) (string, error) { return "", ErrUnsupported }
func ReadVisibleInputContext(context.Context, string) (string, error) {
	return "", ErrUnsupported
}
func ShowResult(string) {}
