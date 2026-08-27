package ssl

import (
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"testing"
)

func TestCheckSSLCrtInfo(t *testing.T) {
	if err := config.Init(); err != nil {
		t.Fatal(err)
	}

	got, got1, match, err := CheckSSLCrtInfo()

	fmt.Println(got, got1, match, err)
}
