package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPluginAllowListRejectsUnsafeNames(t *testing.T) {
	allowed := pluginAllowList([]string{"safe-plugin", "safe-plugin", "../outside", "nested/file", "*", ""})
	if len(allowed) != 1 || !allowed["safe-plugin"] {
		t.Fatalf("unexpected allow list: %#v", allowed)
	}
}

func TestIsPluginExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !isPluginExecutable("safe.exe", 0600) || isPluginExecutable("safe", 0600) {
			t.Fatal("Windows executable validation is incorrect")
		}
		return
	}
	if !isPluginExecutable("safe", os.FileMode(0700)) || isPluginExecutable("safe.txt", os.FileMode(0700)) || isPluginExecutable("safe", os.FileMode(0600)) {
		t.Fatal("Unix executable validation is incorrect")
	}
}

func TestSecurePluginFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		return
	}
	if !securePluginFile(0700) || securePluginFile(0775) || securePluginFile(0702) || securePluginFile(os.ModeSetuid|0700) || securePluginFile(os.ModeSetgid|0700) {
		t.Fatal("plugin file permission validation is incorrect")
	}
}

func TestMinimalPluginEnvUsesAllowList(t *testing.T) {
	t.Setenv("SMAIL_PLUGIN_SECRET", "should-not-be-forwarded")
	t.Setenv("DB_PASSWORD", "should-not-be-forwarded")
	env := minimalPluginEnv("C:/smail")
	values := make(map[string]string, len(env))
	for _, item := range env {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	if values["SMAIL_ROOT"] != "C:/smail" || values["SMAIL_PLUGIN"] != "1" {
		t.Fatalf("plugin identity environment is incorrect: %#v", values)
	}
	for _, name := range []string{"SMAIL_PLUGIN_SECRET", "DB_PASSWORD", "HOME"} {
		if _, ok := values[name]; ok {
			t.Fatalf("sensitive environment variable was forwarded: %s", name)
		}
	}
}

func TestProductionPluginIsolationPolicy(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows production policy test")
	}
	if _, err := newPluginLaunchSpec("plugin.exe", ".", "plugin.exe", "1.socket", nil); err == nil {
		t.Fatal("Windows production plugin isolation must fail closed")
	}
}

func TestPersistPluginConfigReplacesFileAtomically(t *testing.T) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(workingDirectory)
		ResetHooks()
	})
	if err = os.Mkdir("plugins", 0700); err != nil {
		t.Fatal(err)
	}
	ResetHooks()
	if !RegisterHook("spam_block", &HookSender{name: "spam_block.exe"}) {
		t.Fatal("plugin hook registration failed")
	}
	for _, payload := range []string{`{"url":"https://model.example.com"}`, `{"url":"https://model.example.com/v2"}`} {
		if err = PersistPluginConfig("spam_block", payload); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "plugins", "spam_block_config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"url":"https://model.example.com/v2"}` {
		t.Fatalf("plugin configuration content = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("plugin configuration permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestRegisterHookRejectsUnsafeNames(t *testing.T) {
	ResetHooks()
	t.Cleanup(ResetHooks)
	for _, name := range []string{"../unsafe", "nested/name", `nested\name`, "name with space", strings.Repeat("a", 65)} {
		if RegisterHook(name, &HookSender{}) {
			t.Fatalf("unsafe hook name was accepted: %q", name)
		}
	}
	if !RegisterHook("safe-plugin_1", &HookSender{}) {
		t.Fatal("safe hook name was rejected")
	}
}
