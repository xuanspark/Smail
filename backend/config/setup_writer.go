package config

import (
	"fmt"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

// WriteSetupConfig 将安装向导使用的兼容配置安全提交到分区配置。
// 运行配置已经分区化时，安装流程不得直接覆盖 config.json。
func WriteSetupConfig(cfg *Config) error {
	legacyConfigMu.Lock()
	defer legacyConfigMu.Unlock()
	return writeSetupConfigUnlocked(cfg)
}

func writeSetupConfigUnlocked(cfg *Config) error {
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
	if !partitioned {
		return writeConfig(cfg)
	}

	current, err := loadSetupRuntime()
	if err != nil {
		return err
	}
	candidate, err := setupRuntimeFromConfig(current, cfg)
	if err != nil {
		return err
	}
	validation := RuntimeValidationOptions{
		HasSuperAdmin: candidate.SetupState == SetupStateReady,
		Production:    !DevelopmentMode,
	}
	if err := NewTransactionStore(ROOT_PATH, validation).Commit(current, candidate); err != nil {
		return err
	}
	loaded, err := loadPartitionedCompatibilityWithOptions(validation)
	if err != nil {
		return err
	}
	PublishConfigSnapshot(loaded)
	return nil
}

func loadSetupRuntime() (RuntimeConfig, error) {
	production := !DevelopmentMode
	var lastErr error
	for _, hasSuperAdmin := range []bool{false, true} {
		runtime, err := NewConfigLoader(ROOT_PATH, ConfigLoaderOptions{
			Validation: RuntimeValidationOptions{HasSuperAdmin: hasSuperAdmin, Production: production},
		}).Load()
		if err == nil {
			return runtime, nil
		}
		lastErr = err
	}
	return RuntimeConfig{}, lastErr
}

func setupRuntimeFromConfig(current RuntimeConfig, cfg *Config) (RuntimeConfig, error) {
	vault, err := credential.NewVault(ROOT_PATH)
	if err != nil {
		return RuntimeConfig{}, err
	}
	legacy := legacyMigrationConfigFromConfig(cfg)
	database, manualFields, err := legacyDatabaseConfig(ROOT_PATH, vault, legacy)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if len(manualFields) != 0 {
		return RuntimeConfig{}, fmt.Errorf("安装配置无法安全转换：%s", strings.Join(manualFields, ", "))
	}
	relay, manualFields, err := legacySMTPConfig(ROOT_PATH, vault, legacy)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if len(manualFields) != 0 {
		return RuntimeConfig{}, fmt.Errorf("安装配置无法安全转换：%s", strings.Join(manualFields, ", "))
	}
	ssl, manualFields := legacySSLConfig(legacy, true)
	if len(manualFields) != 0 {
		return RuntimeConfig{}, fmt.Errorf("安装配置无法安全转换：%s", strings.Join(manualFields, ", "))
	}

	candidate := cloneRuntimeConfig(current)
	candidate.Generation = current.Generation + 1
	candidate.SetupState = cfg.SetupState
	if !candidate.SetupState.Valid() {
		candidate.SetupState = SetupStateBootstrap
	}
	candidate.Root.Generation = candidate.Generation
	candidate.Root.SetupState = candidate.SetupState
	candidate.Root.LogLevel = cfg.LogLevel
	candidate.Root.Database = database
	candidate.Root.Domains = DomainConfig{
		Primary:       cfg.Domain,
		Accepted:      append([]string(nil), cfg.Domains...),
		Web:           cfg.WebDomain,
		AdminRecovery: cfg.AdminRecoveryDomain,
		AccessHosts:   append([]string(nil), cfg.AccessHosts...),
	}
	httpsEnabled := cfg.HttpsEnabled != 2
	httpsPort := cfg.HttpsPort
	tlsMode := strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	if tlsMode == HTTPSTLSModeProxy {
		httpsEnabled = false
		httpsPort = 0
	}
	candidate.Root.HTTP = HTTPConfig{
		Port:              cfg.HttpPort,
		HTTPSPort:         httpsPort,
		PublicHTTPSPort:   cfg.PublicHTTPSPort,
		HTTPSEnabled:      httpsEnabled,
		TLSMode:           tlsMode,
		TrustedProxyCIDRs: append([]string(nil), cfg.TrustedProxyCIDRs...),
	}
	candidate.SMTP.Relay = relay
	candidate.SMTP.Relay.VerificationEnabled = cfg.VerificationEnabled
	candidate.SMTP.Relay.LoginPolicy = cfg.LoginPolicy
	candidate.Email.Policy.SpamFilterLevel = cfg.SpamFilterLevel
	candidate.Email.Policy.ExternalRecoveryEnabled = cfg.ExternalRecoveryEnabled
	candidate.SSL.TLS = ssl
	candidate.Root.UpdatedAt = time.Now().UTC()
	setCandidateGeneration(&candidate, candidate.Generation)
	return candidate, nil
}

func legacyMigrationConfigFromConfig(cfg *Config) legacyMigrationConfig {
	return legacyMigrationConfig{
		LogLevel:                cfg.LogLevel,
		Domain:                  cfg.Domain,
		Domains:                 append([]string(nil), cfg.Domains...),
		WebDomain:               cfg.WebDomain,
		AdminRecoveryDomain:     cfg.AdminRecoveryDomain,
		AccessHosts:             append([]string(nil), cfg.AccessHosts...),
		SSLType:                 cfg.SSLType,
		SSLPrivateKeyPath:       cfg.SSLPrivateKeyPath,
		SSLPublicKeyPath:        cfg.SSLPublicKeyPath,
		SSL:                     cfg.SSL,
		DbDSN:                   cfg.DbDSN,
		DbType:                  cfg.DbType,
		HttpsEnabled:            cfg.HttpsEnabled,
		HttpPort:                cfg.HttpPort,
		HttpsPort:               cfg.HttpsPort,
		PublicHTTPSPort:         cfg.PublicHTTPSPort,
		TLSMode:                 cfg.TLSMode,
		TrustedProxyCIDRs:       append([]string(nil), cfg.TrustedProxyCIDRs...),
		SMTPHost:                cfg.SMTPHost,
		SMTPPort:                cfg.SMTPPort,
		SMTPUsername:            cfg.SMTPUsername,
		SMTPPassword:            cfg.SMTPPassword,
		SMTPSecurity:            cfg.SMTPSecurity,
		SMTPFrom:                cfg.SMTPFrom,
		SMTPFromDomain:          cfg.SMTPFromDomain,
		VerificationEnabled:     boolPointer(cfg.VerificationEnabled),
		ExternalRecoveryEnabled: boolPointer(cfg.ExternalRecoveryEnabled),
		LoginPolicy:             cfg.LoginPolicy,
		SetupState:              cfg.SetupState,
		IsInit:                  cfg.IsInit,
	}
}

func boolPointer(value bool) *bool {
	return &value
}
