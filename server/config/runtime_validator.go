package config

import (
	"encoding/json"
	"errors"
	"git.pcux.cn/xuans/Smail/plugins/catalog"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrInvalidRuntimeConfig = errors.New("invalid runtime configuration")
	ErrPortConflict         = errors.New("configuration port conflict")
	ErrInvalidDomainConfig  = errors.New("invalid domain configuration")
	ErrPluginCapability     = errors.New("invalid plugin capability configuration")
)

const maxPluginSettingsBytes = 64 << 10

var capabilityPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

type RuntimeValidationOptions struct {
	HasSuperAdmin bool
	Production    bool
}

func ValidateRuntimeConfig(runtime RuntimeConfig, options RuntimeValidationOptions) error {
	if runtime.Generation == 0 || runtime.Root.Generation != runtime.Generation || runtime.SetupState != runtime.Root.SetupState {
		return ErrInvalidRuntimeConfig
	}
	if err := ValidateSectionReferences(runtime.Root.Sections, runtime.Generation); err != nil {
		return err
	}
	for section, meta := range map[Section]SectionMeta{
		SectionSMTP:    runtime.SMTP.Meta,
		SectionEmail:   runtime.Email.Meta,
		SectionSSL:     runtime.SSL.Meta,
		SectionPlugins: runtime.Plugins.Meta,
	} {
		if err := ValidateSectionMeta(section, meta, runtime.Generation); err != nil {
			return err
		}
	}
	candidate := cloneRootConfig(runtime.Root)
	candidate.SMTP = runtime.SMTP.Relay
	candidate.Email = runtime.Email.Policy
	candidate.SSL = runtime.SSL.TLS
	candidate.Plugins = pluginList(runtime.Plugins)
	if err := ValidateRootConfig(candidate); err != nil {
		return err
	}
	if err := validateDomains(runtime.Root.Domains); err != nil {
		return err
	}
	if err := validateRuntimePorts(runtime.Root.HTTP, runtime.Email.Policy); err != nil {
		return err
	}
	if err := validatePluginSection(runtime.Plugins); err != nil {
		return err
	}
	if runtime.SetupState == SetupStateReady {
		if !options.HasSuperAdmin || runtime.Root.Domains.Primary == "" || len(runtime.Root.Domains.Accepted) == 0 || runtime.Root.Domains.Web == "" || !runtime.Root.Database.IsStructured() {
			return ErrInvalidRuntimeConfig
		}
		if runtime.Email.Policy.SMTPPort == 0 || runtime.Email.Policy.IMAPPort == 0 || runtime.Email.Policy.POP3Port == 0 {
			return ErrInvalidRuntimeConfig
		}
		if runtime.Root.HTTP.HTTPSEnabled && runtime.Root.HTTP.HTTPSPort == 0 {
			return ErrInvalidRuntimeConfig
		}
	}
	if options.Production && runtime.SetupState == SetupStateDegraded && !runtime.Root.HTTP.HTTPSEnabled {
		return ErrInvalidRuntimeConfig
	}
	return nil
}

func validateDomains(domains DomainConfig) error {
	if domains.Primary == "" && domains.Web == "" && len(domains.Accepted) == 0 && len(domains.AccessHosts) == 0 {
		return nil
	}
	primary, err := normalizeConfigDomain(domains.Primary)
	if err != nil || primary != strings.ToLower(strings.TrimSuffix(domains.Primary, ".")) {
		return ErrInvalidDomainConfig
	}
	web, err := normalizeConfigDomain(domains.Web)
	if err != nil || web != strings.ToLower(strings.TrimSuffix(domains.Web, ".")) {
		return ErrInvalidDomainConfig
	}
	seen := make(map[string]struct{}, len(domains.Accepted))
	for _, item := range domains.Accepted {
		domain, normalizeErr := normalizeConfigDomain(item)
		if normalizeErr != nil || domain != strings.ToLower(strings.TrimSuffix(item, ".")) {
			return ErrInvalidDomainConfig
		}
		if _, exists := seen[domain]; exists {
			return ErrInvalidDomainConfig
		}
		seen[domain] = struct{}{}
	}
	if _, ok := seen[primary]; !ok {
		return ErrInvalidDomainConfig
	}
	accessHosts := make(map[string]struct{}, len(domains.AccessHosts))
	for _, item := range domains.AccessHosts {
		host, normalizeErr := normalizeConfigAccessHost(item)
		if normalizeErr != nil || host != strings.ToLower(strings.TrimSuffix(strings.TrimSpace(item), ".")) {
			return ErrInvalidDomainConfig
		}
		if _, exists := accessHosts[host]; exists {
			return ErrInvalidDomainConfig
		}
		accessHosts[host] = struct{}{}
	}
	return nil
}

