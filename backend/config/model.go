package config

import "time"

type SetupState string

const (
	SetupStateNew       SetupState = "new"
	SetupStateBootstrap SetupState = "bootstrap"
	SetupStateReady     SetupState = "ready"
	SetupStateDegraded  SetupState = "degraded"
)

type DatabaseType string

const (
	DatabaseTypeMySQL      DatabaseType = "mysql"
	DatabaseTypePostgreSQL DatabaseType = "postgresql"
	DatabaseTypeSQLite     DatabaseType = "sqlite"
)

const (
	DefaultMySQLPort      uint16 = 3306
	DefaultPostgreSQLPort uint16 = 5432
)

type RootConfig struct {
	ConfigVersion uint32                 `json:"configVersion,omitempty"`
	SchemaVersion int                    `json:"schemaVersion,omitempty"`
	Generation    uint64                 `json:"generation"`
	Version       int                    `json:"version,omitempty"`
	SetupState    SetupState             `json:"setupState"`
	LogLevel      string                 `json:"logLevel,omitempty"`
	Database      DatabaseConfig         `json:"database"`
	Domains       DomainConfig           `json:"domains,omitempty"`
	HTTP          HTTPConfig             `json:"http,omitempty"`
	Sections      map[Section]SectionRef `json:"sections,omitempty"`
	SMTP          SMTPConfig             `json:"smtp,omitempty"`
	Email         EmailConfig            `json:"email,omitempty"`
	SSL           SSLConfig              `json:"ssl,omitempty"`
	Plugins       []PluginConfig         `json:"plugins,omitempty"`
	Security      SecurityConfig         `json:"security,omitempty"`
	UpdatedAt     time.Time              `json:"updatedAt"`
}

const (
	CurrentConfigVersion = 1
	CurrentSchemaVersion = 2
)

func normalizeRootConfig(source RootConfig) RootConfig {
	result := cloneRootConfig(source)
	if result.ConfigVersion == 0 {
		result.ConfigVersion = CurrentConfigVersion
	}
	if result.SchemaVersion == 0 {
		result.SchemaVersion = CurrentSchemaVersion
	}
	if result.Generation == 0 {
		if result.Version > 0 {
			result.Generation = uint64(result.Version)
		} else {
			result.Generation = 1
		}
	}
	result.Database = NormalizeDatabaseConfig(result.Database)
	if result.LogLevel == "" {
		result.LogLevel = "info"
	}
	result.Version = 0
	return result
}

type DatabaseConfig struct {
	Type     DatabaseType `json:"type"`
	Host     string       `json:"host,omitempty"`
	Port     uint16       `json:"port,omitempty"`
	Username string       `json:"username,omitempty"`
	Password StoredSecret `json:"password,omitempty"`
	Name     string       `json:"name,omitempty"`
	LocalRef string       `json:"localRef,omitempty"`
}

type SMTPConfig struct {
	RelayHost           string       `json:"relayHost"`
	RelayPort           int          `json:"relayPort"`
	Username            string       `json:"username"`
	Password            StoredSecret `json:"password,omitempty"`
	Security            string       `json:"security"`
	From                string       `json:"from"`
	FromDomain          string       `json:"fromDomain"`
	VerificationEnabled bool         `json:"verificationEnabled"`
	LoginPolicy         string       `json:"loginPolicy"`
}

type EmailConfig struct {
	SMTPPort                int    `json:"smtpPort,omitempty"`
	IMAPPort                int    `json:"imapPort,omitempty"`
	POP3Port                int    `json:"pop3Port,omitempty"`
	MaxMessageBytes         int64  `json:"maxMessageBytes,omitempty"`
	SpamFilterLevel         int    `json:"spamFilterLevel,omitempty"`
	LoginVerificationPolicy string `json:"loginVerificationPolicy,omitempty"`
	ExternalRecoveryEnabled bool   `json:"externalRecoveryEnabled"`
}

type SSLConfig struct {
	Management       string `json:"management,omitempty"`
	Mode             string `json:"mode"`
	DefaultDirectory string `json:"defaultDirectory"`
	CustomDirectory  string `json:"customDirectory,omitempty"`
	Challenge        string `json:"challenge,omitempty"`
	CertificateRef   string `json:"certificateRef,omitempty"`
	PrivateKeyRef    string `json:"privateKeyRef,omitempty"`
}

type PluginConfig struct {
	LogicalID        string       `json:"logicalId"`
	FileName         string       `json:"fileName"`
	Enabled          bool         `json:"enabled"`
	NetworkAllowlist []string     `json:"networkAllowlist,omitempty"`
	Secret           StoredSecret `json:"secret,omitempty"`
}

type SecurityConfig struct {
	PasswordVersion       string   `json:"passwordVersion"`
	EncryptionKeyRef      string   `json:"encryptionKeyRef"`
	WebAuthnEnabled       bool     `json:"webAuthnEnabled"`
	WebAuthnRPID          string   `json:"webAuthnRpId,omitempty"`
	WebAuthnRPDisplayName string   `json:"webAuthnRpDisplayName,omitempty"`
	WebAuthnRPOrigins     []string `json:"webAuthnRpOrigins,omitempty"`
	ExternalEmailEnabled  bool     `json:"externalEmailEnabled"`
}

func (s SetupState) Valid() bool {
	switch s {
	case SetupStateNew, SetupStateBootstrap, SetupStateReady, SetupStateDegraded:
		return true
	default:
		return false
	}
}

func (t DatabaseType) Valid() bool {
	switch t {
	case DatabaseTypeMySQL, DatabaseTypePostgreSQL, DatabaseTypeSQLite:
		return true
	default:
		return false
	}
}

func DefaultDatabasePort(databaseType DatabaseType) (uint16, bool) {
	switch databaseType {
	case DatabaseTypeMySQL:
		return DefaultMySQLPort, true
	case DatabaseTypePostgreSQL:
		return DefaultPostgreSQLPort, true
	default:
		return 0, false
	}
}

func NormalizeDatabaseConfig(database DatabaseConfig) DatabaseConfig {
	if database.Port == 0 {
		if port, ok := DefaultDatabasePort(database.Type); ok {
			database.Port = port
		}
	}
	return database
}

func (d DatabaseConfig) IsStructured() bool {
	return d.Type == DatabaseTypeSQLite || (d.Host != "" && d.Username != "" && d.Name != "" && d.Password.Configured())
}
