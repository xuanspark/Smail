package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

const maxPluginConfigBytes int64 = 64 << 10

type pluginLaunchSpec struct {
	path        string
	args        []string
	procAttr    *os.ProcAttr
	socketPath  string
	runtimeDir  string
	parentFiles []*os.File
	egressStop  func()
}

func compatibilityPluginLaunchSpec(pluginPath, root, name, socketName string, env []string) *pluginLaunchSpec {
	return &pluginLaunchSpec{
		path: pluginPath,
		args: []string{name, socketName},
		procAttr: &os.ProcAttr{
			Dir: root,
			Env: env,
			Files: []*os.File{
				os.Stdin,
				os.Stdout,
				os.Stderr,
			},
		},
		socketPath: filepath.Join(root, socketName),
	}
}

func isAbstractPluginSocket(socketPath string) bool {
	return strings.HasPrefix(socketPath, "@")
}

func (s *pluginLaunchSpec) cleanup() {
	if s == nil {
		return
	}
	s.closeParentFiles()
	if s.egressStop != nil {
		s.egressStop()
	}
	if s.runtimeDir != "" {
		_ = os.RemoveAll(s.runtimeDir)
	}
}

func (s *pluginLaunchSpec) closeParentFiles() {
	if s == nil {
		return
	}
	for _, file := range s.parentFiles {
		if file != nil {
			_ = file.Close()
		}
	}
	s.parentFiles = nil
}

func pluginConfigFileName(name string) string {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	return base + "_config.json"
}

func isolatedPluginEnv(env []string) []string {
	result := make([]string, 0, len(env)+2)
	for _, item := range env {
		if len(item) >= len("SMAIL_ROOT=") && item[:len("SMAIL_ROOT=")] == "SMAIL_ROOT=" {
			result = append(result, "SMAIL_ROOT=/")
			continue
		}
		result = append(result, item)
	}
	result = append(result, "SMAIL_PLUGIN_SANDBOX=1")
	return result
}
