package dto

import (
	"encoding/json"
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/models"
	"strings"
)

type RuleType int

// 1已读，2转发，3删除
var (
	READ    RuleType = 1
	FORWARD RuleType = 2
	DELETE  RuleType = 3
	MOVE    RuleType = 4
)

type Rule struct {
	Id     int      `json:"id"`
	UserId int      `json:"user_id"`
	Name   string   `json:"name"`
	Rules  []*Value `json:"rules"`
	Action RuleType `json:"action"`
	Params string   `json:"params"`
	Sort   int      `json:"sort"`
}

type Value struct {
	Field string `json:"field"`
	Type  string `json:"type"`
	Rule  string `json:"rule"`
}

func (p *Rule) Decode(data *models.Rule) (*Rule, error) {
	if p == nil || data == nil {
		return nil, errors.New("rule is nil")
	}
	if strings.TrimSpace(data.Value) != "" {
		if err := json.Unmarshal([]byte(data.Value), &p.Rules); err != nil {
			return nil, fmt.Errorf("decode stored rule: %w", err)
		}
	}
	p.Id = data.Id
	p.Name = data.Name
	p.Action = RuleType(data.Action)
	p.Sort = data.Sort
	p.Params = data.Params
	p.UserId = data.UserId
	return p, nil
}

func (p *Rule) Encode() (*models.Rule, error) {
	if p == nil {
		return nil, errors.New("rule is nil")
	}
	v, err := json.Marshal(p.Rules)
	if err != nil {
		return nil, fmt.Errorf("encode rule: %w", err)
	}
	ret := &models.Rule{
		Id:     p.Id,
		Name:   p.Name,
		Value:  string(v),
		Action: int(p.Action),
		Sort:   p.Sort,
		Params: p.Params,
	}
	return ret, nil
}
