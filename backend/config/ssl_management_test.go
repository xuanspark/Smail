package config

import "testing"

func TestEffectiveSSLManagementSupportsThreeBusinessModes(t *testing.T) {
	for _, test := range []struct {
		name string
		ssl  SSLConfig
		want string
	}{
		{name: "web_server", ssl: SSLConfig{Management: SSLManagementWebServer, Mode: ManagedSSLModeUpload}, want: SSLManagementWebServer},
		{name: "project", ssl: SSLConfig{Management: SSLManagementProject, Mode: ManagedSSLModeDefault}, want: SSLManagementProject},
		{name: "custom", ssl: SSLConfig{Management: SSLManagementCustom, Mode: ManagedSSLModeUpload}, want: SSLManagementCustom},
		{name: "legacy_project", ssl: SSLConfig{Mode: "default"}, want: SSLManagementProject},
		{name: "legacy_custom", ssl: SSLConfig{Mode: "custom"}, want: SSLManagementCustom},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := EffectiveSSLManagement(test.ssl); got != test.want {
				t.Fatalf("management = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateSSLManagementMatchesStorageMode(t *testing.T) {
	valid := SSLConfig{
		Management:       SSLManagementProject,
		Mode:             ManagedSSLModeDefault,
		DefaultDirectory: "config/ssl",
		Challenge:        SSLChallengeHTTP,
	}
	if err := validateSSL(valid); err != nil {
		t.Fatalf("project managed SSL was rejected: %v", err)
	}
	valid.Management = SSLManagementWebServer
	valid.Mode = ManagedSSLModeUpload
	valid.CustomDirectory = "config/ssl"
	if err := validateSSL(valid); err != nil {
		t.Fatalf("web server managed SSL was rejected: %v", err)
	}
	valid.Management = SSLManagementCustom
	if err := validateSSL(valid); err != nil {
		t.Fatalf("custom managed SSL was rejected: %v", err)
	}
	// 管理责任切换时继续使用当前证书，待项目签发成功后再切换底层存储引用。
	valid.Management = SSLManagementProject
	if err := validateSSL(valid); err != nil {
		t.Fatalf("project management rejected the existing certificate during transition: %v", err)
	}
	valid.Management = "unsupported"
	if err := validateSSL(valid); err == nil {
		t.Fatal("unsupported SSL management was accepted")
	}
	legacy := SSLConfig{Mode: "default", DefaultDirectory: "config/ssl"}
	if err := validateSSL(legacy); err != nil {
		t.Fatalf("legacy SSL configuration without management was rejected: %v", err)
	}
}
