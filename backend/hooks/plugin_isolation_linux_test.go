//go:build linux

package hooks

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyPluginFileRejectsGrowthPastLimit(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.json")
	targetPath := filepath.Join(directory, "target.json")
	if err := os.WriteFile(sourcePath, []byte(strings.Repeat("a", int(maxPluginConfigBytes)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyPluginFile(sourcePath, targetPath, os.Getuid(), os.Getgid(), 0600); err == nil {
		t.Fatal("oversized plugin configuration was copied")
	}
	if _, err := os.Lstat(targetPath); !os.IsNotExist(err) {
		t.Fatalf("oversized target was not removed: %v", err)
	}
}

func TestPluginCopiesApplySecurityBeforeClosingTargets(t *testing.T) {
	directory := t.TempDir()
	configSource := filepath.Join(directory, "source.json")
	configTarget := filepath.Join(directory, "target.json")
	if err := os.WriteFile(configSource, []byte(`{"enabled":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyPluginFile(configSource, configTarget, os.Getuid(), os.Getgid(), 0600); err != nil {
		t.Fatalf("copy plugin configuration: %v", err)
	}
	configInfo, err := os.Stat(configTarget)
	if err != nil {
		t.Fatal(err)
	}
	if configInfo.Mode().Perm() != 0600 {
		t.Fatalf("configuration mode = %o, want 0600", configInfo.Mode().Perm())
	}

	executableSource := filepath.Join(directory, "source-plugin")
	executableTarget := filepath.Join(directory, "target-plugin")
	if err := os.WriteFile(executableSource, []byte("plugin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := copyPluginExecutable(executableSource, executableTarget, os.Getuid(), os.Getgid()); err != nil {
		t.Fatalf("copy plugin executable: %v", err)
	}
	executableInfo, err := os.Stat(executableTarget)
	if err != nil {
		t.Fatal(err)
	}
	if executableInfo.Mode().Perm() != 0700 {
		t.Fatalf("executable mode = %o, want 0700", executableInfo.Mode().Perm())
	}
}
func TestInheritedAbstractSocketRemainsReachableAcrossListenerCopy(t *testing.T) {
	socketName, err := newPluginSocketName(1)
	if err != nil {
		t.Fatal(err)
	}
	address := "@" + socketName
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: address, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	file, err := listener.File()
	if closeErr := listener.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	inherited, err := net.FileListener(file)
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Close()
	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := inherited.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
		accepted <- acceptErr
	}()
	conn, err := net.Dial("unix", address)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func testPluginEgressSocketDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if len(dir)+len("/egress.socket") >= 90 {
		shortDir, err := os.MkdirTemp("/tmp", "smail-egress-*")
		if err == nil {
			t.Cleanup(func() { _ = os.RemoveAll(shortDir) })
			return shortDir
		}
	}
	return dir
}

func TestPluginEgressSocketLifecycle(t *testing.T) {
	spec := &pluginLaunchSpec{
		runtimeDir: testPluginEgressSocketDir(t),
		procAttr: &os.ProcAttr{Env: []string{
			"SMAIL_PLUGIN_ID=wechat_push",
			"SMAIL_PLUGIN_RPC_TOKEN=" + strings.Repeat("b", 64),
		}},
	}
	if err := startPluginEgress(spec, os.Getuid(), os.Getgid()); err != nil {
		t.Fatal(err)
	}
	defer spec.cleanup()
	socketPath := filepath.Join(spec.runtimeDir, "egress.socket")
	info, err := os.Stat(socketPath)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatal("egress socket is missing or permissions are unsafe")
	}
	token := ""
	for _, value := range spec.procAttr.Env {
		if strings.HasPrefix(value, "SMAIL_PLUGIN_EGRESS_TOKEN=") {
			token = strings.TrimPrefix(value, "SMAIL_PLUGIN_EGRESS_TOKEN=")
		}
	}
	if len(token) != 64 || token == strings.Repeat("b", 64) {
		t.Fatal("egress token is missing or reuses the RPC token")
	}
	spec.egressStop()
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatal("egress socket survived shutdown")
	}
}
