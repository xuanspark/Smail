package imap_server

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"testing"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestIMAPBusyReturnsUnavailableAndAllowsRetry(t *testing.T) {
	certificate, err := os.ReadFile(config.Instance.SSLPublicKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("测试证书不可解析")
	}
	client, err := imapclient.DialTLS(imapTestAddr, &imapclient.Options{TLSConfig: &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		var temporary *imap.Error
		err := client.Login("testCase@test.domain", imapTestProtocolCode).Wait()
		if !errors.As(err, &temporary) || temporary.Type != imap.StatusResponseTypeNo || temporary.Code != imap.ResponseCodeUnavailable {
			t.Fatalf("IMAP 繁忙响应错误：%v", err)
		}
	}
	release()
	if err := client.Login("testCase@test.domain", imapTestProtocolCode).Wait(); err != nil {
		t.Fatalf("IMAP 繁忙恢复后无法认证：%v", err)
	}
}
