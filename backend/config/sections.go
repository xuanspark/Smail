package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

type Section string

const (
	SectionRoot    Section = "root"
	SectionSMTP    Section = "smtp"
	SectionEmail   Section = "email"
	SectionSSL     Section = "ssl"
	SectionPlugins Section = "plugins"
)

const CurrentSectionSchema uint32 = 1

type SectionRef struct {
	File       string `json:"file"`
	Schema     uint32 `json:"schema"`
	Generation uint64 `json:"generation"`
	SHA256     string `json:"sha256"`
}

type SectionMeta struct {
	Schema     uint32 `json:"schema"`
	Generation uint64 `json:"generation"`
}

type DomainConfig struct {
	Primary     string   `json:"primary,omitempty"`
	Accepted    []string `json:"accepted,omitempty"`
	Web         string   `json:"web,omitempty"`
	AccessHosts []string `json:"accessHosts,omitempty"`
}

type HTTPConfig struct {
	Port              int      `json:"port,omitempty"`
	HTTPSPort         int      `json:"httpsPort,omitempty"`
	HTTPSEnabled      bool     `json:"httpsEnabled"`
	TLSMode           string   `json:"tlsMode,omitempty"`
	TrustedProxyCIDRs []string `json:"trustedProxyCidrs,omitempty"`
}

const (
	HTTPSTLSModeDirect = "direct"
	HTTPSTLSModeProxy  = "proxy"
)

type SMTPSectionConfig struct {
	Meta     SectionMeta        `json:"meta"`
	Relay    SMTPConfig         `json:"relay"`
	Listener SMTPListenerConfig `json:"listener,omitempty"`
}

type EmailSectionConfig struct {
	Meta   SectionMeta        `json:"meta"`
	Policy EmailConfig        `json:"policy"`
	IMAP   IMAPListenerConfig `json:"imap,omitempty"`
	POP3   POP3ListenerConfig `json:"pop3,omitempty"`
}

type SSLSectionConfig struct {
	Meta SectionMeta `json:"meta"`
	TLS  SSLConfig   `json:"tls"`
}

type PluginNetworkTarget struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type PluginPermissions struct {
	Capabilities []string              `json:"capabilities,omitempty"`
	Network      []PluginNetworkTarget `json:"network,omitempty"`
}

type PluginInstance struct {
	Enabled     bool              `json:"enabled"`
	Executable  string            `json:"executable"`
	Permissions PluginPermissions `json:"permissions"`
	Secret      StoredSecret      `json:"secret,omitempty"`
	Settings    json.RawMessage   `json:"settings,omitempty"`
}

type PluginsSectionConfig struct {
	Meta    SectionMeta               `json:"meta"`
	Plugins map[string]PluginInstance `json:"plugins"`
}

type RuntimeConfig struct {
	Generation uint64
	SetupState SetupState
	Root       RootConfig
	SMTP       SMTPSectionConfig
	Email      EmailSectionConfig
	SSL        SSLSectionConfig
	Plugins    PluginsSectionConfig
	LoadedAt   time.Time
}

type sectionDefinition struct {
	File   string
	Schema uint32
}

var fixedSectionDefinitions = map[Section]sectionDefinition{
	SectionRoot:    {File: "config.json", Schema: CurrentConfigVersion},
	SectionSMTP:    {File: "smtp.json", Schema: CurrentSectionSchema},
	SectionEmail:   {File: "email.json", Schema: CurrentSectionSchema},
	SectionSSL:     {File: "ssl.json", Schema: CurrentSectionSchema},
	SectionPlugins: {File: "plugins.json", Schema: CurrentSectionSchema},
}

var requiredPartitionSections = []Section{SectionSMTP, SectionEmail, SectionSSL, SectionPlugins}

var (
	ErrInvalidSection            = errors.New("invalid configuration section")
	ErrInvalidSectionReference   = errors.New("invalid configuration section reference")
	ErrSectionGenerationMismatch = errors.New("configuration section generation mismatch")
)

func FixedSectionFile(section Section) (string, bool) {
	definition, ok := fixedSectionDefinitions[section]
	return definition.File, ok
}

func FixedSectionSchema(section Section) (uint32, bool) {
	definition, ok := fixedSectionDefinitions[section]
	return definition.Schema, ok
}

func RequiredPartitionSections() []Section {
	result := append([]Section(nil), requiredPartitionSections...)
	return result
}

func ValidateSectionReferences(references map[Section]SectionRef, generation uint64) error {
	if generation == 0 || len(references) != len(requiredPartitionSections) {
		return ErrInvalidSectionReference
	}
	for section := range references {
		if section == SectionRoot {
			return ErrInvalidSectionReference
		}
		if _, ok := fixedSectionDefinitions[section]; !ok {
			return ErrInvalidSectionReference
		}
	}
	for _, section := range requiredPartitionSections {
		reference, ok := references[section]
		if !ok {
			return ErrInvalidSectionReference
		}
		definition := fixedSectionDefinitions[section]
		if reference.File != definition.File || reference.Schema != definition.Schema {
			return ErrInvalidSectionReference
		}
		if reference.Generation != generation {
			return ErrSectionGenerationMismatch
		}
		if !validSHA256(reference.SHA256) {
			return ErrInvalidSectionReference
		}
	}
	return nil
}

func ValidateSectionMeta(section Section, meta SectionMeta, expectedGeneration uint64) error {
	definition, ok := fixedSectionDefinitions[section]
	if !ok || section == SectionRoot || meta.Schema != definition.Schema {
		return ErrInvalidSection
	}
	if meta.Generation == 0 || meta.Generation != expectedGeneration {
		return ErrSectionGenerationMismatch
	}
	return nil
}

func SectionDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func OrderedPartitionSections() []Section {
	result := RequiredPartitionSections()
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return false
	}
	return value == hex.EncodeToString(decoded)
}
