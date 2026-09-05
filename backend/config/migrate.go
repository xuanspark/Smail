package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/plugins/catalog"
	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

var (
	ErrMigrationNotRequired  = errors.New("legacy configuration migration is not required")
	ErrMigrationUnknownField = errors.New("legacy configuration contains unknown fields")
	ErrMigrationUnsafeValue  = errors.New("legacy configuration contains a value that requires manual migration")
	ErrMigrationTargetExists = errors.New("partitioned configuration files already exist")
)

const legacyMigrationSourceCopy = "config.json.legacy"

type LegacyMigrationPlan struct {
	SourceDigest     string
	TargetGeneration uint64
	UnknownFields    []string
	ManualFields     []string
}

type LegacyMigrationResult struct {
	Generation uint64
	BackupPath string
}

type legacyMigrationConfig struct {
	LogLevel                string     `json:"logLevel"`
	Domain                  string     `json:"domain"`
	Domains                 []string   `json:"domains"`
	WebDomain               string     `json:"webDomain"`
	AccessHosts             []string   `json:"accessHosts"`
	DkimPrivateKeyPath      string     `json:"dkimPrivateKeyPath"`
	SSLType                 string     `json:"sslType"`
	SSLPrivateKeyPath       string     `json:"SSLPrivateKeyPath"`
	SSLPublicKeyPath        string     `json:"SSLPublicKeyPath"`
	DbDSN                   string     `json:"dbDSN"`
	DbType                  string     `json:"dbType"`
	HttpsEnabled            int        `json:"httpsEnabled"`
	SpamFilterLevel         int        `json:"spamFilterLevel"`
	HttpPort                int        `json:"httpPort"`
	HttpsPort               int        `json:"httpsPort"`
	TLSMode                 string     `json:"tlsMode"`
	TrustedProxyCIDRs       []string   `json:"trustedProxyCidrs"`
	WeChatPushAppID         string     `json:"weChatPushAppId"`
	WeChatPushSecret        string     `json:"weChatPushSecret"`
	WeChatPushTemplateID    string     `json:"weChatPushTemplateId"`
	WeChatPushUserID        string     `json:"weChatPushUserId"`
	TGBotToken              string     `json:"tgBotToken"`
	TGChatID                string     `json:"tgChatId"`
	IsInit                  bool       `json:"isInit"`
	SetupState              SetupState `json:"setupState"`
	WebPushURL              string     `json:"webPushUrl"`
	WebPushToken            string     `json:"webPushToken"`
	SMTPHost                string     `json:"smtpHost"`
	SMTPPort                int        `json:"smtpPort"`
	SMTPUsername            string     `json:"smtpUsername"`
	SMTPPassword            string     `json:"smtpPassword"`
	SMTPSecurity            string     `json:"smtpSecurity"`
	SMTPFrom                string     `json:"smtpFrom"`
	SMTPFromDomain          string     `json:"smtpFromDomain"`
	VerificationEnabled     bool       `json:"verificationEnabled"`
	ExternalRecoveryEnabled bool       `json:"externalRecoveryEnabled"`
	LoginPolicy             string     `json:"loginPolicy"`
	VerificationLoginPolicy string     `json:"verificationLoginPolicy"`
	PluginAllowList         []string   `json:"pluginAllowList"`
}

var legacyMigrationKnownFields = map[string]struct{}{
	"logLevel": {}, "domain": {}, "domains": {}, "webDomain": {}, "accessHosts": {},
	"dkimPrivateKeyPath": {}, "sslType": {}, "SSLPrivateKeyPath": {}, "SSLPublicKeyPath": {},
	"dbDSN": {}, "dbType": {}, "httpsEnabled": {}, "spamFilterLevel": {}, "httpPort": {}, "httpsPort": {}, "tlsMode": {}, "trustedProxyCidrs": {},
	"weChatPushAppId": {}, "weChatPushSecret": {}, "weChatPushTemplateId": {}, "weChatPushUserId": {},
	"tgBotToken": {}, "tgChatId": {}, "isInit": {}, "setupState": {}, "webPushUrl": {}, "webPushToken": {},
	"smtpHost": {}, "smtpPort": {}, "smtpUsername": {}, "smtpPassword": {}, "smtpSecurity": {}, "smtpFrom": {},
	"smtpFromDomain": {}, "verificationEnabled": {}, "externalRecoveryEnabled": {}, "loginPolicy": {},
	"verificationLoginPolicy": {}, "pluginAllowList": {},
}

