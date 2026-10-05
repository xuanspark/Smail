package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"git.pcux.cn/xuans/Smail/utils/credential"
)

// publishedConfig 是旧配置模型的只读兼容快照。
// 新业务应通过 CurrentConfig 读取，禁止在请求处理中直接持有并修改 Instance。
var publishedConfig atomic.Pointer[Config]
var hasPublishedConfig atomic.Bool
var setupPort atomic.Int64

func IsSetupReady() bool {
	value := CurrentConfig()
	if value != nil && value.SetupState.Valid() {
		return value.SetupState == SetupStateReady
	}
	return legacyInitReady()
}

// PublishConfigSnapshot 发布一份深拷贝，供运行时只读访问。
// 调用方仍可继续使用旧的 Instance 兼容路径，但不能把返回值当作可变全局状态。
func PublishConfigSnapshot(value *Config) {
	hasPublishedConfig.Store(true)
	if value == nil {
		publishedConfig.Store(nil)
		return
	}
	clone := cloneConfig(value)
	publishedConfig.Store(clone)
}

// CurrentConfig 返回当前运行时配置的独立副本。
// 返回值可以安全地在业务代码中读取或局部修改，不会改变全局配置快照。
func CurrentConfig() *Config {
	if value := publishedConfig.Load(); value != nil {
		return cloneConfig(value)
	}
	if hasPublishedConfig.Load() {
		return nil
	}
	if Instance == nil {
		return nil
	}
	return cloneConfig(Instance)
}

// CurrentHTTPConfig 返回当前进程实际使用的 HTTP 配置。
// 分区配置存在时优先读取分区配置，旧版配置仅作为兼容回退。
func CurrentHTTPConfig() HTTPConfig {
	if manager := CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		if runtime.Generation != 0 {
			return ResolveHTTPPortDefaults(runtime.Root.HTTP, RuntimeSetupPort())
		}
	}
	cfg := CurrentConfig()
	if cfg == nil {
		return ResolveHTTPPortDefaults(HTTPConfig{}, RuntimeSetupPort())
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.TLSMode))
	return ResolveHTTPPortDefaults(HTTPConfig{
		Port:              cfg.HttpPort,
		HTTPSPort:         cfg.HttpsPort,
		PublicHTTPSPort:   cfg.PublicHTTPSPort,
		HTTPSEnabled:      cfg.HttpsEnabled != 2,
		TLSMode:           mode,
		TrustedProxyCIDRs: append([]string(nil), cfg.TrustedProxyCIDRs...),
	}, cfg.GetSetupPort())
}

func ResolveHTTPPortDefaults(value HTTPConfig, setupPort int) HTTPConfig {
	result := cloneHTTPConfig(value)
	result.TLSMode = strings.ToLower(strings.TrimSpace(result.TLSMode))
	if override := environmentPort("SMAIL_HTTP_PORT_OVERRIDE"); override != 0 {
		result.Port = override
	} else if result.Port == 0 {
		result.Port = environmentPort("SMAIL_HTTP_PORT")
		if result.Port == 0 && validServicePort(setupPort) {
			result.Port = setupPort
		}
		if result.Port == 0 {
			result.Port = 80
		}
	}
	if override := strings.ToLower(strings.TrimSpace(os.Getenv("SMAIL_HTTP_TLS_MODE_OVERRIDE"))); override == HTTPSTLSModeDirect || override == HTTPSTLSModeProxy {
		result.TLSMode = override
	}
	if result.TLSMode == HTTPSTLSModeProxy {
		result.HTTPSPort = 0
		result.HTTPSEnabled = false
		return result
	}
	if result.HTTPSPort == 0 {
		result.HTTPSPort = environmentPort("SMAIL_HTTPS_PORT")
		if result.HTTPSPort == 0 {
			result.HTTPSPort = 443
		}
	}
	return result
}

func environmentPort(name string) int {
	port, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || !validServicePort(port) {
		return 0
	}
	return port
}

func validServicePort(port int) bool {
	return port > 0 && port <= 65535
}

// CurrentWebDomain 返回当前进程实际使用的 Web 域名。
// 分区配置存在时优先读取分区配置，旧版配置仅作为兼容回退。
func CurrentWebDomain() string {
	if manager := CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		if runtime.Generation != 0 {
			return runtime.Root.Domains.Web
		}
	}
	cfg := CurrentConfig()
	if cfg == nil {
		return ""
	}
	return cfg.WebDomain
}

// CurrentAdminRecoveryDomain 仅用于读取旧配置。
// 公网一次性恢复入口统一使用 CurrentWebDomain，不再使用独立恢复域名。
func CurrentAdminRecoveryDomain() string {
	if manager := CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		if runtime.Generation != 0 {
			if domain := runtime.Root.Domains.AdminRecovery; domain != "" {
				return domain
			}
			return runtime.Root.Domains.Primary
		}
	}
	cfg := CurrentConfig()
	if cfg == nil {
		return ""
	}
	if cfg.AdminRecoveryDomain != "" {
		return cfg.AdminRecoveryDomain
	}
	return cfg.Domain
}

// SetRuntimeSetupPort stores the temporary setup listener port outside the persisted configuration.
func SetRuntimeSetupPort(port int) {
	setupPort.Store(int64(port))
}

func RuntimeSetupPort() int {
	return int(setupPort.Load())
}

