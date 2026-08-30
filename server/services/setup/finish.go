package setup

import (
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/signal"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"sync"
)

var finishMu sync.Mutex

// Finish 标记初始化完成
func Finish() error {
	finishMu.Lock()
	defer finishMu.Unlock()
	cfg, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	if cfg.IsSetupReady() {
		config.IsInit = true
		return nil
	}
	cfg.SetupState = config.SetupStateReady
	cfg.IsInit = true

	err = config.WriteConfig(cfg)
	if err != nil {
		return errors.Wrap(err)
	}
	config.IsInit = true
	// 初始化完成
	select {
	case signal.InitChan <- true:
	default:
		// 初始化通知只需唤醒一次启动流程，避免重复完成请求阻塞响应。
	}
	return nil
}