var legacyPluginExecutableMap = map[string]string{
	"spam_block":              "spam_block",
	"spam_block.exe":          "spam_block",
	"wechat_push":             "wechat_push",
	"wechat_push.exe":         "wechat_push",
	"telegram_push":           "telegram_push",
	"telegram_push.exe":       "telegram_push",
	"pmail_telegram_push":     "telegram_push",
	"pmail_telegram_push.exe": "telegram_push",
}

var legacyMySQLDSNPattern = regexp.MustCompile(`^([^:@/[:space:]]+):([^@\r\n]*)@tcp\(([^():/[:space:]]+)(?::([0-9]{1,5}))?\)/([A-Za-z0-9][A-Za-z0-9._-]{0,127})(?:\?[^#[:space:]]*)?$`)

func PlanLegacyMigration(root string, _ RuntimeValidationOptions) (LegacyMigrationPlan, error) {
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		return LegacyMigrationPlan{}, err
	}
	data, err := readManagerConfigFile(directory, "config.json")
	if err != nil {
		return LegacyMigrationPlan{}, err
	}
	var raw map[string]json.RawMessage
	if err := ParseStrict(data, &raw); err != nil {
		return LegacyMigrationPlan{}, err
	}
	if _, exists := raw["sections"]; exists {
		return LegacyMigrationPlan{}, ErrMigrationNotRequired
	}
	plan := LegacyMigrationPlan{SourceDigest: SectionDigest(data), TargetGeneration: 1}
	for field := range raw {
		if _, known := legacyMigrationKnownFields[field]; !known {
			plan.UnknownFields = append(plan.UnknownFields, field)
		}
	}
	sort.Strings(plan.UnknownFields)
	if len(plan.UnknownFields) > 0 {
		return plan, ErrMigrationUnknownField
	}
	var legacy legacyMigrationConfig
	if err := json.Unmarshal(data, &legacy); err != nil {
		return LegacyMigrationPlan{}, err
	}
	plan.ManualFields = legacyManualFields(legacy)
	if len(plan.ManualFields) > 0 {
		return plan, ErrMigrationUnsafeValue
	}
	return plan, nil
}
func MigrateLegacyConfig(root string, options RuntimeValidationOptions) (LegacyMigrationResult, error) {
	plan, err := PlanLegacyMigration(root, options)
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	directory, err := secureManagerConfigDirectory(root)
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	legacyData, err := readManagerConfigFile(directory, "config.json")
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	if SectionDigest(legacyData) != plan.SourceDigest {
		return LegacyMigrationResult{}, ErrGenerationConflict
	}
	var legacy legacyMigrationConfig
	if err := json.Unmarshal(legacyData, &legacy); err != nil {
		return LegacyMigrationResult{}, err
	}
	runtime, manualFields, err := legacyRuntimeConfig(root, legacy)
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	if len(manualFields) > 0 {
		return LegacyMigrationResult{}, ErrMigrationUnsafeValue
	}
	if err := resolveLegacySetupState(&runtime, legacy, options); err != nil {
		return LegacyMigrationResult{}, err
	}
	if err := ValidateRuntimeConfig(runtime, options); err != nil {
		return LegacyMigrationResult{}, err
	}
	backupPath, err := writeLegacyMigrationBackup(directory, legacyData, time.Now().UTC())
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	if err := preserveLegacyMigrationSource(directory, legacyData); err != nil {
		return LegacyMigrationResult{}, err
	}
	files, err := marshalRuntimeFilesWithPluginSecrets(runtime, true)
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	vault, err := credential.NewVault(root)
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	resolvedFiles := make(map[string][]byte, len(files))
	for name, data := range files {
		resolvedFiles[name] = data
	}
	for _, section := range RequiredPartitionSections() {
		name, _ := FixedSectionFile(section)
		resolved, resolveErr := resolveLegacyMigrationSection(directory, section, files[name], vault)
		if resolveErr != nil {
			return LegacyMigrationResult{}, resolveErr
		}
		resolvedFiles[name] = resolved
	}
	files = resolvedFiles
	if err := refreshLegacyMigrationRoot(runtime, files); err != nil {
		return LegacyMigrationResult{}, err
	}
	for _, section := range RequiredPartitionSections() {
		name, _ := FixedSectionFile(section)
		path := filepath.Join(directory, name)
		if _, statErr := os.Lstat(path); os.IsNotExist(statErr) {
			if err := atomicfile.Write(path, files[name], 0600); err != nil {
				return LegacyMigrationResult{}, err
			}
		} else if statErr != nil {
			return LegacyMigrationResult{}, statErr
		}
	}
	rootPath := filepath.Join(directory, "config.json")
	currentRoot, err := readManagerConfigFile(directory, "config.json")
	if err != nil {
		return LegacyMigrationResult{}, err
	}
	if !bytes.Equal(currentRoot, legacyData) && !bytes.Equal(currentRoot, files["config.json"]) {
		return LegacyMigrationResult{}, ErrGenerationConflict
	}
	if bytes.Equal(currentRoot, legacyData) {
		if err := atomicfile.Write(rootPath, files["config.json"], 0600); err != nil {
			return LegacyMigrationResult{}, err
		}
	}
	if err := atomicfile.SyncDirectory(directory); err != nil {
		return LegacyMigrationResult{}, err
	}
	return LegacyMigrationResult{Generation: runtime.Generation, BackupPath: backupPath}, nil
}

