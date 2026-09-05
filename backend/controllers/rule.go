package controllers

import (
	"encoding/json"
	"fmt"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/rule"
	"git.pcux.cn/xuans/Smail/services/rule/match"
	"git.pcux.cn/xuans/Smail/utils/address"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	log "github.com/sirupsen/logrus"
	"io"
	"net/http"
	"strings"
)

const (
	maxRuleRequestBytes int64 = 64 << 10
	maxRulesPerUser           = 100
	maxRuleConditions         = 16
	maxRuleNameBytes          = 128
	maxRuleValueBytes         = 2048
	maxRuleParamsBytes        = 512
	maxRuleSort               = 10000
)

func GetRule(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	res, err := rule.GetAllRules(ctx, ctx.UserID)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("load rules failed")
		response.NewErrorResponse(response.ServerError, "\u8bfb\u53d6\u89c4\u5219\u5931\u8d25", "").FPrint(w)
		return
	}
	response.NewSuccessResponse(res).FPrint(w)
}

func UpsertRule(ctx *context.Context, w http.ResponseWriter, req *http.Request) {

	var data dto.Rule
	if err := decodeRuleBody(w, req, &data); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid rule request")
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	data.Name = strings.TrimSpace(data.Name)
	data.Params = strings.TrimSpace(data.Params)
	if data.Id < 0 || data.UserId != 0 || data.Name == "" || len(data.Name) > maxRuleNameBytes || len(data.Params) > maxRuleParamsBytes || data.Sort < -maxRuleSort || data.Sort > maxRuleSort {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	if data.Action == dto.FORWARD && !address.IsValidEmailAddress(data.Params) {

		response.NewErrorResponse(response.ParamsError, "ParamsError error", i18n.GetText(ctx.Lang, "invalid_email_address")).FPrint(w)
		return
	}

	if (data.Action == dto.READ || data.Action == dto.DELETE) && data.Params != "" {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	if len(data.Rules) == 0 || len(data.Rules) > maxRuleConditions || !array.InArray(data.Action, []dto.RuleType{dto.READ, dto.FORWARD, dto.DELETE, dto.MOVE}) {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	for _, r := range data.Rules {
		if r == nil || r.Rule == "" || len(r.Rule) > maxRuleValueBytes || !array.InArray(r.Field, []string{"From", "Subject", "To", "Cc", "Text", "Html", "Content"}) || !array.InArray(r.Type, []string{match.RuleTypeRegex, match.RuleTypeContains, match.RuleTypeEq}) {
			response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
			return
		}
		if r.Type == match.RuleTypeRegex {
			if err := match.ValidateRegex(r.Rule); err != nil {
				response.NewErrorResponse(response.ParamsError, "\u6b63\u5219\u8868\u8fbe\u5f0f\u65e0\u6548", "").FPrint(w)
				return
			}
		}
	}

	encoded, err := data.Encode()
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("encode rule failed")
		response.NewErrorResponse(response.ServerError, "\u4fdd\u5b58\u89c4\u5219\u5931\u8d25", "").FPrint(w)
		return
	}
	if err := save(ctx, encoded); err != nil {
		log.WithContext(ctx).WithError(err).Error("save rule failed")
		response.NewErrorResponse(response.ServerError, "\u4fdd\u5b58\u89c4\u5219\u5931\u8d25", "").FPrint(w)
		return
	}
	response.NewSuccessResponse("succ").FPrint(w)
}

func decodeRuleBody(w http.ResponseWriter, req *http.Request, target any) error {
	req.Body = http.MaxBytesReader(w, req.Body, maxRuleRequestBytes)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func save(ctx *context.Context, p *models.Rule) error {

	if p.Id > 0 {
		_, err := db.Instance.Exec(db.WithContext(ctx, "update rule set name=? ,value = ? ,action = ?,params = ?,sort = ? where id = ? and user_id = ?"), p.Name, p.Value, p.Action, p.Params, p.Sort, p.Id, ctx.UserID)
		if err != nil {
			return errors.Wrap(err)
		}
		return nil
	} else {
		count, err := db.Instance.Where("user_id = ?", ctx.UserID).Count(new(models.Rule))
		if err != nil {
			return errors.Wrap(err)
		}
		if count >= maxRulesPerUser {
			return fmt.Errorf("rule limit exceeded")
		}
		_, err = db.Instance.Exec(db.WithContext(ctx, "insert into rule (name,value,user_id,action,params,sort) values (?,?,?,?,?,?)"), p.Name, p.Value, ctx.UserID, p.Action, p.Params, p.Sort)
		if err != nil {
			return errors.Wrap(err)
		}
		return nil
	}

}

type delRuleReq struct {
	Id int `json:"id"`
}

func DelRule(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var data delRuleReq
	if err := decodeRuleBody(w, req, &data); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid delete rule request")
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	if data.Id <= 0 {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	_, err := db.Instance.Exec(db.WithContext(ctx, "delete from rule where id =? and user_id =?"), data.Id, ctx.UserID)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("delete rule failed")
		response.NewErrorResponse(response.ServerError, "\u5220\u9664\u89c4\u5219\u5931\u8d25", "").FPrint(w)
		return
	}

	response.NewSuccessResponse("succ").FPrint(w)
}
