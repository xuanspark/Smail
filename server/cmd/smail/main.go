package main

import (
	"fmt"
	"os"

	"git.pcux.cn/xuans/Smail/internal/app"
)

func main() {
	if err := app.RecoverPanic(app.Run); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
