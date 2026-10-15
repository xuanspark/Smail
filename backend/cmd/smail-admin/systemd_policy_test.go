package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdUnitIsManualStartOnly(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	unit, err := os.ReadFile(filepath.Join(root, "deploy", "systemd", "smail-admin.service"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(unit)
	for _, forbidden := range []string{
		"[Install]",
		"WantedBy=",
		"RequiredBy=",
		"Alias=",
		"Restart=always",
		"Restart=on-failure",
		"ExecReload=",
		"ReadWritePaths=/run/systemd/ask-password",
		"PartOf=",
		"BindsTo=",
		"Wants=",
		"Requires=",
		"Upholds=",
		"PropagatesReloadTo=",
		"ReloadPropagatedFrom=",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("smail-admin unit contains forbidden directive %q", forbidden)
		}
	}
	for _, required := range []string{
		"Type=oneshot",
		"RemainAfterExit=no",
		"Requisite=smail.service",
		"Restart=no",
		"PrivateNetwork=true",
		"RestrictAddressFamilies=AF_UNIX",
		"KillMode=mixed",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("smail-admin unit is missing %q", required)
		}
	}
}

func TestInstallerDisablesAdminAutostart(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	installer, err := os.ReadFile(filepath.Join(root, "deploy", "install-systemd.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(installer)
	for _, forbidden := range []string{
		"systemctl enable smail-admin",
		"systemctl enable --now smail-admin",
		"systemctl start smail-admin",
		"systemctl restart smail-admin",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("installer contains forbidden smail-admin action %q", forbidden)
		}
	}
	if strings.Contains(content, "systemctl preset smail-admin") {
		t.Fatal("installer enables smail-admin")
	}
	if !strings.Contains(content, "systemctl disable smail-admin") {
		t.Fatal("installer does not clear legacy smail-admin enablement")
	}
}

func TestInstallerManagesProtectedEnvironmentFile(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	installer, err := os.ReadFile(filepath.Join(root, "deploy", "install-systemd.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(installer)
	for _, required := range []string{
		"/etc/smail/smail.env",
		"install -d -o root -g root -m 0750 /etc/smail",
		"if [ ! -e /etc/smail/smail.env ]; then",
		"'SMAIL_ROOT=/opt/smail'",
		"chmod 0600 /etc/smail/smail.env",
		"每次部署都重新创建服务环境文件，不继承旧的凭据或代理配置。",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("installer is missing protected environment-file behavior %q", required)
		}
	}
}

func TestInstallerInstallsConfigurationTool(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	installer, err := os.ReadFile(filepath.Join(root, "deploy", "install-systemd.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(installer)
	for _, required := range []string{
		"out/backend/smail-config_linux_$arch",
		"install -o root -g root -m 0700 \"$root_dir/out/backend/smail-config_linux_$arch\" \"$runtime_dir/smail-config\"",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("installer does not install smail-config: missing %q", required)
		}
	}
}

func TestInstallerRequiresExplicitFreshReset(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	installer, err := os.ReadFile(filepath.Join(root, "deploy", "install-systemd.sh"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(installer)
	for _, required := range []string{
		"fresh=true",
		"确认全新部署 Smail",
		"for legacy_backup in /var/backups/smail-deploy-*",
		"rm -rf -- \"$legacy_backup\"",
		"find \"$runtime_dir\" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +",
		"systemctl disable smail smail-admin",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("installer is missing explicit fresh-reset behavior %q", required)
		}
	}
	forbidden := []string{
		"默认模式保留配置、数据库、证书、日志和插件",
		"cp -a -- \"$runtime_dir/.\" \"$backup_dir/runtime/\"",
		"cp -p -- /etc/smail/smail.env \"$backup_dir/smail.env\"",
		"配置、数据库和证书未覆盖",
	}
	for _, value := range forbidden {
		if strings.Contains(content, value) {
			t.Fatalf("installer retains forbidden legacy-data behavior %q", value)
		}
	}
	if strings.Contains(content, "全新部署已停止：$target 非空") {
		t.Fatal("installer must not reject existing data without an explicit fresh-reset request")
	}
}

func TestSmailServiceRetainsPluginIsolationCapabilities(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	unit, err := os.ReadFile(filepath.Join(root, "deploy", "systemd", "smail.service"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(unit)
	for _, required := range []string{
		"EnvironmentFile=/etc/smail/smail.env",
		"NoNewPrivileges=true",
		"AmbientCapabilities=CAP_NET_BIND_SERVICE CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_FOWNER",
		"CapabilityBoundingSet=CAP_NET_BIND_SERVICE CAP_SYS_CHROOT CAP_SETUID CAP_SETGID CAP_CHOWN CAP_SYS_ADMIN CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_FOWNER",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("smail unit is missing %q", required)
		}
	}
	usesEnvironmentFile := false
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == "EnvironmentFile=/etc/smail/smail.env" {
			usesEnvironmentFile = true
			break
		}
	}
	if !usesEnvironmentFile {
		t.Fatal("smail unit must define EnvironmentFile as a standalone directive")
	}
	if strings.Contains(content, "User=smail") || strings.Contains(content, "Group=smail") {
		t.Fatal("smail unit must retain root privileges for plugin isolation")
	}
	if strings.Count(content, "CAP_NET_BIND_SERVICE") != 2 {
		t.Fatal("smail unit does not retain the expected bind capability entries")
	}
}
