package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/credential"
	"github.com/go-sql-driver/mysql"
)

// isPartitionedConfig 在旧版解码前识别分区根文件，避免静默丢失数据库等字段。
func isPartitionedConfig(data []byte) (bool, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return false, err
	}
	_, sections := fields["sections"]
	_, database := fields["database"]
	return sections || database, nil
}

// loadPartitionedCompatibility 仅构造旧业务所需的只读快照，不重写分区配置。
func loadPartitionedCompatibility() (*Config, error) {
	runtime, err := NewConfigLoader(ROOT_PATH, ConfigLoaderOptions{
		Validation: RuntimeValidationOptions{HasSuperAdmin: true, Production: !DevelopmentMode},
	}).Load()
	if err != nil {
		return nil, err
	}
	return runtimeCompatibility(runtime)
}

// runtimeCompatibility 从同一已验证候选构造兼容快照，避免提交成功后再次读盘产生新失败窗口。
func runtimeCompatibility(runtime RuntimeConfig) (*Config, error) {
	vault, err := credential.NewVault(ROOT_PATH)
	if err != nil {
		return nil, err
	}
	if err := ValidateRuntimeSecrets(vault, runtime); err != nil {
		return nil, err
	}
	driver, dsn, err := compatibilityDatabase(runtime.Root.Database, vault)
	if err != nil {
		return nil, err
	}
	root := runtime.Root
	cfg := &Config{
		LogLevel: root.LogLevel, Domain: root.Domains.Primary,
		Domains: append([]string(nil), root.Domains.Accepted...), WebDomain: root.Domains.Web,
		AccessHosts: append([]string(nil), root.Domains.AccessHosts...),
		DbType:      driver, DbDSN: dsn, SetupState: runtime.SetupState,
		IsInit:   runtime.SetupState == SetupStateReady,
		HttpPort: root.HTTP.Port, HttpsPort: root.HTTP.HTTPSPort,
		HttpsEnabled: 2, TLSMode: root.HTTP.TLSMode,
		TrustedProxyCIDRs:       append([]string(nil), root.HTTP.TrustedProxyCIDRs...),
		DkimPrivateKeyPath:      filepath.Join(ROOT_PATH, "config", "dkim", "dkim.priv"),
		SSLType:                 SSLTypeUser,
		SpamFilterLevel:         runtime.Email.Policy.SpamFilterLevel,
		LoginPolicy:             runtime.SMTP.Relay.LoginPolicy,
		ExternalRecoveryEnabled: runtime.Email.Policy.ExternalRecoveryEnabled,
		SMTPHost:                runtime.SMTP.Relay.RelayHost, SMTPPort: runtime.SMTP.Relay.RelayPort,
		SMTPUsername: runtime.SMTP.Relay.Username, SMTPSecurity: runtime.SMTP.Relay.Security,
		SMTPFrom: runtime.SMTP.Relay.From, SMTPFromDomain: runtime.SMTP.Relay.FromDomain,
		VerificationEnabled: runtime.SMTP.Relay.VerificationEnabled,
	}
	if root.HTTP.HTTPSEnabled {
		cfg.HttpsEnabled = 1
	}
	if runtime.SMTP.Relay.Password.Configured() {
		cfg.SMTPPassword, err = OpenStoredSecret(vault, runtime.SMTP.Relay.Password)
		if err != nil {
			return nil, err
		}
	}
	tls := runtime.SSL.TLS
	if EffectiveSSLManagement(tls) == SSLManagementProject {
		if tls.Challenge == SSLChallengeDNS {
			cfg.SSLType = SSLTypeAutoDNS
		} else {
			cfg.SSLType = SSLTypeAutoHTTP
		}
	}
	// 迁移器保留的旧证书引用仍走严格的只读文件校验；新托管引用沿用托管解析器。
	if strings.HasPrefix(tls.CertificateRef, managedSSLRelativeRoot+"/") {
		cfg.SSL = tls
	} else {
		cfg.SSLPublicKeyPath = resolvePath(tls.CertificateRef)
		cfg.SSLPrivateKeyPath = resolvePath(tls.PrivateKeyRef)
	}
	return cfg, nil
}

func compatibilityDatabase(database DatabaseConfig, vault *credential.Vault) (string, string, error) {
	if database.Type == DatabaseTypeSQLite {
		return DBTypeSQLite, filepath.Join(ROOT_PATH, "config", database.LocalRef), nil
	}
	password, err := OpenStoredSecret(vault, database.Password)
	if err != nil {
		return "", "", err
	}
	address := net.JoinHostPort(database.Host, strconv.Itoa(int(database.Port)))
	switch database.Type {
	case DatabaseTypeMySQL:
		settings := mysql.NewConfig()
		settings.User, settings.Passwd = database.Username, password
		settings.Net, settings.Addr, settings.DBName = "tcp", address, database.Name
		settings.ParseTime = true
		// 与迁移连接保持相同时区，避免重启后 TIMESTAMP 会话到期时间偏移。
		settings.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
		return DBTypeMySQL, settings.FormatDSN(), nil
	case DatabaseTypePostgreSQL:
		uri := url.URL{Scheme: "postgres", User: url.UserPassword(database.Username, password), Host: address, Path: "/" + database.Name}
		uri.RawQuery = "sslmode=require&timezone=UTC&search_path=public"
		return DBTypePostgres, uri.String(), nil
	default:
		return "", "", fmt.Errorf("不支持的数据库类型")
	}
}
