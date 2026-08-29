package main

import (
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"github.com/ydzydzydz/pmail_telegram_push/hook"
)

func main() {
	framework.CreatePlugin(
		hook.PLUGIN_NAME,
		hook.NewPmailTelegramPushHook(),
	).Run()
}
