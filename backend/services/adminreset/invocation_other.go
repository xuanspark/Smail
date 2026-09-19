//go:build !linux

package adminreset

import "context"

// OpenInvocation 保留其他平台原有的本机管理员身份校验。
func OpenInvocation(parent context.Context) (context.Context, func(), error) {
	if err := AuthorizeCurrentProcess(); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, nil
}

func ReadProtectedPasswordContext(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ReadProtectedPassword()
}

func ShowResultContext(ctx context.Context, message string) {
	if ctx.Err() == nil {
		ShowResult(message)
	}
}
