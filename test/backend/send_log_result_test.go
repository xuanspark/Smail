package send

import "testing"

func TestDeliveryLogResultUsesConfirmedRecipientCount(t *testing.T) {
	for _, item := range []struct {
		accepted, total int
		failed          bool
		want            string
	}{
		{3, 3, false, "accepted"},
		{1, 3, true, "partial"},
		{1, 3, false, "partial"},
		{3, 3, true, "partial"},
		{0, 3, true, "failure"},
		{0, 3, false, "unknown"},
		{0, 0, false, "unknown"},
	} {
		if got := deliveryLogResult(item.accepted, item.total, item.failed); got != item.want {
			t.Fatalf("接收人数 %d/%d，失败=%v：得到 %s，期望 %s", item.accepted, item.total, item.failed, got, item.want)
		}
	}
}