func refreshLegacyMigrationRoot(runtime RuntimeConfig, files map[string][]byte) error {
	references := make(map[Section]SectionRef, len(requiredPartitionSections))
	for _, section := range requiredPartitionSections {
		name, ok := FixedSectionFile(section)
		if !ok {
			return ErrInvalidSection
		}
		schema, ok := FixedSectionSchema(section)
		if !ok {
			return ErrInvalidSection
		}
		data, ok := files[name]
		if !ok {
			return ErrConfigSectionMissing
		}
		references[section] = SectionRef{File: name, Schema: schema, Generation: runtime.Generation, SHA256: SectionDigest(data)}
	}
	runtime.Root.Generation = runtime.Generation
	runtime.Root.SetupState = runtime.SetupState
	runtime.Root.Sections = references
	data, err := json.Marshal(runtime.Root)
	if err != nil {
		return err
	}
	files["config.json"] = data
	return nil
}

func resolveLegacyMigrationSection(directory string, section Section, expected []byte, vault *credential.Vault) ([]byte, error) {
	name, ok := FixedSectionFile(section)
	if !ok || section == SectionRoot {
		return nil, ErrInvalidSection
	}
	existing, err := readManagerConfigFile(directory, name)
	if err != nil {
		if os.IsNotExist(err) {
			return expected, nil
		}
		return nil, err
	}
	if bytes.Equal(existing, expected) {
		return existing, nil
	}
	equivalent, err := equivalentLegacyMigrationSection(section, expected, existing, vault)
	if err != nil {
		return nil, err
	}
	if !equivalent {
		return nil, ErrMigrationTargetExists
	}
	return existing, nil
}

func equivalentLegacyMigrationSection(section Section, expected, existing []byte, vault *credential.Vault) (bool, error) {
	switch section {
	case SectionSMTP:
		var expectedConfig, existingConfig SMTPSectionConfig
		if err := ParseStrict(expected, &expectedConfig); err != nil {
			return false, err
		}
		if err := ParseStrict(existing, &existingConfig); err != nil {
			return false, err
		}
		if !equivalentStoredSecret(expectedConfig.Relay.Password, existingConfig.Relay.Password, vault) {
			return false, nil
		}
		expectedConfig.Relay.Password = StoredSecret{}
		existingConfig.Relay.Password = StoredSecret{}
		return reflect.DeepEqual(expectedConfig, existingConfig), nil
	case SectionPlugins:
		var expectedConfig, existingConfig PluginsSectionConfig
		if err := ParseStrict(expected, &expectedConfig); err != nil {
			return false, err
		}
		if err := ParseStrict(existing, &existingConfig); err != nil {
			return false, err
		}
		if len(expectedConfig.Plugins) != len(existingConfig.Plugins) {
			return false, nil
		}
		for logicalID, expectedPlugin := range expectedConfig.Plugins {
			existingPlugin, ok := existingConfig.Plugins[logicalID]
			if !ok || !equivalentStoredSecret(expectedPlugin.Secret, existingPlugin.Secret, vault) {
				return false, nil
			}
			expectedPlugin.Secret = StoredSecret{}
			existingPlugin.Secret = StoredSecret{}
			if !reflect.DeepEqual(expectedPlugin, existingPlugin) {
				return false, nil
			}
		}
		return reflect.DeepEqual(expectedConfig.Meta, existingConfig.Meta), nil
	case SectionEmail:
		var expectedConfig, existingConfig EmailSectionConfig
		if err := ParseStrict(expected, &expectedConfig); err != nil {
			return false, err
		}
		if err := ParseStrict(existing, &existingConfig); err != nil {
			return false, err
		}
		return reflect.DeepEqual(expectedConfig, existingConfig), nil
	case SectionSSL:
		var expectedConfig, existingConfig SSLSectionConfig
		if err := ParseStrict(expected, &expectedConfig); err != nil {
			return false, err
		}
		if err := ParseStrict(existing, &existingConfig); err != nil {
			return false, err
		}
		return reflect.DeepEqual(expectedConfig, existingConfig), nil
	default:
		return false, ErrInvalidSection
	}
}

