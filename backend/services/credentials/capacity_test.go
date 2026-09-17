package credentials

import (
	"errors"
	"testing"

	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestCredentialBusyDoesNotConsumeOrReplaceCodes(t *testing.T) {
	service := newFixture(t)
	if err := service.engine.Sync2(&models.ClientCredential{}); err != nil {
		t.Fatal(err)
	}
	if err := service.SetSecurityQuestion(1, "问题", "answer"); err != nil {
		t.Fatal(err)
	}
	if err := service.SetProtocolEnabled(1, ProtocolIMAP, true); err != nil {
		t.Fatal(err)
	}
	protocolCode, err := service.GenerateProtocolCode(1, ProtocolIMAP)
	if err != nil {
		t.Fatal(err)
	}
	codes, err := service.GenerateRecoveryCodes(1, 5)
	if err != nil {
		t.Fatal(err)
	}
	release := passwordtest.Hold(t)
	if _, err := service.VerifySecurityQuestion(1, "answer"); !errors.Is(err, password.ErrBusy) {
		t.Fatal(err)
	}
	if _, err := service.CheckProtocolCode(1, ProtocolIMAP, protocolCode); !errors.Is(err, password.ErrBusy) {
		t.Fatal(err)
	}
	if _, err := service.CheckClientOrProtocolCode(1, ProtocolIMAP, protocolCode); !errors.Is(err, password.ErrBusy) {
		t.Fatal(err)
	}
	if err := service.ConsumeRecoveryCode(1, codes[0]); !errors.Is(err, password.ErrBusy) {
		t.Fatal(err)
	}
	if _, err := service.GenerateRecoveryCodes(1, 5); !errors.Is(err, password.ErrBusy) {
		t.Fatal(err)
	}
	release()
	if err := service.ConsumeRecoveryCode(1, codes[0]); err != nil {
		t.Fatalf("繁忙后恢复码已被错误消费或撤销：%v", err)
	}
	if _, err := service.CheckProtocolCode(1, ProtocolIMAP, protocolCode); err != nil {
		t.Fatal(err)
	}
}
