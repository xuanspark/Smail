package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsDeploymentScriptsKeepAdminChannelManual(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	installer, err := os.ReadFile(filepath.Join(root, "deploy", "install-windows.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(installer)
	for _, required := range []string{
		"sc.exe",
		"smail-admin.exe",
		"Set-RestrictedAcl",
		"NT SERVICE\\Smail:(F)",
		".backup-",
		"$mainBackup",
		"$adminBackup",
		"不会自动注册或启动服务",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("Windows installer is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"create SmailAdmin",
		"Start-Service -Name \"SmailAdmin\"",
		"start= auto" + "\n" + "start= auto",
	} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("Windows installer contains forbidden admin-service action %q", forbidden)
		}
	}
}

func TestWindowsUninstallerPreservesData(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	uninstaller, err := os.ReadFile(filepath.Join(root, "deploy", "uninstall-windows.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(uninstaller)
	if strings.Contains(content, "Remove-Item -LiteralPath $resolvedInstallRoot -Recurse") {
		t.Fatal("Windows uninstaller recursively removes the data root")
	}
	for _, required := range []string{
		"smail.exe",
		"smail-admin.exe",
		"配置、数据库、证书和用户数据未删除",
	} {
		if !strings.Contains(content, required) {
			t.Fatalf("Windows uninstaller is missing %q", required)
		}
	}
}
