package config

import (
	"errors"
	"net"
	"net/mail"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrInvalidSetupState = errors.New("invalid setup state")
	ErrInvalidDatabase   = errors.New("invalid database configuration")
	ErrInvalidSMTP       = errors.New("invalid SMTP configuration")
	ErrInvalidSSL        = errors.New("invalid SSL configuration")
	ErrInvalidPlugin     = errors.New("invalid plugin configuration")
	ErrInvalidHTTP       = errors.New("invalid HTTP configuration")
	ErrInvalidEmail      = errors.New("invalid email configuration")
	ErrInvalidLogLevel   = errors.New("invalid log level")
)

var logicalIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)
var databaseLocalRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidateRootConfig(root RootConfig) error {
	if root.ConfigVersion > CurrentConfigVersion || root.SchemaVersion <= 0 || root.SchemaVersion > CurrentSchemaVersion || root.Generation == 0 || !root.SetupState.Valid() {
		return ErrInvalidSetupState
	}
	if !validLogLevel(root.LogLevel) {
		return ErrInvalidLogLevel
	}
	if len(root.Sections) > 0 {
		if err := ValidateSectionReferences(root.Sections, root.Generation); err != nil {
			return err
		}
	}
	if err := ValidateDatabaseConfig(root.Database); err != nil {
		return err
	}
	if err := validateSMTP(root.SMTP); err != nil {
		return err
	}
	if err := validateSSL(root.SSL); err != nil {
		return err
	}
	if err := validateHTTP(root.HTTP); err != nil {
		return err
	}
	if err := validateEmail(root.Email); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(root.Plugins))
	for _, plugin := range root.Plugins {
		if err := validatePlugin(plugin); err != nil {
			return err
		}
		if _, ok := seen[plugin.LogicalID]; ok {
			return ErrInvalidPlugin
		}
		seen[plugin.LogicalID] = struct{}{}
	}
	if root.Security.PasswordVersion != "argon2id" {
		return errors.New("only Argon2id password service is allowed")
	}
	return nil
}

func ValidateDatabaseConfig(database DatabaseConfig) error {
	return validateDatabase(database)
}

func validateDatabase(database DatabaseConfig) error {
	database = NormalizeDatabaseConfig(database)
	switch database.Type {
	case DatabaseTypeSQLite:
		if !databaseLocalRefPattern.MatchString(database.LocalRef) ||
			filepath.IsAbs(database.LocalRef) ||
			isWindowsAbsolute(database.LocalRef) ||
			strings.ContainsAny(database.LocalRef, `/\\:`) ||
			database.Host != "" || database.Port != 0 || database.Username != "" ||
			database.Password.Configured() || database.Password.Ciphertext != "" || database.Password.Version != 0 || !database.Password.UpdatedAt.IsZero() || database.Name != "" {
			return ErrInvalidDatabase
		}
	case DatabaseTypeMySQL, DatabaseTypePostgreSQL:
		if database.Host == "" || database.Username == "" || database.Name == "" || !database.Password.Configured() || database.LocalRef != "" || !validPort(int(database.Port)) {
			return ErrInvalidDatabase
		}
		if err := ValidateStoredSecret(database.Password, true); err != nil {
			return ErrInvalidDatabase
		}
		if net.ParseIP(database.Host) == nil && strings.ContainsAny(database.Host, `/\\ `) {
			return ErrInvalidDatabase
		}
	default:
		return ErrInvalidDatabase
	}
	return nil
}

func validateSMTP(smtp SMTPConfig) error {
	if !validLoginVerificationPolicy(smtp.LoginPolicy) {
		return ErrInvalidSMTP
	}
	if smtp.From != "" {
		parsedFrom, err := mail.ParseAddress(smtp.From)
		if err != nil || parsedFrom.Address != smtp.From || strings.ContainsAny(smtp.From, "\r\n") {
			return ErrInvalidSMTP
		}
	}
	if !smtp.VerificationEnabled {
		if smtp.RelayHost != "" && !validSMTPHost(smtp.RelayHost) {
			return ErrInvalidSMTP
		}
		if smtp.RelayPort != 0 && !validPort(smtp.RelayPort) {
			return ErrInvalidSMTP
		}
		if smtp.FromDomain != "" && !validCanonicalDomain(smtp.FromDomain) {
			return ErrInvalidSMTP
		}
		if strings.TrimSpace(smtp.Username) != smtp.Username {
			return ErrInvalidSMTP
		}
		if err := ValidateStoredSecret(smtp.Password, false); err != nil {
			return ErrInvalidSMTP
		}
		if smtp.Username != "" {
			if err := ValidateStoredSecret(smtp.Password, true); err != nil {
				return ErrInvalidSMTP
			}
		}
		if smtp.Security != "" {
			switch smtp.Security {
			case "starttls", "implicit_tls", "tls":
			case "plain":
				if !isLocalSMTPHost(smtp.RelayHost) {
					return ErrInvalidSMTP
				}
			default:
				return ErrInvalidSMTP
			}
		}
		return nil
	}
	if !validSMTPHost(smtp.RelayHost) || !validPort(smtp.RelayPort) || !validCanonicalDomain(smtp.FromDomain) || smtp.From == "" {
		return ErrInvalidSMTP
	}
	if strings.TrimSpace(smtp.Username) != smtp.Username {
		return ErrInvalidSMTP
	}
	if smtp.Username == "" {
		if err := ValidateStoredSecret(smtp.Password, false); err != nil || smtp.Password.Configured() {
			return ErrInvalidSMTP
		}
	} else if err := ValidateStoredSecret(smtp.Password, true); err != nil {
		return ErrInvalidSMTP
	}
	switch smtp.Security {
	case "starttls", "implicit_tls", "tls":
	case "plain":
		if !isLocalSMTPHost(smtp.RelayHost) {
			return ErrInvalidSMTP
		}
	default:
		return ErrInvalidSMTP
	}
	return nil
}

