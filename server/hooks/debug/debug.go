package main

import (
	"fmt"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"os"
)

type Debug struct {
}

func NewDebug() *Debug {
	return &Debug{}
}

func (d Debug) SendBefore(ctx *context.Context, email *parsemail.Email) {
	fmt.Print("[debug] SendBefore")

}

func (d Debug) SendAfter(ctx *context.Context, email *parsemail.Email, err map[string]error) {
	fmt.Print("[debug] SendAfter")

}

func (d Debug) ReceiveParseBefore(ctx *context.Context, email *[]byte) {
	fmt.Print("[debug] ReceiveParseBefore")

}

func (d Debug) ReceiveParseAfter(ctx *context.Context, email *parsemail.Email) {
	fmt.Print("[debug] ReceiveParseAfter")
	email.Status = 5
}

func (d Debug) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail) {
	fmt.Print("[debug] ReceiveSaveAfter")
}

func (d Debug) GetName(ctx *context.Context) string {
	return "debug"
}

func (d Debug) SettingsHtml(ctx *context.Context, url string, requestData string) string {
	return ""
}

func main() {
	if err := framework.CreatePlugin("debug_plugin", NewDebug()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