func equivalentStoredSecret(expected, existing StoredSecret, vault *credential.Vault) bool {
	if expected.Configured() != existing.Configured() {
		return false
	}
	if !expected.Configured() {
		return true
	}
	expectedPlaintext, expectedErr := OpenStoredSecret(vault, expected)
	existingPlaintext, existingErr := OpenStoredSecret(vault, existing)
	return expectedErr == nil && existingErr == nil && expectedPlaintext == existingPlaintext
}

func legacyManualFields(legacy legacyMigrationConfig) []string {
	fields := make([]string, 0)
	_, _, databaseManualFields := legacyDatabasePlan(legacy)
	fields = append(fields, databaseManualFields...)
	if _, ok := legacyControlledReference(legacy.SSLPublicKeyPath); legacy.SSLPublicKeyPath != "" && !ok {
		fields = append(fields, "SSLPublicKeyPath")
	}
	if _, ok := legacyControlledReference(legacy.SSLPrivateKeyPath); legacy.SSLPrivateKeyPath != "" && !ok {
		fields = append(fields, "SSLPrivateKeyPath")
	}
	if legacy.SMTPUsername == "" && legacy.SMTPPassword != "" {
		fields = append(fields, "smtpPassword")
	}
	for field, value := range map[string]string{
		"smtpPassword": legacy.SMTPPassword, "weChatPushSecret": legacy.WeChatPushSecret, "tgBotToken": legacy.TGBotToken,
	} {
		if value != "" && strings.TrimSpace(value) != value {
			fields = append(fields, field)
		}
	}
	wechatEnabled := legacy.WeChatPushAppID != "" || legacy.WeChatPushSecret != "" || legacy.WeChatPushTemplateID != "" || legacy.WeChatPushUserID != ""
	if wechatEnabled && legacy.WeChatPushSecret == "" {
		fields = append(fields, "weChatPushSecret")
	}
	telegramEnabled := legacy.TGBotToken != "" || legacy.TGChatID != ""
	if telegramEnabled && legacy.TGBotToken == "" {
		fields = append(fields, "tgBotToken")
	}
	if legacy.TGChatID != "" {
		fields = append(fields, "tgChatId")
	}
	if legacy.WebPushURL != "" {
		fields = append(fields, "webPushUrl")
	}
	if legacy.WebPushToken != "" {
		fields = append(fields, "webPushToken")
	}
	enabledPlugins, pluginManualFields := legacyPluginEnabledSet(legacy.PluginAllowList)
	fields = append(fields, pluginManualFields...)
	if enabledPlugins["wechat_push"] && !wechatEnabled {
		fields = append(fields, "weChatPushSecret")
	}
	if enabledPlugins["telegram_push"] && !telegramEnabled {
		fields = append(fields, "tgBotToken")
	}
	sort.Strings(fields)
	return compactMigrationFields(fields)
}

func compactMigrationFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	result := fields[:0]
	for _, field := range fields {
		if len(result) == 0 || result[len(result)-1] != field {
			result = append(result, field)
		}
	}
	return result
}
func legacyRuntimeConfig(root string, legacy legacyMigrationConfig) (RuntimeConfig, []string, error) {
	vault, err := credential.NewVault(root)
	if err != nil {
		return RuntimeConfig{}, nil, err
	}
	manualFields := make([]string, 0)
	database, databaseManual, err := legacyDatabaseConfig(root, vault, legacy)
	if err != nil {
		return RuntimeConfig{}, nil, err
	}
	manualFields = append(manualFields, databaseManual...)
	generation := uint64(1)
	setupState := legacy.SetupState
	if !setupState.Valid() {
		if legacy.IsInit {
			setupState = SetupStateReady
		} else {
			setupState = SetupStateNew
		}
	}
	accepted := append([]string(nil), legacy.Domains...)
	if len(accepted) == 0 && legacy.Domain != "" {
		accepted = []string{legacy.Domain}
	}
	if legacy.Domain == "" && len(accepted) > 0 {
		legacy.Domain = accepted[0]
	}
	httpsEnabled := legacy.HttpsEnabled != 2
	httpsPort := legacy.HttpsPort
	tlsMode := strings.ToLower(strings.TrimSpace(legacy.TLSMode))
	if tlsMode == HTTPSTLSModeProxy {
		httpsEnabled = false
		httpsPort = 0
	}
	if httpsEnabled && httpsPort == 0 {
		httpsPort = 443
	}
	if !httpsEnabled {
		httpsPort = 0
	}
	ssl, sslManual := legacySSLConfig(legacy)
	manualFields = append(manualFields, sslManual...)
	relay, relayManual, err := legacySMTPConfig(root, vault, legacy)
	if err != nil {
		return RuntimeConfig{}, nil, err
	}
	manualFields = append(manualFields, relayManual...)
	plugins, pluginManual, err := legacyPluginsConfig(root, vault, legacy)
	if err != nil {
		return RuntimeConfig{}, nil, err
	}
	manualFields = append(manualFields, pluginManual...)
	rootConfig := RootConfig{
		ConfigVersion: CurrentConfigVersion,
		SchemaVersion: CurrentSchemaVersion,
		Generation:    generation,
		SetupState:    setupState,
		LogLevel:      legacy.LogLevel,
		Database:      database,
		Domains: DomainConfig{
			Primary:     legacy.Domain,
			Accepted:    accepted,
			Web:         legacy.WebDomain,
			AccessHosts: append([]string(nil), legacy.AccessHosts...),
		},
		HTTP: HTTPConfig{
			Port:              legacy.HttpPort,
			HTTPSPort:         httpsPort,
			HTTPSEnabled:      httpsEnabled,
			TLSMode:           tlsMode,
			TrustedProxyCIDRs: append([]string(nil), legacy.TrustedProxyCIDRs...),
		},
		Security:  SecurityConfig{PasswordVersion: "argon2id", EncryptionKeyRef: "credential.key"},
		UpdatedAt: time.Now().UTC(),
	}
	meta := SectionMeta{Schema: CurrentSectionSchema, Generation: generation}
	runtime := RuntimeConfig{
		Generation: generation,
		SetupState: setupState,
		Root:       rootConfig,
		SMTP:       SMTPSectionConfig{Meta: meta, Relay: relay},
		Email: EmailSectionConfig{Meta: meta, Policy: EmailConfig{
			SMTPPort:                25,
			IMAPPort:                143,
			POP3Port:                110,
			SpamFilterLevel:         legacy.SpamFilterLevel,
			LoginVerificationPolicy: legacyLoginPolicy(legacy),
			ExternalRecoveryEnabled: legacy.ExternalRecoveryEnabled,
		}},
		SSL:      SSLSectionConfig{Meta: meta, TLS: ssl},
		Plugins:  PluginsSectionConfig{Meta: meta, Plugins: plugins},
		LoadedAt: time.Now().UTC(),
	}
	prepared, err := prepareLegacyRuntime(runtime)
	if err != nil {
		return RuntimeConfig{}, nil, err
	}
	return prepared, manualFields, nil
}

func resolveLegacySetupState(runtime *RuntimeConfig, legacy legacyMigrationConfig, options RuntimeValidationOptions) error {
	if runtime == nil || legacy.SetupState.Valid() || !legacy.IsInit {
		return nil
	}
	readyErr := ValidateRuntimeConfig(*runtime, options)
	if readyErr == nil {
		return nil
	}
	degraded := *runtime
	degraded.SetupState = SetupStateDegraded
	degraded.Root.SetupState = SetupStateDegraded
	prepared, err := prepareLegacyRuntime(degraded)
	if err != nil {
		return err
	}
	if err := ValidateRuntimeConfig(prepared, options); err != nil {
		return readyErr
	}
	*runtime = prepared
	return nil
}

func prepareLegacyRuntime(runtime RuntimeConfig) (RuntimeConfig, error) {
	files, err := marshalRuntimeFiles(runtime)
	if err != nil {
		return RuntimeConfig{}, err
	}
	var root RootConfig
	if err := ParseStrict(files["config.json"], &root); err != nil {
		return RuntimeConfig{}, err
	}
	runtime.Root = root
	return runtime, nil
}