func validateSSL(ssl SSLConfig) error {
	if !isControlledRelativePath(ssl.DefaultDirectory) {
		return ErrInvalidSSL
	}
	activeDirectory := ssl.DefaultDirectory
	switch ssl.Mode {
	case ManagedSSLModeDefault, "default":
		if ssl.DefaultDirectory == "" || ssl.CustomDirectory != "" {
			return ErrInvalidSSL
		}
	case ManagedSSLModeUpload, "custom":
		if !isControlledRelativePath(ssl.CustomDirectory) || ssl.CustomDirectory == "" {
			return ErrInvalidSSL
		}
		activeDirectory = ssl.CustomDirectory
	default:
		return ErrInvalidSSL
	}
	if (ssl.CertificateRef != "" && !isControlledRelativePath(ssl.CertificateRef)) ||
		(ssl.PrivateKeyRef != "" && !isControlledRelativePath(ssl.PrivateKeyRef)) {
		return ErrInvalidSSL
	}
	if (ssl.CertificateRef == "") != (ssl.PrivateKeyRef == "") {
		return ErrInvalidSSL
	}
	if ssl.CertificateRef != "" && (!isPathWithinDirectory(ssl.CertificateRef, activeDirectory) || !isPathWithinDirectory(ssl.PrivateKeyRef, activeDirectory)) {
		return ErrInvalidSSL
	}
	return nil
}

func validateHTTP(http HTTPConfig) error {
	if http.Port != 0 && !validPort(http.Port) {
		return ErrInvalidHTTP
	}
	if http.HTTPSPort != 0 && !validPort(http.HTTPSPort) {
		return ErrInvalidHTTP
	}
	if http.TLSMode != "" && http.TLSMode != HTTPSTLSModeDirect && http.TLSMode != HTTPSTLSModeProxy {
		return ErrInvalidHTTP
	}
	if http.TLSMode == HTTPSTLSModeProxy && (http.HTTPSEnabled || http.HTTPSPort != 0) {
		return ErrInvalidHTTP
	}
	for _, cidr := range http.TrustedProxyCIDRs {
		if strings.TrimSpace(cidr) != cidr {
			return ErrInvalidHTTP
		}
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return ErrInvalidHTTP
		}
	}
	if http.HTTPSEnabled && http.HTTPSPort == 0 {
		return ErrInvalidHTTP
	}
	return nil
}

func validateEmail(email EmailConfig) error {
	for _, port := range []int{email.SMTPPort, email.IMAPPort, email.POP3Port} {
		if port != 0 && !validPort(port) {
			return ErrInvalidEmail
		}
	}
	if email.MaxMessageBytes < 0 || email.SpamFilterLevel < 0 || email.SpamFilterLevel > 3 || !validLoginVerificationPolicy(email.LoginVerificationPolicy) {
		return ErrInvalidEmail
	}
	return nil
}

func isControlledRelativePath(value string) bool {
	if value == "" || path.IsAbs(value) || filepath.IsAbs(value) || isWindowsAbsolute(value) || strings.ContainsAny(value, `\\:`) {
		return false
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	return true
}

func isPathWithinDirectory(value, directory string) bool {
	if !isControlledRelativePath(value) || !isControlledRelativePath(directory) {
		return false
	}
	return value != directory && strings.HasPrefix(value, directory+"/")
}

func validatePlugin(plugin PluginConfig) error {
	if !logicalIDPattern.MatchString(plugin.LogicalID) || filepath.Base(plugin.FileName) != plugin.FileName || plugin.FileName == "" {
		return ErrInvalidPlugin
	}
	if err := ValidateStoredSecret(plugin.Secret, false); err != nil {
		return ErrInvalidPlugin
	}
	seenHosts := make(map[string]struct{}, len(plugin.NetworkAllowlist))
	for _, host := range plugin.NetworkAllowlist {
		normalized, ok := normalizeSMTPHost(host)
		if !ok || net.ParseIP(normalized) != nil {
			return ErrInvalidPlugin
		}
		if _, exists := seenHosts[normalized]; exists {
			return ErrInvalidPlugin
		}
		seenHosts[normalized] = struct{}{}
	}
	return nil
}

func validLogLevel(value string) bool {
	switch value {
	case "", "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

func validLoginVerificationPolicy(value string) bool {
	switch value {
	case "", "off", "always", "new_device":
		return true
	default:
		return false
	}
}

func validCanonicalDomain(value string) bool {
	normalized, err := normalizeConfigDomain(value)
	return err == nil && normalized == value
}

func validSMTPHost(value string) bool {
	_, ok := normalizeSMTPHost(value)
	return ok
}

func normalizeSMTPHost(value string) (string, bool) {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "@/\\\r\n\t ") {
		return "", false
	}
	if parsed := net.ParseIP(value); parsed != nil {
		return parsed.String(), true
	}
	normalized, err := normalizeConfigDomain(value)
	if err != nil || normalized != value {
		return "", false
	}
	return normalized, true
}

func isLocalSMTPHost(value string) bool {
	normalized, ok := normalizeSMTPHost(value)
	if !ok {
		return false
	}
	if normalized == "localhost" {
		return true
	}
	parsed := net.ParseIP(normalized)
	return parsed != nil && parsed.IsLoopback()
}

func validPort(port int) bool { return port > 0 && port <= 65535 }