func normalizeConfigAccessHost(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if parsed := net.ParseIP(strings.Trim(value, "[]")); parsed != nil {
		return parsed.String(), nil
	}
	return normalizeConfigDomain(value)
}

func validateRuntimePorts(http HTTPConfig, email EmailConfig) error {
	ports := []int{http.Port, email.SMTPPort, email.IMAPPort, email.POP3Port}
	if http.HTTPSEnabled || http.HTTPSPort != 0 {
		ports = append(ports, http.HTTPSPort)
	}
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if port == 0 {
			continue
		}
		if !validPort(port) {
			return ErrPortConflict
		}
		if _, exists := seen[port]; exists {
			return ErrPortConflict
		}
		seen[port] = struct{}{}
	}
	return nil
}

func validatePluginSection(section PluginsSectionConfig) error {
	for logicalID, plugin := range section.Plugins {
		if !logicalIDPattern.MatchString(logicalID) || filepath.Base(plugin.Executable) != plugin.Executable || !catalog.IsCurrentExecutable(logicalID, plugin.Executable) {
			return ErrInvalidPlugin
		}
		definition, ok := catalog.Lookup(logicalID)
		if !ok {
			return ErrInvalidPlugin
		}
		capabilities := make(map[string]struct{}, len(plugin.Permissions.Capabilities))
		for _, capability := range plugin.Permissions.Capabilities {
			if !capabilityPattern.MatchString(capability) {
				return ErrPluginCapability
			}
			if _, exists := capabilities[capability]; exists {
				return ErrPluginCapability
			}
			capabilities[capability] = struct{}{}
		}
		if !matchesCatalogCapabilities(capabilities, definition.Capabilities) {
			return ErrPluginCapability
		}
		if len(plugin.Permissions.Network) > 0 {
			if _, ok := capabilities["network"]; !ok {
				return ErrPluginCapability
			}
		}
		targets := make(map[string]struct{}, len(plugin.Permissions.Network))
		for _, target := range plugin.Permissions.Network {
			if target.Scheme != "https" && target.Scheme != "wss" {
				return ErrPluginCapability
			}
			host, err := normalizeConfigDomain(target.Host)
			if err != nil || host != strings.ToLower(strings.TrimSuffix(target.Host, ".")) || net.ParseIP(host) != nil || !validPort(target.Port) {
				return ErrPluginCapability
			}
			key := target.Scheme + "://" + host + ":" + strconv.Itoa(target.Port)
			if _, exists := targets[key]; exists {
				return ErrPluginCapability
			}
			targets[key] = struct{}{}
		}
		if len(plugin.Settings) > maxPluginSettingsBytes || !validPluginSettings(plugin.Settings) {
			return ErrInvalidPlugin
		}
		if err := ValidateStoredSecret(plugin.Secret, false); err != nil {
			return ErrInvalidPlugin
		}
	}
	return nil
}

func matchesCatalogCapabilities(actual map[string]struct{}, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for _, capability := range expected {
		if _, ok := actual[capability]; !ok {
			return false
		}
	}
	return true
}

func validPluginSettings(data json.RawMessage) bool {
	if len(data) == 0 {
		return true
	}
	var value any
	if err := ParseStrict(data, &value); err != nil {
		return false
	}
	return !containsSensitiveSetting(value)
}

func containsSensitiveSetting(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
			for _, blocked := range []string{"password", "secret", "token", "apikey", "credential", "privatekey"} {
				if strings.Contains(normalized, blocked) {
					return true
				}
			}
			if containsSensitiveSetting(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsSensitiveSetting(item) {
				return true
			}
		}
	}
	return false
}

func pluginList(section PluginsSectionConfig) []PluginConfig {
	result := make([]PluginConfig, 0, len(section.Plugins))
	for logicalID, plugin := range section.Plugins {
		targets := make([]string, 0, len(plugin.Permissions.Network))
		for _, target := range plugin.Permissions.Network {
			targets = append(targets, target.Host)
		}
		result = append(result, PluginConfig{
			LogicalID:        logicalID,
			FileName:         plugin.Executable,
			Enabled:          plugin.Enabled,
			NetworkAllowlist: targets,
			Secret:           plugin.Secret,
		})
	}
	return result
}

func normalizeConfigDomain(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	if value == "" || len(value) > 253 || net.ParseIP(value) != nil || strings.ContainsAny(value, ":/@\\?#") {
		return "", ErrInvalidDomainConfig
	}
	labels := strings.Split(value, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidDomainConfig
		}
		for _, char := range []byte(label) {
			isLetter := char >= 'a' && char <= 'z'
			isDigit := char >= '0' && char <= '9'
			if !isLetter && !isDigit && char != '-' {
				return "", ErrInvalidDomainConfig
			}
		}
	}
	return value, nil
}
