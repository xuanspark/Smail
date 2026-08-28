package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
)

func TestLoginSecurityFieldsByRole(t *testing.T) {
	ordinary := loginSecurityFields(&models.User{ID: 1, Role: string(access.RoleUser)})
	if ordinary["recoveryCodesConfigured"] != false || ordinary["showRecoverySetupPrompt"] != true {
		t.Fatalf("ordinary user security fields = %#v", ordinary)
	}

	superAdmin := loginSecurityFields(&models.User{ID: 2, Role: string(access.RoleSuperAdmin)})
	if superAdmin["recoveryCodesConfigured"] != false || superAdmin["showRecoverySetupPrompt"] != false {
		t.Fatalf("super administrator security fields = %#v", superAdmin)
	}
}

func TestLoginLimiterBlocksAndExpires(t *testing.T) {
	limiter := newLoginLimiter()
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	limiter.now = func() time.Time { return now }
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	key := limiter.key(request, " User.Name@example.com ")
	if strings.Contains(key, "192.0.2.10") || strings.Contains(key, "user.name") {
		t.Fatal("login limiter key contains identifying input")
	}
	for index := 0; index < loginFailureLimit-1; index++ {
		limiter.recordFailure(key)
		if limiter.blocked(key) {
			t.Fatalf("login was blocked too early at attempt %d", index+1)
		}
	}
	limiter.recordFailure(key)
	if !limiter.blocked(key) {
		t.Fatal("login was not blocked after the failure limit")
	}
	now = now.Add(loginBlockTime + time.Second)
	if limiter.blocked(key) {
		t.Fatal("login block did not expire")
	}
}

func TestLoginLimiterAggregatesAccountFailuresAcrossIPs(t *testing.T) {
	limiter := newLoginLimiter(3)
	key := limiter.accountKey("user@example.com")
	for index := 0; index < 3; index++ {
		limiter.recordFailure(key)
	}
	if !limiter.blocked(key) {
		t.Fatal("account-wide login failures were not blocked")
	}
}

func TestLoginLimiterAggregatesIPFailuresAcrossAccounts(t *testing.T) {
	limiter := newLoginLimiter(3)
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	key := limiter.ipKey(request)
	for index := 0; index < 3; index++ {
		limiter.recordFailure(key)
	}
	if !limiter.blocked(key) {
		t.Fatal("IP-wide login failures were not blocked")
	}
}

func TestLoginLimiterClearsSuccessfulKey(t *testing.T) {
	limiter := newLoginLimiter()
	request := httptest.NewRequest(http.MethodPost, "https://mail.example.com/api/login", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	key := limiter.key(request, "user")
	limiter.recordFailure(key)
	limiter.clear(key)
	if len(limiter.attempts) != 0 || limiter.blocked(key) {
		t.Fatal("successful login did not clear failures")
	}
}
