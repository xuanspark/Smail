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
		"cp -p -- /etc/smail/smail.env \"$backup_dir/smail.env\"",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("installer is missing protected environment-file behavior %q", required)
		}
	}
}

func TestSmailServiceUsesOnlyBindServiceCapability(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	unit, err := os.ReadFile(filepath.Join(root, "deploy", "systemd", "smail.service"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(unit)
	for _, required := range []string{
		"User=smail",
		"Group=smail",
		"EnvironmentFile=/etc/smail/smail.env",
		"NoNewPrivileges=true",
		"AmbientCapabilities=CAP_NET_BIND_SERVICE",
		"CapabilityBoundingSet=CAP_NET_BIND_SERVICE",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("smail unit is missing %q", required)
		}
	}
	if strings.Contains(content, "User=root") {
		t.Fatal("smail unit must not run as root")
	}
	if strings.Count(content, "CAP_NET_BIND_SERVICE") != 2 {
		t.Fatal("smail unit grants unexpected Linux capabilities")
	}
}