func legacyDatabaseConfig(root string, vault *credential.Vault, legacy legacyMigrationConfig) (DatabaseConfig, []string, error) {
	database, password, manualFields := legacyDatabasePlan(legacy)
	if len(manualFields) > 0 {
		return DatabaseConfig{}, manualFields, nil
	}
	if database.Type == DatabaseTypeSQLite {
		return database, nil, nil
	}
	stored, err := legacyStoredSecret(root, vault, password)
	if err != nil {
		return DatabaseConfig{}, nil, err
	}
	database.Password = stored
	return database, nil, nil
}

func legacyDatabasePlan(legacy legacyMigrationConfig) (DatabaseConfig, string, []string) {
	switch legacy.DbType {
	case "", DBTypeSQLite:
		name, ok := legacySQLiteLocalRef(legacy.DbDSN)
		if !ok {
			return DatabaseConfig{}, "", []string{"dbDSN"}
		}
		return DatabaseConfig{Type: DatabaseTypeSQLite, LocalRef: name}, "", nil
	case DBTypeMySQL:
		database, password, ok := parseLegacyMySQLDSN(legacy.DbDSN)
		if !ok {
			return DatabaseConfig{}, "", []string{"dbDSN"}
		}
		return database, password, nil
	case DBTypePostgres:
		database, password, ok := parseLegacyPostgreSQLDSN(legacy.DbDSN)
		if !ok {
			return DatabaseConfig{}, "", []string{"dbDSN"}
		}
		return database, password, nil
	default:
		return DatabaseConfig{}, "", []string{"dbType"}
	}
}

func legacySQLiteLocalRef(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "smail.db"
	}
	if filepath.IsAbs(value) || isWindowsAbsolute(value) {
		return "", false
	}
	value = filepath.ToSlash(value)
	value = strings.TrimPrefix(value, "./")
	value = strings.TrimPrefix(value, "config/")
	if value == "" || strings.HasPrefix(value, "../") || value == ".." || strings.Contains(value, "/../") || strings.Contains(value, "\\") || strings.Contains(value, ":") {
		return "", false
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || !databaseLocalRefPattern.MatchString(part) {
			return "", false
		}
	}
	return value, true
}

func parseLegacyMySQLDSN(value string) (DatabaseConfig, string, bool) {
	matches := legacyMySQLDSNPattern.FindStringSubmatch(value)
	if matches == nil {
		return DatabaseConfig{}, "", false
	}
	port := int(DefaultMySQLPort)
	if matches[4] != "" {
		parsed, err := strconv.Atoi(matches[4])
		if err != nil || !validPort(parsed) {
			return DatabaseConfig{}, "", false
		}
		port = parsed
	}
	host, ok := normalizeLegacyDatabaseHost(matches[3])
	if !ok || !validLegacyDatabaseUser(matches[1]) || !validLegacyDatabasePassword(matches[2]) {
		return DatabaseConfig{}, "", false
	}
	return DatabaseConfig{Type: DatabaseTypeMySQL, Host: host, Port: uint16(port), Username: matches[1], Name: matches[5]}, matches[2], true
}

func parseLegacyPostgreSQLDSN(value string) (DatabaseConfig, string, bool) {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User == nil || parsed.Host == "" {
		return DatabaseConfig{}, "", false
	}
	username := parsed.User.Username()
	password, ok := parsed.User.Password()
	if !ok || !validLegacyDatabaseUser(username) || !validLegacyDatabasePassword(password) {
		return DatabaseConfig{}, "", false
	}
	host, ok := normalizeLegacyDatabaseHost(parsed.Hostname())
	if !ok {
		return DatabaseConfig{}, "", false
	}
	port := int(DefaultPostgreSQLPort)
	if rawPort := parsed.Port(); rawPort != "" {
		parsedPort, parseErr := strconv.Atoi(rawPort)
		if parseErr != nil || !validPort(parsedPort) {
			return DatabaseConfig{}, "", false
		}
		port = parsedPort
	}
	name := strings.TrimPrefix(parsed.EscapedPath(), "/")
	if name == "" || strings.Contains(name, "/") || !databaseLocalRefPattern.MatchString(name) {
		return DatabaseConfig{}, "", false
	}
	return DatabaseConfig{Type: DatabaseTypePostgreSQL, Host: host, Port: uint16(port), Username: username, Name: name}, password, true
}

func normalizeLegacyDatabaseHost(value string) (string, bool) {
	if value == "" || strings.TrimSpace(value) != value {
		return "", false
	}
	if address := net.ParseIP(value); address != nil {
		return address.String(), true
	}
	normalized, err := normalizeConfigDomain(value)
	if err != nil {
		return "", false
	}
	return normalized, true
}

