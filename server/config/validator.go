package config

import (
	"errors"
	"net"
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
)

var logicalIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,63}$`)

func ValidateRootConfig(root RootConfig) error {
	if root.ConfigVersion > CurrentConfigVersion || root.SchemaVersion <= 0 || root.SchemaVersion > CurrentSchemaVersion || root.Generation == 0 || !root.SetupState.Valid() {
		return ErrInvalidSetupState
	}
	if len(root.Sections) > 0 {
		if err := ValidateSectionReferences(root.Sections, root.Generation); err != nil {
			return err
		}
	}
	if err := validateDatabase(root.Database); err != nil {
		return err
	}
	if err := validateSMTP(root.SMTP); err != nil {
		return err
	}
	if err := validateSSL(root.SSL); err != nil {
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

func validateDatabase(database DatabaseConfig) error {
	switch database.Type {
	case DBTypeSQLite:
		if database.SQLiteLocalRef == "" || filepath.IsAbs(database.SQLiteLocalRef) || strings.Contains(database.SQLiteLocalRef, "..") {
			return ErrInvalidDatabase
		}
	case DBTypeMySQL, DBTypePostgres:
		if database.Host == "" || database.Username == "" || database.Name == "" || !database.Password.Configured() || !validPort(database.Port) {
			return ErrInvalidDatabase
		}
		if err := ValidateStoredSecret(database.Password, true); err != nil {
			return ErrInvalidDatabase
		}
		if net.ParseIP(database.Host) == nil && strings.ContainsAny(database.Host, "/\\ ") {
			return ErrInvalidDatabase
		}
	default:
		return ErrInvalidDatabase
	}
	return nil
}

func validateSMTP(smtp SMTPConfig) error {
	if !smtp.VerificationEnabled {
		return nil
	}
	if smtp.RelayHost == "" || !validPort(smtp.RelayPort) || smtp.FromDomain == "" {
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
	case "starttls", "implicit_tls", "plain":
	default:
		return ErrInvalidSMTP
	}
	return nil
}

func validateSSL(ssl SSLConfig) error {
	switch ssl.Mode {
	case "default":
		if ssl.DefaultDirectory == "" {
			return ErrInvalidSSL
		}
	case "custom":
		if ssl.CustomDirectory == "" || filepath.IsAbs(ssl.CustomDirectory) {
			return ErrInvalidSSL
		}
	default:
		return ErrInvalidSSL
	}
	return nil
}

func validatePlugin(plugin PluginConfig) error {
	if !logicalIDPattern.MatchString(plugin.LogicalID) || filepath.Base(plugin.FileName) != plugin.FileName || plugin.FileName == "" {
		return ErrInvalidPlugin
	}
	if err := ValidateStoredSecret(plugin.Secret, false); err != nil {
		return ErrInvalidPlugin
	}
	for _, host := range plugin.NetworkAllowlist {
		if strings.TrimSpace(host) == "" || strings.ContainsAny(host, "/\\") {
			return ErrInvalidPlugin
		}
	}
	return nil
}

func validPort(port int) bool { return port > 0 && port <= 65535 }
