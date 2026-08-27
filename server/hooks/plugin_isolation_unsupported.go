//go:build !linux && !windows

package hooks

import (
	"errors"
	"os"
)

func newPluginLaunchSpec(pluginPath, root, name, socketName string, env []string) (*pluginLaunchSpec, error) {
	return nil, errors.New("production plugin isolation is unavailable on this platform")
}

func applyPluginResourceLimits(process *os.Process) error {
	return nil
}

func securePluginDirectoryOwner(root string) bool {
	return true
}

func securePluginFileOwner(info os.FileInfo) bool {
	return true
}
