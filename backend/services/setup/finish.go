package setup

import (
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/signal"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"sync"
)

var finishMu sync.Mutex

// Finish 标记初始化完成
func Finish() error {
	finishMu.Lock()
	defer finishMu.Unlock()
	if err := db.ValidateSuperAdmin(db.Instance); err != nil {
		return err
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	initializing := !cfg.IsInit && cfg.SetupState != config.SetupStateReady
	portsChanged := applyServicePorts(cfg)
	if cfg.IsSetupReady() {
		if portsChanged {
			if err = config.WriteSetupConfig(cfg); err != nil {
				return errors.Wrap(err)
			}
		}
		config.SetInitState(true)
		return nil
	}
	cfg.SetupState = config.SetupStateReady
	cfg.IsInit = true
	if initializing {
		cfg.VerificationEnabled = true
		cfg.ExternalRecoveryEnabled = true
		cfg.LoginPolicy = "new_device"
	}

	err = config.FinalizeSetupConfig(cfg)
	if err != nil {
		return errors.Wrap(err)
	}
	config.SetInitState(true)
	// 初始化完成
	select {
	case signal.InitChan <- true:
	default:
		// 初始化通知只需唤醒一次启动流程，避免重复完成请求阻塞响应。
	}
	return nil
}

func applyServicePorts(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	resolved := config.ResolveHTTPPortDefaults(config.HTTPConfig{
		Port:         cfg.HttpPort,
		HTTPSPort:    cfg.HttpsPort,
		HTTPSEnabled: cfg.HttpsEnabled != 2,
		TLSMode:      cfg.TLSMode,
	}, cfg.GetSetupPort())
	changed := cfg.HttpPort != resolved.Port || cfg.HttpsPort != resolved.HTTPSPort || cfg.TLSMode != resolved.TLSMode
	cfg.HttpPort = resolved.Port
	cfg.HttpsPort = resolved.HTTPSPort
	cfg.TLSMode = resolved.TLSMode
	if resolved.TLSMode == config.HTTPSTLSModeProxy && cfg.HttpsEnabled != 2 {
		cfg.HttpsEnabled = 2
		changed = true
	}
	return changed
}
