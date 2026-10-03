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
	httpClient, err := credential.NewTelegramHTTPClient(config.Timeout*1000, config.Proxy)
	if err != nil {
		return nil, err
	}
	opts = append(opts, bot.WithHTTPClient(timeout, httpClient))
	return bot.New(token, opts...)
}
