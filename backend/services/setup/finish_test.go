package setup

import (
	"os"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

func TestFinishPersistsServicePorts(t *testing.T) {
	prepareFinishConfigTest(t)
	t.Setenv("SMAIL_HTTP_PORT", "9001")
	t.Setenv("SMAIL_HTTPS_PORT", "9002")
	config.SetRuntimeSetupPort(18080)

	if err := config.WriteConfig(&config.Config{
		SetupState:   config.SetupStateBootstrap,
		HttpsEnabled: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Finish(); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HttpPort != 9001 || loaded.HttpsPort != 9002 {
		t.Fatalf("persisted HTTP ports = %d/%d, want 9001/9002", loaded.HttpPort, loaded.HttpsPort)
	}
	if loaded.SetupState != config.SetupStateReady || !loaded.IsInit {
		t.Fatalf("setup state = %q, isInit = %t", loaded.SetupState, loaded.IsInit)
	}
	if !loaded.VerificationEnabled || !loaded.ExternalRecoveryEnabled || loaded.LoginPolicy != "new_device" {
		t.Fatalf("security defaults = verification %t, recovery %t, policy %q", loaded.VerificationEnabled, loaded.ExternalRecoveryEnabled, loaded.LoginPolicy)
	}
}

func TestFinishPreservesExplicitlyDisabledSecurityOptions(t *testing.T) {
	prepareFinishConfigTest(t)
	if err := config.WriteConfig(&config.Config{
		SetupState:              config.SetupStateReady,
		IsInit:                  true,
		VerificationEnabled:     false,
		ExternalRecoveryEnabled: false,
		LoginPolicy:             "off",
	}); err != nil {
		t.Fatal(err)
	}

	if err := Finish(); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.VerificationEnabled || loaded.ExternalRecoveryEnabled || loaded.LoginPolicy != "off" {
		t.Fatalf("explicit security options changed: verification %t, recovery %t, policy %q", loaded.VerificationEnabled, loaded.ExternalRecoveryEnabled, loaded.LoginPolicy)
	}
}

func TestFinishRepairsReadyConfigWithMissingPorts(t *testing.T) {
	prepareFinishConfigTest(t)
	t.Setenv("SMAIL_HTTP_PORT", "9001")
	t.Setenv("SMAIL_HTTPS_PORT", "9002")

	if err := config.WriteConfig(&config.Config{
		SetupState:   config.SetupStateReady,
		IsInit:       true,
		HttpsEnabled: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Finish(); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HttpPort != 9001 || loaded.HttpsPort != 9002 {
		t.Fatalf("repaired HTTP ports = %d/%d, want 9001/9002", loaded.HttpPort, loaded.HttpsPort)
	}
}

func TestFinishKeepsInternalHTTPSDisabledInProxyMode(t *testing.T) {
	prepareFinishConfigTest(t)
	t.Setenv("SMAIL_HTTP_PORT", "9001")
	t.Setenv("SMAIL_HTTPS_PORT", "9002")

	if err := config.WriteConfig(&config.Config{
		SetupState:   config.SetupStateBootstrap,
		TLSMode:      config.HTTPSTLSModeProxy,
		HttpsPort:    9443,
		HttpsEnabled: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := Finish(); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HttpPort != 9001 || loaded.HttpsPort != 0 || loaded.HttpsEnabled != 2 {
		t.Fatalf("proxy config = HTTP %d, HTTPS %d, enabled %d", loaded.HttpPort, loaded.HttpsPort, loaded.HttpsEnabled)
	}
}

func prepareFinishConfigTest(t *testing.T) {
	t.Helper()
	previousRoot := config.ROOT_PATH
	previousInstance := config.Instance
	previousManager := config.CurrentManager()
	previousSetupPort := config.RuntimeSetupPort()
	previousInitState := config.IsInit

	config.ROOT_PATH = t.TempDir() + string(os.PathSeparator)
	config.Instance = nil
	config.PublishConfigSnapshot(nil)
	config.SetRuntimeManager(nil)
	config.SetRuntimeSetupPort(0)
	config.SetInitState(false)

	t.Cleanup(func() {
		config.ROOT_PATH = previousRoot
		config.Instance = previousInstance
		config.PublishConfigSnapshot(previousInstance)
		config.SetRuntimeManager(previousManager)
		config.SetRuntimeSetupPort(previousSetupPort)
		config.SetInitState(previousInitState)
	})
}
