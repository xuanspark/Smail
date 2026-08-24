package main

import (
	"fmt"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

type Debug struct {
}

func NewDebug() *Debug {
	return &Debug{}
}

func (d Debug) SendBefore(ctx *context.Context, email *parsemail.Email) {
	fmt.Printf("[debug SendBefore] %+v  ", email)

}

func (d Debug) SendAfter(ctx *context.Context, email *parsemail.Email, err map[string]error) {
	fmt.Printf("[debug SendAfter] %+v  ", email)

}

func (d Debug) ReceiveParseBefore(ctx *context.Context, email *[]byte) {
	fmt.Printf("[debug ReceiveParseBefore] %s  ", *email)

}

func (d Debug) ReceiveParseAfter(ctx *context.Context, email *parsemail.Email) {
	fmt.Printf("[debug ReceiveParseAfter] %+v ", email)
	email.Status = 5
}

func (d Debug) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail) {
	fmt.Printf("[debug ReceiveSaveAfter] %+v  %+v ", email, ue)
}

func (d Debug) GetName(ctx *context.Context) string {
	return "debug"
}

func (d Debug) SettingsHtml(ctx *context.Context, url string, requestData string) string {
	return ""
}

func main() {
	framework.CreatePlugin("debug_plugin", NewDebug()).Run()
}
