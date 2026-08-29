package hook

import (
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
	"github.com/go-telegram/bot"
	"github.com/ydzydzydz/pmail_telegram_push/logger"
)

func NewBot(config pluginSettings, token string) (*bot.Bot, error) {
	timeout := time.Duration(config.Timeout) * time.Second
	opts := []bot.Option{bot.WithCheckInitTimeout(timeout)}
	if config.Debug {
		opts = append(opts,
			bot.WithDebug(),
			bot.WithDebugHandler(func(format string, args ...any) {
				logger.BotLogger.Debug().Msgf(format, args...)
			}),
		)
	}
	if config.Proxy != "" {
		return nil, errUnsupportedProxy
	}
	httpClient := credential.NewRestrictedHTTPClientForTargets(config.Timeout*1000, []credential.NetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}})
	opts = append(opts, bot.WithHTTPClient(timeout, httpClient))
	return bot.New(token, opts...)
}

var errUnsupportedProxy = &unsupportedProxyError{}

type unsupportedProxyError struct{}

func (*unsupportedProxyError) Error() string { return "不支持的代理协议" }
