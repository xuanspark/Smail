package validation

import "testing"

func TestNotificationAddressAndSenderValidation(t *testing.T) {
	domain := "example.com"
	primarySender := NotificationAddress(domain)
	if primarySender != "notification@example.com" {
		t.Fatalf("NotificationAddress want notification@example.com, got %s", primarySender)
	}

	legacySender := LegacyNotificationAddress(domain)
	if legacySender != "notification+system@example.com" {
		t.Fatalf("LegacyNotificationAddress want notification+system@example.com, got %s", legacySender)
	}

	domains := []string{"example.com", "other.com"}

	// 验证 IsNotificationAddress：主地址和历史地址均应被识别为通知地址
	if !IsNotificationAddress("notification@example.com", domains) {
		t.Fatalf("notification@example.com should be recognized as notification address")
	}
	if !IsNotificationAddress("notification+system@example.com", domains) {
		t.Fatalf("notification+system@example.com should be recognized as notification address")
	}
	if !IsNotificationAddress("NOTIFICATION@OTHER.COM", domains) {
		t.Fatalf("case insensitive notification address check failed")
	}
	if IsNotificationAddress("user@example.com", domains) {
		t.Fatalf("user@example.com should NOT be notification address")
	}

	// 验证 IsAuthorizedNotificationSender
	if !IsAuthorizedNotificationSender("notification@example.com", domain) {
		t.Fatalf("notification@example.com should be authorized sender")
	}
	if !IsAuthorizedNotificationSender("notification+system@example.com", domain) {
		t.Fatalf("notification+system@example.com should be authorized sender")
	}
	if IsAuthorizedNotificationSender("attacker@example.com", domain) {
		t.Fatalf("attacker@example.com should NOT be authorized notification sender")
	}
	if IsAuthorizedNotificationSender("notification@attacker.com", domain) {
		t.Fatalf("notification@attacker.com should NOT be authorized notification sender for domain")
	}
}
