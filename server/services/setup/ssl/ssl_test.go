package ssl

import (
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"testing"
)

func TestCheckSSLCrtInfo(t *testing.T) {
	config.Init()

	got, got1, match, err := CheckSSLCrtInfo()

	fmt.Println(got, got1, match, err)
}
