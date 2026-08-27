package session

import (
	"context"
	"testing"

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
	user := &models.User{ID: 7, Account: "admin", Name: "Admin", IsAdmin: 1, SessionVersion: 4}
	if err := PutUser(loaded, user); err != nil {
		t.Fatal(err)
	}
	identity, err := GetUser(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if identity.ID != user.ID || identity.Account != user.Account || identity.Name != user.Name || identity.IsAdmin != 1 || identity.SessionVersion != 4 {
		t.Fatalf("unexpected session identity: %+v", identity)
	}
}