func validLegacyDatabaseUser(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func validLegacyDatabasePassword(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

func legacySMTPConfig(root string, vault *credential.Vault, legacy legacyMigrationConfig) (SMTPConfig, []string, error) {
	relay := SMTPConfig{
		RelayHost:           legacy.SMTPHost,
		RelayPort:           legacy.SMTPPort,
		Username:            legacy.SMTPUsername,
		Security:            legacy.SMTPSecurity,
		From:                legacy.SMTPFrom,
		FromDomain:          legacy.SMTPFromDomain,
		VerificationEnabled: legacy.VerificationEnabled,
		LoginPolicy:         legacyLoginPolicy(legacy),
	}
	if relay.FromDomain == "" {
		relay.FromDomain = legacy.Domain
	}
	if !relay.VerificationEnabled && relay.RelayHost == "" && relay.RelayPort == 0 && relay.Username == "" && legacy.SMTPPassword == "" {
		return relay, nil, nil
	}
	if relay.RelayPort == 0 {
		relay.RelayPort = 587
	}
	if relay.Security == "" {
		relay.Security = "starttls"
	}
	if relay.Username == "" && legacy.SMTPPassword != "" {
		return SMTPConfig{}, []string{"smtpPassword"}, nil
	}
	if relay.Username != "" {
		stored, err := legacyStoredSecret(root, vault, legacy.SMTPPassword)
		if err != nil {
			return SMTPConfig{}, nil, err
		}
		relay.Password = stored
	}
	return relay, nil, nil
}

func legacySSLConfig(legacy legacyMigrationConfig) (SSLConfig, []string) {
	certificateRef, certificateOK := legacyControlledReference(legacy.SSLPublicKeyPath)
	privateKeyRef, privateKeyOK := legacyControlledReference(legacy.SSLPrivateKeyPath)
	ssl := SSLConfig{Mode: ManagedSSLModeDefault, DefaultDirectory: "config/ssl"}
	if legacy.SSLType == SSLTypeUser {
		ssl.Mode = ManagedSSLModeUpload
		ssl.CustomDirectory = "config/ssl"
	}
	if legacy.SSLPublicKeyPath == "" && legacy.SSLPrivateKeyPath == "" {
		return ssl, nil
	}
	if !certificateOK || !privateKeyOK {
		return ssl, []string{"SSLPublicKeyPath", "SSLPrivateKeyPath"}
	}
	ssl.CertificateRef = certificateRef
	ssl.PrivateKeyRef = privateKeyRef
	return ssl, nil
}

func legacyPluginsConfig(root string, vault *credential.Vault, legacy legacyMigrationConfig) (map[string]PluginInstance, []string, error) {
	enabled, manualFields := legacyPluginEnabledSet(legacy.PluginAllowList)
	spamExecutable, err := catalog.CurrentExecutableName("spam_block")
	if err != nil {
		return nil, nil, err
	}
	wechatExecutable, err := catalog.CurrentExecutableName("wechat_push")
	if err != nil {
		return nil, nil, err
	}
	telegramExecutable, err := catalog.CurrentExecutableName("telegram_push")
	if err != nil {
		return nil, nil, err
	}
	plugins := map[string]PluginInstance{
		"spam_block": {Enabled: enabled["spam_block"], Executable: spamExecutable, Permissions: PluginPermissions{Capabilities: []string{"network"}}},
	}
	wechatEnabled := legacy.WeChatPushAppID != "" || legacy.WeChatPushSecret != "" || legacy.WeChatPushTemplateID != "" || legacy.WeChatPushUserID != ""
	wechat := PluginInstance{
		Enabled:     enabled["wechat_push"],
		Executable:  wechatExecutable,
		Permissions: PluginPermissions{Capabilities: []string{"network"}, Network: []PluginNetworkTarget{{Scheme: "https", Host: "api.weixin.qq.com", Port: 443}}},
	}
	if wechatEnabled {
		settings, err := json.Marshal(map[string]string{"appId": legacy.WeChatPushAppID, "templateId": legacy.WeChatPushTemplateID, "userId": legacy.WeChatPushUserID, "webDomain": legacy.WebDomain})
		if err != nil {
			return nil, nil, err
		}
		wechat.Settings = settings
		if legacy.WeChatPushSecret == "" {
			manualFields = append(manualFields, "weChatPushSecret")
		} else {
			stored, storeErr := legacyStoredSecret(root, vault, legacy.WeChatPushSecret)
			if storeErr != nil {
				return nil, nil, storeErr
			}
			wechat.Secret = stored
		}
	}
	if wechat.Enabled && !wechatEnabled {
		manualFields = append(manualFields, "weChatPushSecret")
	}
	plugins["wechat_push"] = wechat
	telegramEnabled := legacy.TGBotToken != "" || legacy.TGChatID != ""
	telegram := PluginInstance{
		Enabled:     enabled["telegram_push"],
		Executable:  telegramExecutable,
		Permissions: PluginPermissions{Capabilities: []string{"network"}, Network: []PluginNetworkTarget{{Scheme: "https", Host: "api.telegram.org", Port: 443}}},
	}
	if telegramEnabled {
		if legacy.TGBotToken == "" {
			manualFields = append(manualFields, "tgBotToken")
		} else {
			stored, storeErr := legacyStoredSecret(root, vault, legacy.TGBotToken)
			if storeErr != nil {
				return nil, nil, storeErr
			}
			telegram.Secret = stored
		}
		if legacy.TGChatID != "" {
			manualFields = append(manualFields, "tgChatId")
		}
	}
	if telegram.Enabled && !telegramEnabled {
		manualFields = append(manualFields, "tgBotToken")
	}
	plugins["telegram_push"] = telegram
	if legacy.WebPushURL != "" || legacy.WebPushToken != "" {
		manualFields = append(manualFields, "webPushUrl", "webPushToken")
	}
	sort.Strings(manualFields)
	return plugins, compactMigrationFields(manualFields), nil
}

func legacyPluginEnabledSet(values []string) (map[string]bool, []string) {
	enabled := make(map[string]bool, len(legacyPluginExecutableMap))
	manualFields := make([]string, 0)
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" || name != value || name != strings.ToLower(name) || filepath.Base(name) != name || strings.ContainsAny(name, `/\\*?[]`) {
			manualFields = append(manualFields, "pluginAllowList")
			continue
		}
		logicalID, ok := legacyPluginExecutableMap[name]
		if !ok || enabled[logicalID] {
			manualFields = append(manualFields, "pluginAllowList")
			continue
		}
		enabled[logicalID] = true
	}
	sort.Strings(manualFields)
	return enabled, compactMigrationFields(manualFields)
}

