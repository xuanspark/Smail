package session

import (
	"context"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"github.com/alexedwards/scs/v2"
)

func TestUserIdentityRoundTrip(t *testing.T) {
	oldInstance := Instance
	Instance = scs.New()
	t.Cleanup(func() { Instance = oldInstance })

	loaded, err := Instance.Load(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	user := &models.User{ID: 7, Account: "admin", Name: "Admin", IsAdmin: 1, SessionVersion: 4, SecurityVersion: 3, MustChangePassword: 1}
	Instance.Put(loaded, SecuritySetupDismissedKey, true)
	if err := PutUser(loaded, user); err != nil {
		t.Fatal(err)
	}
	if Instance.GetBool(loaded, SecuritySetupDismissedKey) {
		t.Fatal("new login retained the previous dismissal")
	}
	Instance.Put(loaded, SecuritySetupDismissedKey, true)
	if err := RefreshRecentAuthentication(loaded, user); err != nil {
		t.Fatal(err)
	}
	if !Instance.GetBool(loaded, SecuritySetupDismissedKey) {
		t.Fatal("recent authentication cleared the current dismissal")
	}
	identity, err := GetUser(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID != user.ID || identity.Account != user.Account || identity.Name != user.Name || identity.IsAdmin != 1 || identity.SessionVersion != 4 || identity.SecurityVersion != 3 || identity.MustChangePassword != 1 {
		t.Fatalf("unexpected session identity: %+v", identity)
	}
	if !identity.HasRecentAuthentication(time.Now().UTC()) {
		t.Fatal("completed login did not record recent authentication")
	}
}

func TestRecentAuthenticationRejectsMissingFutureAndExpiredTimes(t *testing.T) {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name string
		at   int64
		want bool
	}{
		{"legacy session", 0, false},
		{"future time", now.Add(time.Second).Unix(), false},
		{"fresh login", now.Unix(), true},
		{"before expiry", now.Add(-recentAuthenticationLifetime + time.Second).Unix(), true},
		{"exact expiry", now.Add(-recentAuthenticationLifetime).Unix(), false},
		{"expired login", now.Add(-time.Hour).Unix(), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			identity := UserIdentity{ID: 1, AuthenticatedAt: test.at}
			if identity.HasRecentAuthentication(now) != test.want {
				t.Fatal("unexpected recent authentication state")
			}
		})
	}
}