func cloneConfig(source *Config) *Config {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Domains = append([]string(nil), source.Domains...)
	clone.AccessHosts = append([]string(nil), source.AccessHosts...)
	clone.TrustedProxyCIDRs = append([]string(nil), source.TrustedProxyCIDRs...)
	clone.PluginAllowList = append([]string(nil), source.PluginAllowList...)
	if source.Tables != nil {
		clone.Tables = make(map[string]string, len(source.Tables))
		for key, value := range source.Tables {
			clone.Tables[key] = value
		}
	}
	if source.TablesInitData != nil {
		clone.TablesInitData = make(map[string]string, len(source.TablesInitData))
		for key, value := range source.TablesInitData {
			clone.TablesInitData[key] = value
		}
	}
	return &clone
}

type MailPorts struct {
	SMTP         int
	SMTPSTARTTLS int
	SMTPTLS      int
	IMAP         int
	POP3         int
	POP3TLS      int
}

type MailDomains struct {
	Primary  string
	Accepted []string
}

func CurrentMailDomains() MailDomains {
	manager := CurrentManager()
	if manager != nil {
		runtime := manager.Snapshot()
		if runtime.Generation != 0 {
			return MailDomains{
				Primary:  runtime.Root.Domains.Primary,
				Accepted: append([]string(nil), runtime.Root.Domains.Accepted...),
			}
		}
	}

	cfg := CurrentConfig()
	if cfg == nil {
		return MailDomains{}
	}
	domains := MailDomains{
		Primary:  cfg.Domain,
		Accepted: append([]string(nil), cfg.Domains...),
	}
	if domains.Primary == "" && len(domains.Accepted) > 0 {
		domains.Primary = domains.Accepted[0]
	}
	if len(domains.Accepted) == 0 && domains.Primary != "" {
		domains.Accepted = []string{domains.Primary}
	}
	return domains
}

func CurrentEmailPolicy() EmailConfig {
	manager := CurrentManager()
	if manager != nil {
		runtime := manager.Snapshot()
		if runtime.Generation != 0 {
			return runtime.Email.Policy
		}
	}

	cfg := CurrentConfig()
	if cfg == nil {
		return EmailConfig{}
	}
	return EmailConfig{
		SMTPPort:                cfg.SMTPPort,
		SpamFilterLevel:         cfg.SpamFilterLevel,
		LoginVerificationPolicy: cfg.LoginPolicy,
		ExternalRecoveryEnabled: cfg.ExternalRecoveryEnabled,
	}
}

type SMTPRuntimeSettings struct {
	Relay                   SMTPConfig
	Password                string
	VerificationEnabled     bool
	ExternalRecoveryEnabled bool
}

func CurrentSMTPRuntimeSettings() (SMTPRuntimeSettings, error) {
	settings := SMTPRuntimeSettings{}
	if manager := CurrentManager(); manager != nil {
		runtime := manager.Snapshot()
		if runtime.Generation != 0 {
			settings.Relay = runtime.SMTP.Relay
			settings.ExternalRecoveryEnabled = runtime.Email.Policy.ExternalRecoveryEnabled
			if settings.Relay.Username != "" {
				vault, err := credential.NewVault(ROOT_PATH)
				if err != nil {
					return SMTPRuntimeSettings{}, fmt.Errorf("create SMTP credential vault: %w", err)
				}
				password, err := OpenStoredSecret(vault, settings.Relay.Password)
				if err != nil {
					return SMTPRuntimeSettings{}, fmt.Errorf("open SMTP relay password: %w", err)
				}
				settings.Password = password
			}
			settings.VerificationEnabled = settings.Relay.VerificationEnabled
			return settings, nil
		}
	}

	cfg := CurrentConfig()
	if cfg == nil {
		return SMTPRuntimeSettings{}, fmt.Errorf("runtime configuration is unavailable")
	}
	settings.Relay = SMTPConfig{
		RelayEnabled:        cfg.SMTPHost != "" || cfg.SMTPPort != 0 || cfg.SMTPUsername != "" || cfg.SMTPFrom != "" || cfg.SMTPPassword != "",
		RelayHost:           cfg.SMTPHost,
		RelayPort:           cfg.SMTPPort,
		Username:            cfg.SMTPUsername,
		Security:            cfg.SMTPSecurity,
		From:                cfg.SMTPFrom,
		FromDomain:          cfg.SMTPFromDomain,
		VerificationEnabled: cfg.VerificationEnabled,
		LoginPolicy:         cfg.LoginPolicy,
	}
	settings.Password = cfg.SMTPPassword
	settings.VerificationEnabled = cfg.VerificationEnabled
	settings.ExternalRecoveryEnabled = cfg.ExternalRecoveryEnabled
	return settings, nil
}

func CurrentMailPorts() MailPorts {
	if manager := CurrentManager(); manager != nil {
		if runtime := manager.Snapshot(); runtime.Generation != 0 {
			return ResolveMailPorts(runtime.SMTP, runtime.Email)
		}
	}
	// 旧版 SMTPPort 属于发信中继，不能用作本机监听端口。
	return ResolveMailPorts(SMTPSectionConfig{}, EmailSectionConfig{})
}

func CurrentMailSecurity() string {
	if manager := CurrentManager(); manager != nil {
		if runtime := manager.Snapshot(); runtime.Generation != 0 {
			return ResolveMailSecurity(runtime.SMTP.Listener)
		}
	}
	return MailSecurityBoth
}
func cloneHTTPConfig(source HTTPConfig) HTTPConfig {
	clone := source
	clone.TrustedProxyCIDRs = append([]string(nil), source.TrustedProxyCIDRs...)
	return clone
}
