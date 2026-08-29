package catalog

import (
	"errors"
	"runtime"
	"sort"
	"strings"
)

var (
	ErrUnknownPluginID     = errors.New("unknown plugin ID")
	ErrInvalidPluginTarget = errors.New("invalid plugin target")
)

type Definition struct {
	LogicalID             string
	WindowsExecutable     string
	UnixExecutable        string
	Capabilities          []string
	HandshakeCapabilities []string
	SettingsFields        []string
}

var definitions = []Definition{
	{
		LogicalID:             "spam_block",
		WindowsExecutable:     "spam_block.exe",
		UnixExecutable:        "spam_block",
		Capabilities:          []string{"network"},
		HandshakeCapabilities: []string{"network"},
		SettingsFields:        []string{"url", "timeout", "threshold"},
	},
	{
		LogicalID:             "wechat_push",
		WindowsExecutable:     "wechat_push.exe",
		UnixExecutable:        "wechat_push",
		Capabilities:          []string{"network"},
		HandshakeCapabilities: []string{"network"},
		SettingsFields:        []string{"appId", "templateId", "userId", "webDomain"},
	},
	{
		LogicalID:             "telegram_push",
		WindowsExecutable:     "pmail_telegram_push.exe",
		UnixExecutable:        "pmail_telegram_push",
		Capabilities:          []string{"network"},
		HandshakeCapabilities: []string{"network", "userSettings", "settingsHtml"},
		SettingsFields:        []string{"proxy", "timeout", "debug", "webDomain", "httpsEnabled"},
	},
}

func Definitions() []Definition {
	result := make([]Definition, len(definitions))
	for index, definition := range definitions {
		result[index] = cloneDefinition(definition)
	}
	return result
}

func Lookup(logicalID string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.LogicalID == logicalID {
			return cloneDefinition(definition), true
		}
	}
	return Definition{}, false
}

func ExecutableName(logicalID, goos string) (string, error) {
	definition, ok := Lookup(logicalID)
	if !ok {
		return "", ErrUnknownPluginID
	}
	if goos == "windows" {
		return definition.WindowsExecutable, nil
	}
	if goos == "linux" || goos == "darwin" {
		return definition.UnixExecutable, nil
	}
	return "", ErrInvalidPluginTarget
}

func CurrentExecutableName(logicalID string) (string, error) {
	return ExecutableName(logicalID, runtime.GOOS)
}

func EnabledDefinitions(values []string, goos string) ([]Definition, error) {
	enabled := make(map[string]struct{}, len(values))
	for _, value := range values {
		logicalID, err := LogicalIDFromValue(value, goos)
		if err != nil {
			return nil, err
		}
		if _, exists := enabled[logicalID]; exists {
			return nil, ErrInvalidPluginTarget
		}
		enabled[logicalID] = struct{}{}
	}
	result := make([]Definition, 0, len(enabled))
	for _, definition := range definitions {
		if _, ok := enabled[definition.LogicalID]; ok {
			result = append(result, cloneDefinition(definition))
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].LogicalID < result[right].LogicalID
	})
	return result, nil
}

func LogicalIDFromValue(value, goos string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value != strings.ToLower(value) || strings.ContainsAny(value, `/\\*?[]`) {
		return "", ErrInvalidPluginTarget
	}
	if _, ok := Lookup(value); ok {
		return value, nil
	}
	for _, definition := range definitions {
		executable, err := ExecutableName(definition.LogicalID, goos)
		if err != nil {
			return "", err
		}
		if value == executable {
			return definition.LogicalID, nil
		}
	}
	return "", ErrUnknownPluginID
}

func IsCurrentExecutable(logicalID, executable string) bool {
	expected, err := CurrentExecutableName(logicalID)
	return err == nil && executable == expected
}

func cloneDefinition(source Definition) Definition {
	result := source
	result.Capabilities = append([]string(nil), source.Capabilities...)
	result.HandshakeCapabilities = append([]string(nil), source.HandshakeCapabilities...)
	result.SettingsFields = append([]string(nil), source.SettingsFields...)
	return result
}
