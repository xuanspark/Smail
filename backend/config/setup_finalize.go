package config

// FinalizeSetupConfig 先保存可重试的安装状态，再以分区根文件发布就绪状态。
// 只由完成安装的流程调用，不自动迁移已有的运行实例。
func FinalizeSetupConfig(cfg *Config) error {
	legacyConfigMu.Lock()
	defer legacyConfigMu.Unlock()
	if cfg == nil {
		return ErrInvalidRuntimeConfig
	}
	data, err := readConfigFile("config.json")
	if err != nil {
		return err
	}
	partitioned, err := isPartitionedConfig(data)
	if err != nil {
		return err
	}
	if partitioned {
		return finalizePartitionedSetupConfig(cfg)
	}

	pending := *cfg
	pending.SetupState = SetupStateBootstrap
	pending.IsInit = false
	if err := writeConfig(&pending); err != nil {
		return err
	}
	if _, err := migrateLegacyConfig(ROOT_PATH, RuntimeValidationOptions{
		HasSuperAdmin: true,
		Production:    !DevelopmentMode,
	}, true); err != nil {
		return err
	}
	loaded, err := loadPartitionedCompatibility()
	if err != nil {
		return err
	}
	PublishConfigSnapshot(loaded)
	return nil
}

func finalizePartitionedSetupConfig(cfg *Config) error {
	data := *cfg
	data.SetupState = SetupStateReady
	data.IsInit = true
	return writeSetupConfigUnlocked(&data)
}
