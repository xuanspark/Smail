package config

import "testing"

func TestResolveHTTPPortDefaultsPreservesConfiguredPorts(t *testing.T) {
	t.Setenv("SMAIL_HTTP_PORT_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_TLS_MODE_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_PORT", "19001")
	t.Setenv("SMAIL_HTTPS_PORT", "19002")

	got := ResolveHTTPPortDefaults(HTTPConfig{
		Port:         9001,
		HTTPSPort:    9002,
		HTTPSEnabled: true,
	}, 18080)
	if got.Port != 9001 || got.HTTPSPort != 9002 {
		t.Fatalf("HTTP ports = %d/%d, want 9001/9002", got.Port, got.HTTPSPort)
	}
}

func TestResolveHTTPPortDefaultsUsesEnvironmentPorts(t *testing.T) {
	t.Setenv("SMAIL_HTTP_PORT_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_TLS_MODE_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_PORT", "9001")
	t.Setenv("SMAIL_HTTPS_PORT", "9002")

	got := ResolveHTTPPortDefaults(HTTPConfig{HTTPSEnabled: true}, 18080)
	if got.Port != 9001 || got.HTTPSPort != 9002 {
		t.Fatalf("HTTP ports = %d/%d, want 9001/9002", got.Port, got.HTTPSPort)
	}
}

func TestResolveHTTPPortDefaultsUsesSetupAndDirectDefaults(t *testing.T) {
	t.Setenv("SMAIL_HTTP_PORT_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_TLS_MODE_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_PORT", "")
	t.Setenv("SMAIL_HTTPS_PORT", "")

	got := ResolveHTTPPortDefaults(HTTPConfig{HTTPSEnabled: true}, 9001)
	if got.Port != 9001 || got.HTTPSPort != 443 {
		t.Fatalf("HTTP ports = %d/%d, want 9001/443", got.Port, got.HTTPSPort)
	}
}

func TestResolveHTTPPortDefaultsDisablesInternalHTTPSInProxyMode(t *testing.T) {
	t.Setenv("SMAIL_HTTP_PORT_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_TLS_MODE_OVERRIDE", "")
	t.Setenv("SMAIL_HTTP_PORT", "9001")
	t.Setenv("SMAIL_HTTPS_PORT", "9002")

	got := ResolveHTTPPortDefaults(HTTPConfig{
		HTTPSPort:    9443,
		HTTPSEnabled: true,
		TLSMode:      " PROXY ",
	}, 18080)
	if got.Port != 9001 || got.HTTPSPort != 0 || got.HTTPSEnabled {
		t.Fatalf("proxy HTTP config = %+v, want HTTP 9001 with internal HTTPS disabled", got)
	}
}

func TestResolveHTTPPortDefaultsUsesExplicitRuntimeOverride(t *testing.T) {
	t.Setenv("SMAIL_HTTP_PORT_OVERRIDE", "5201")
	t.Setenv("SMAIL_HTTP_TLS_MODE_OVERRIDE", HTTPSTLSModeProxy)
	t.Setenv("SMAIL_HTTP_PORT", "9001")
	t.Setenv("SMAIL_HTTPS_PORT", "9002")

	got := ResolveHTTPPortDefaults(HTTPConfig{
		Port:         80,
		HTTPSPort:    443,
		HTTPSEnabled: true,
		TLSMode:      HTTPSTLSModeDirect,
	}, 18080)
	if got.Port != 5201 || got.HTTPSPort != 0 || got.HTTPSEnabled || got.TLSMode != HTTPSTLSModeProxy {
		t.Fatalf("runtime override HTTP config = %+v, want proxy API on port 5201", got)
	}
}
