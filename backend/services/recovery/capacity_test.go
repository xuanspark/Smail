package recovery

import (
	"errors"
	"testing"

	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestRecoveryBusyPreservesTicket(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	ticketID, err := service.issueTicket(user.ID, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	release := passwordtest.Hold(t)
	for index := 0; index < 12; index++ {
		if err := service.Complete(ticketID, "NewPass123"); !errors.Is(err, password.ErrBusy) {
			t.Fatalf("繁忙期间票据失效或返回错误：%v", err)
		}
	}
	release()
	if err := service.Complete(ticketID, "NewPass123"); err != nil {
		t.Fatalf("繁忙恢复后不能使用原票据：%v", err)
	}
	if err := service.Complete(ticketID, "OtherPass12"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("成功后票据未保持一次性：%v", err)
	}
}

func TestRecoveryBusyDoesNotRestoreRevokedTicket(t *testing.T) {
	service, _, _, user := newRecoveryFixture(t)
	ticketID, err := service.issueTicket(user.ID, user.SessionVersion)
	if err != nil {
		t.Fatal(err)
	}
	release := passwordtest.Hold(t)
	if err := service.Complete(ticketID, "NewPass123"); !errors.Is(err, password.ErrBusy) {
		t.Fatal(err)
	}
	service.InvalidateUser(user.ID)
	release()
	if err := service.Complete(ticketID, "NewPass123"); !errors.Is(err, ErrRecoveryExpired) {
		t.Fatalf("撤销的票据被恢复：%v", err)
	}
}
