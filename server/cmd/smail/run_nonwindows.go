//go:build !windows

package main

import "git.pcux.cn/xuans/Smail/internal/app"

func runMain() error {
	return app.RecoverPanic(app.Run)
}
