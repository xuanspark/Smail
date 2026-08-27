package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	_ "modernc.org/sqlite"
	"xorm.io/xorm"
)

func TestDecodeRuleBodyRejectsTrailingJSON(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/rule/add", strings.NewReader(`{"name":"one"} {"name":"two"}`))
	var target map[string]any
	if err := decodeRuleBody(httptest.NewRecorder(), req, &target); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
}

func TestDecodeRuleBodyRejectsOversizedBody(t *testing.T) {
	body := `{"name":"` + strings.Repeat("a", int(maxRuleRequestBytes)) + `"}`
	req := httptest.NewRequest("POST", "/api/rule/add", strings.NewReader(body))
	var target map[string]any
	if err := decodeRuleBody(httptest.NewRecorder(), req, &target); err == nil {
		t.Fatal("oversized rule body was accepted")
	}
}

func TestUpsertRuleRejectsResourceLimitViolations(t *testing.T) {
	validCondition := &dto.Value{Field: "Subject", Type: "contains", Rule: "report"}
	cases := []dto.Rule{
		{Name: strings.Repeat("a", maxRuleNameBytes+1), Rules: []*dto.Value{validCondition}, Action: dto.READ},
		{Name: "too-many", Rules: make([]*dto.Value, maxRuleConditions+1), Action: dto.READ},
		{Name: "too-long", Rules: []*dto.Value{{Field: "Subject", Type: "contains", Rule: strings.Repeat("a", maxRuleValueBytes+1)}}, Action: dto.READ},
		{Name: "bad-sort", Rules: []*dto.Value{validCondition}, Action: dto.READ, Sort: maxRuleSort + 1},
		{Name: "forged-owner", UserId: 2, Rules: []*dto.Value{validCondition}, Action: dto.READ},
	}
	for index := range cases[1].Rules {
		cases[1].Rules[index] = validCondition
	}
	for index, ruleData := range cases {
		body, err := json.Marshal(ruleData)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/rule/add", strings.NewReader(string(body)))
		UpsertRule(&appcontext.Context{UserID: 1}, recorder, request)
		var result response.Response
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("case %d response decode failed: %v", index, err)
		}
		if result.ErrorNo != response.ParamsError {
			t.Fatalf("case %d was not rejected: %+v", index, result)
		}
	}
}

func TestSaveRuleEnforcesPerUserLimit(t *testing.T) {
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
	rules := make([]models.Rule, maxRulesPerUser)
	for index := range rules {
		rules[index] = models.Rule{UserId: 1, Name: "existing", Value: "[]", Action: int(dto.READ)}
	}
	if _, err := engine.Insert(&rules); err != nil {
		t.Fatal(err)
	}
	err = save(&appcontext.Context{UserID: 1}, &models.Rule{Name: "blocked", Value: "[]", Action: int(dto.READ)})
	if err == nil {
		t.Fatal("rule limit was not enforced")
	}
}
