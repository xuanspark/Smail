package controllers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

func TestPublicConfigSectionsDoNotExposeSecretsAndRemainStrictlyDecodable(t *testing.T) {
	runtime := config.RuntimeConfig{
		Generation: 7,
		SetupState: config.SetupStateReady,
		Root: config.RootConfig{
			ConfigVersion: config.CurrentConfigVersion,
			SchemaVersion: config.CurrentSchemaVersion,
			Generation:    7,
			SetupState:    config.SetupStateReady,
			Database: config.DatabaseConfig{
				Type:     config.DatabaseTypeMySQL,
				Host:     "127.0.0.1",
				Port:     config.DefaultMySQLPort,
				Username: "smail",
				Name:     "smail",
				Password: config.StoredSecret{Ciphertext: "enc:v1:database-secret", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()},
			},
		},
		SMTP: config.SMTPSectionConfig{
			Meta: config.SectionMeta{Schema: config.CurrentSectionSchema, Generation: 7},
			Relay: config.SMTPConfig{
				RelayHost: "smtp.example.com",
				RelayPort: 587,
				Username:  "relay-user",
				Password:  config.StoredSecret{Ciphertext: "enc:v1:relay-secret", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()},
				Security:  "starttls",
			},
		},
		Email: config.EmailSectionConfig{Meta: config.SectionMeta{Schema: config.CurrentSectionSchema, Generation: 7}},
		SSL: config.SSLSectionConfig{
			Meta: config.SectionMeta{Schema: config.CurrentSectionSchema, Generation: 7},
			TLS: config.SSLConfig{
				DNSProvider:  config.DNSProviderAliDNS,
				DNSAccessKey: config.StoredSecret{Ciphertext: "enc:v1:dns-access", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()},
				DNSSecretKey: config.StoredSecret{Ciphertext: "enc:v1:dns-secret", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()},
			},
		},
		Plugins: config.PluginsSectionConfig{
			Meta: config.SectionMeta{Schema: config.CurrentSectionSchema, Generation: 7},
			Plugins: map[string]config.PluginInstance{
				"sample": {
					Enabled: true, Executable: "sample.exe", Permissions: config.PluginPermissions{},
					Secret: config.StoredSecret{Ciphertext: "enc:v1:plugin-secret", Version: credential.CurrentVersion, UpdatedAt: time.Now().UTC()},
				},
			},
		},
	}

	tests := []struct {
		section config.Section
		decode  func([]byte) error
	}{
		{config.SectionRoot, func(data []byte) error {
			var value config.RootConfig
			return config.ParseStrict(data, &value)
		}},
		{config.SectionSMTP, func(data []byte) error {
			var value config.SMTPSectionConfig
			return config.ParseStrict(data, &value)
		}},
		{config.SectionMail, func(data []byte) error {
			var value config.MailSectionConfig
			return config.ParseStrict(data, &value)
		}},
		{config.SectionEmail, func(data []byte) error {
			var value config.EmailSectionConfig
			return config.ParseStrict(data, &value)
		}},
		{config.SectionSSL, func(data []byte) error {
			var value config.SSLSectionConfig
			return config.ParseStrict(data, &value)
		}},
		{config.SectionPlugins, func(data []byte) error {
			var value config.PluginsSectionConfig
			return config.ParseStrict(data, &value)
		}},
	}

	for _, test := range tests {
		t.Run(string(test.section), func(t *testing.T) {
			public, err := publicConfigSection(runtime, test.section)
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(public)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "database-secret") || strings.Contains(string(data), "relay-secret") {
				t.Fatalf("public configuration contains secret data: %s", data)
			}
			if strings.Contains(string(data), "enc:v1:") {
				t.Fatalf("public configuration contains encrypted secret material: %s", data)
			}
			writableData := make(map[string]json.RawMessage)
			if err := json.Unmarshal(data, &writableData); err != nil {
				t.Fatalf("public configuration is not a JSON object: %v", err)
			}
			delete(writableData, "databasePasswordStatus")
			delete(writableData, "relayPasswordStatus")
			if test.section == config.SectionSSL {
				for _, name := range []string{"dnsAccessKeyStatus", "dnsSecretKeyStatus"} {
					if !strings.Contains(string(writableData[name]), `"configured":true`) {
						t.Fatalf("DNS secret status missing: %s", name)
					}
					delete(writableData, name)
				}
			}
			if test.section == config.SectionMail {
				var relay map[string]json.RawMessage
				if err := json.Unmarshal(writableData["relay"], &relay); err != nil {
					t.Fatalf("mail relay public configuration is not an object: %v", err)
				}
				if password, ok := relay["password"]; !ok || string(password) != "null" {
					t.Fatalf("mail relay password placeholder is invalid: %s", password)
				}
			}
			if test.section == config.SectionPlugins {
				var plugins map[string]json.RawMessage
				if err := json.Unmarshal(writableData["plugins"], &plugins); err != nil {
					t.Fatalf("plugin public configuration is not an object: %v", err)
				}
				for id, raw := range plugins {
					var plugin map[string]json.RawMessage
					if err := json.Unmarshal(raw, &plugin); err != nil {
						t.Fatalf("plugin %q public configuration is not an object: %v", id, err)
					}
					status, ok := plugin["secretStatus"]
					if !ok || !strings.Contains(string(status), `"configured":true`) || !strings.Contains(string(status), `"hint":"******"`) {
						t.Fatalf("plugin %q secret status is incomplete: %s", id, status)
					}
					if strings.Contains(string(status), "plugin-secret") || strings.Contains(string(status), "enc:v1:") {
						t.Fatalf("plugin %q secret status exposed secret material: %s", id, status)
					}
					delete(plugin, "secretStatus")
					plugins[id], err = json.Marshal(plugin)
					if err != nil {
						t.Fatal(err)
					}
				}
				writableData["plugins"], err = json.Marshal(plugins)
				if err != nil {
					t.Fatal(err)
				}
			}
			data, err = json.Marshal(writableData)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.decode(data); err != nil {
				t.Fatalf("public configuration is not strictly decodable: %v; data=%s", err, data)
			}
		})
	}
}
