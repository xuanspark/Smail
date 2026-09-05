package main

import (
	"fmt"
	"os"

	"git.pcux.cn/xuans/Smail/utils/password"
)

func main() {
	fmt.Print(password.Verify(os.Getenv("SMAIL_HASH"), os.Getenv("SMAIL_TEST_PASSWORD")))
}
