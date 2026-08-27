package rule

import (
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func newRuleTestEngine(t *testing.T) *xorm.Engine {
	t.Helper()
	oldDB := db.Instance
	engine, err := xorm.NewEngine("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Instance = engine
	t.Cleanup(func() {
		_ = engine.Close()
		db.Instance = oldDB
	})
	if err := engine.Sync2(&models.UserEmail{}, &models.Group{}); err != nil {
		t.Fatal(err)
	}
	return engine
}

func TestDoRuleReadDistinguishesExistingAndMissingRelation(t *testing.T) {
	engine := newRuleTestEngine(t)
	if _, err := engine.Insert(&models.UserEmail{UserID: 1, EmailID: 10, IsRead: 0}); err != nil {
		t.Fatal(err)
	}
	ctx := &context.Context{}
	rule := &dto.Rule{UserId: 1, Action: dto.READ}
	user := &models.User{ID: 1}
	email := &parsemail.Email{MessageId: 10}
	if err := DoRule(ctx, rule, email, user, nil); err != nil {
		t.Fatal(err)
	}
	if err := DoRule(ctx, rule, email, user, nil); err != nil {
		t.Fatalf("already-read relation should be idempotent: %v", err)
	}
	email.MessageId = 11
	if err := DoRule(ctx, rule, email, user, nil); err == nil || !strings.Contains(err.Error(), "relation not found") {
		t.Fatalf("missing relation was not rejected: %v", err)
	}
}

func TestDoRuleRejectsOwnerMismatch(t *testing.T) {
	newRuleTestEngine(t)
	err := DoRule(&context.Context{}, &dto.Rule{UserId: 2, Action: dto.READ}, &parsemail.Email{MessageId: 10}, &models.User{ID: 1}, nil)
	if err == nil || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("owner mismatch was not rejected: %v", err)
	}
}

func TestDoRuleMoveRejectsAnotherUsersGroup(t *testing.T) {
	engine := newRuleTestEngine(t)
	if _, err := engine.Insert(&models.UserEmail{UserID: 1, EmailID: 10}, &models.Group{ID: 5, UserId: 2, Name: "private"}); err != nil {
		t.Fatal(err)
	}
	err := DoRule(&context.Context{}, &dto.Rule{UserId: 1, Action: dto.MOVE, Params: "5"}, &parsemail.Email{MessageId: 10}, &models.User{ID: 1}, nil)
	if err == nil {
		t.Fatal("another user's group was accepted")
	}
}

func TestDoRuleReturnsDatabaseError(t *testing.T) {
	engine := newRuleTestEngine(t)
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	err := DoRule(&context.Context{}, &dto.Rule{UserId: 1, Action: dto.READ}, &parsemail.Email{MessageId: 10}, &models.User{ID: 1}, nil)
	if err == nil {
		t.Fatal("closed database error was ignored")
	}
}

func TestDoRuleRejectsInvalidForwardAddress(t *testing.T) {
	newRuleTestEngine(t)
	err := DoRule(&context.Context{}, &dto.Rule{UserId: 1, Action: dto.FORWARD, Params: "invalid"}, &parsemail.Email{MessageId: 10}, &models.User{ID: 1}, nil)
	if err == nil {
		t.Fatal("invalid forward address was accepted")
	}
}
