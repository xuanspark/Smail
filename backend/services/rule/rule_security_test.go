package rule

import (
	stdcontext "context"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestMatchRuleFailsClosedForInvalidRules(t *testing.T) {
	email := &parsemail.Email{Subject: "report"}
	cases := []*dto.Rule{
		nil,
		{},
		{Rules: []*dto.Value{nil}},
		{Rules: []*dto.Value{{Field: "Subject", Type: "unknown", Rule: "report"}}},
	}
	for index, rule := range cases {
		if MatchRule(&context.Context{}, rule, email) {
			t.Fatalf("invalid rule %d matched", index)
		}
	}
}

func TestMatchRuleStopsWhenExecutionBudgetExpires(t *testing.T) {
	base, cancel := stdcontext.WithCancel(stdcontext.Background())
	cancel()
	ctx := &context.Context{Context: base}
	rule := &dto.Rule{Rules: []*dto.Value{{Field: "Subject", Type: "contains", Rule: "report"}}}
	if MatchRule(ctx, rule, &parsemail.Email{Subject: "report"}) {
		t.Fatal("expired rule execution context was ignored")
	}
}

func TestGetAllRulesRejectsCorruptedStoredJSON(t *testing.T) {
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
	if err := engine.Sync2(&models.Rule{}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Insert(&models.Rule{UserId: 1, Name: "broken", Value: "{"}); err != nil {
		t.Fatal(err)
	}
	if _, err := GetAllRules(&context.Context{}, 1); err == nil {
		t.Fatal("corrupted stored rule was accepted")
	}
}
