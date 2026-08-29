package plugin

import shared "git.pcux.cn/xuans/Smail/plugins/catalog"

var (
	ErrUnknownPluginID     = shared.ErrUnknownPluginID
	ErrInvalidPluginTarget = shared.ErrInvalidPluginTarget
)

type Definition = shared.Definition

func Definitions() []Definition {
	return shared.Definitions()
}

func Lookup(logicalID string) (Definition, bool) {
	return shared.Lookup(logicalID)
}

func ExecutableName(logicalID, goos string) (string, error) {
	return shared.ExecutableName(logicalID, goos)
}

func CurrentExecutableName(logicalID string) (string, error) {
	return shared.CurrentExecutableName(logicalID)
}

func EnabledDefinitions(values []string, goos string) ([]Definition, error) {
	return shared.EnabledDefinitions(values, goos)
}
