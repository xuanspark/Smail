package ssl

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
)

type renewalBlockedTransport struct {
	calls int
}

func (transport *renewalBlockedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return nil, errors.New("测试禁止外部网络请求")
}

func TestExternalManagementBlocksAutomaticIssuance(t *testing.T) {
	previousRoot, previousInstance := config.ROOT_PATH, config.Instance
	previousSnapshot, previousManager := config.CurrentConfig(), config.CurrentManager()
	previousTransport := http.DefaultTransport
	config.SetRuntimeManager(nil)
	t.Cleanup(func() {
		config.ROOT_PATH, config.Instance = previousRoot, previousInstance
		config.PublishConfigSnapshot(previousSnapshot)
		config.SetRuntimeManager(previousManager)
		http.DefaultTransport = previousTransport
	})
	for _, management := range []string{config.SSLManagementCustom, config.SSLManagementWebServer} {
		for _, sslType := range []string{config.SSLTypeAutoHTTP, config.SSLTypeAutoDNS} {
			t.Run(management+sslType, func(t *testing.T) {
				config.ROOT_PATH = t.TempDir()
				cfg := &config.Config{
					SSLType: sslType, SetupState: config.SetupStateReady,
					SSL: config.SSLConfig{Management: management},
				}
				if err := config.WriteConfig(cfg); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(config.ROOT_PATH, "config", "config.json")
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				transport := &renewalBlockedTransport{}
				http.DefaultTransport = transport
				if selected, err := GetSSL(); err != nil || selected != config.SSLTypeUser {
					t.Fatalf("外部托管仍显示自动签发：type=%s err=%v", selected, err)
				}
				for _, update := range []bool{false, true} {
					if err := GenSSL(update); UserCertificateErrorMessage(err) != "当前证书由外部维护，不能自动申请或续期" {
						t.Fatalf("外部托管未拒绝签发或续期：%v", err)
					}
				}
				Update(true)
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) || transport.calls != 0 {
					t.Fatalf("外部托管产生了配置写入或网络请求：calls=%d err=%v", transport.calls, err)
				}
			})
		}
	}
}

func TestProjectDNSReferencePreservesChallenge(t *testing.T) {
	cfg := &config.Config{SSLType: config.SSLTypeUser, SSL: config.SSLConfig{
		Management: config.SSLManagementProject, Challenge: config.SSLChallengeDNS,
	}}
	applyManagedSSLReference(cfg, config.ManagedSSLReference{Mode: config.ManagedSSLModeDefault})
	if cfg.SSL.Challenge != config.SSLChallengeDNS || cfg.EffectiveSSLType() != config.SSLTypeAutoDNS {
		t.Fatal("保存签发结果丢失了项目托管的 DNS 验证方式")
	}
}
