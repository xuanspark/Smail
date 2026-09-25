package controllers

import (
	"context"
	"strconv"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/accountgroup"
	"git.pcux.cn/xuans/Smail/services/logstore"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

func authorizeLogs(ctx *appcontext.Context) error {
	if ctx == nil || ctx.UserID <= 0 {
		return &access.Denial{Code: access.CodeAccessDenied}
	}
	// 此授权只允许进入已实施数据范围过滤的日志接口，不影响旧的全局审计接口。
	return access.Authorize(access.Request{ActorRole: ctx.EffectiveRole(), Capability: access.CapabilityAuditRead, OwnResource: true})
}

func runtimeLogActors(requestContext context.Context, ctx *appcontext.Context) (map[string]bool, error) {
	if ctx.EffectiveRole() == access.RoleSuperAdmin {
		return nil, nil
	}
	actors := map[string]bool{strconv.Itoa(ctx.UserID): true}
	if ctx.EffectiveRole() == access.RoleAdmin {
		if db.Instance == nil {
			return nil, logstore.ErrUnavailable
		}
		var users []models.User
		query := db.Instance.Context(requestContext).Cols("id").Where(models.OrdinaryUserPredicate)
		defer query.Close()
		accountgroup.ApplyScope(query, ctx.UserID, ctx.EffectiveRole())
		if err := query.Find(&users); err != nil {
			return nil, logstore.ErrUnavailable
		}
		for _, user := range users {
			if user.ID != ctx.UserID {
				actors[strconv.Itoa(user.ID)] = false
			}
		}
	}
	return actors, nil
}