func legacyStoredSecret(root string, vault *credential.Vault, value string) (StoredSecret, error) {
	if strings.TrimSpace(value) != value || value == "" {
		return StoredSecret{}, ErrMigrationUnsafeValue
	}
	plaintext := value
	if credential.IsEncrypted(value) {
		decrypted, err := credential.Decrypt(root, value)
		if err != nil {
			return StoredSecret{}, ErrMigrationUnsafeValue
		}
		plaintext = decrypted
	}
	return ApplySecretChange(vault, StoredSecret{}, SecretChange{Operation: SecretReplace, Value: &SecretInput{Plaintext: plaintext}}, SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
}

func legacyLoginPolicy(legacy legacyMigrationConfig) string {
	if legacy.LoginPolicy != "" {
		return legacy.LoginPolicy
	}
	return legacy.VerificationLoginPolicy
}

func legacyControlledReference(value string) (string, bool) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "./")
	value = filepath.ToSlash(value)
	if !isControlledRelativePath(value) {
		return "", false
	}
	return value, true
}

func writeLegacyMigrationBackup(directory string, data []byte, now time.Time) (string, error) {
	backupDirectory := filepath.Join(directory, "migration-backups")
	if err := os.MkdirAll(backupDirectory, 0700); err != nil {
		return "", err
	}
	info, err := os.Lstat(backupDirectory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", ErrManagerStorage
	}
	if err := os.Chmod(backupDirectory, 0700); err != nil {
		return "", err
	}
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	name := "legacy-config-" + now.Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(token) + ".json"
	path := filepath.Join(backupDirectory, name)
	if err := atomicfile.Write(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}

func preserveLegacyMigrationSource(directory string, data []byte) error {
	existing, err := readManagerConfigFile(directory, legacyMigrationSourceCopy)
	if err == nil {
		if bytes.Equal(existing, data) {
			return nil
		}
		return ErrMigrationTargetExists
	}
	if !os.IsNotExist(err) {
		return err
	}
	return atomicfile.Write(filepath.Join(directory, legacyMigrationSourceCopy), data, 0600)
}
