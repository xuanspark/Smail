package address

import "testing"

// TestAuditEmailAddressValidationMustRejectMalformedAddresses 验证转发地址校验拒绝明显非法输入。
func TestAuditEmailAddressValidationMustRejectMalformedAddresses(t *testing.T) {
	invalid := []string{
		"@example.com",
		"user@",
		"user..name@example.com",
		"user@example..com",
		"user name@example.com",
	}
	for _, address := range invalid {
		if IsValidEmailAddress(address) {
			t.Errorf("非法邮箱地址不应通过校验: %q", address)
		}
	}
}
