package controllers

import (
	"errors"
	"net/http"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/accountgroup"
	"git.pcux.cn/xuans/Smail/utils/context"
)

func accountWriteGuard(ctx *context.Context, target models.User) accountgroup.WriteGuard {
	if ctx == nil {
		return nil
	}
	return accountgroup.Guard(ctx.UserID, ctx.EffectiveRole(), ctx.SessionVersion, ctx.SecurityVersion, target)
}

func requireAccountScope(ctx *context.Context, w http.ResponseWriter, targetID int) bool {
	if ctx == nil || !accountgroup.CanAccess(db.Instance, ctx.UserID, ctx.EffectiveRole(), targetID) {
		writeSecurityError(w, response.NoAccessPrivileges, "无权查看或管理该账号")
		return false
	}
	return true
}

func GetAccountGroups(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil {
		writeSecurityError(w, response.NeedLogin, "请先登录")
		return
	}
	groups, err := accountgroup.VisibleGroups(db.Instance, ctx.UserID, ctx.EffectiveRole())
	if err != nil {
		writeAccountGroupError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	response.NewSuccessResponse(map[string]any{"groups": groups, "can_manage": ctx.EffectiveRole() == access.RoleSuperAdmin}).FPrint(w)
}

func requireAccountGroupManagement(ctx *context.Context, w http.ResponseWriter) bool {
	if err := authorizeCapability(ctx, access.CapabilityAccountGroupManage, "", false); err != nil {
		writeUserAuthorizationError(w, err, "无权管理账号分组")
		return false
	}
	return true
}

func SaveAccountGroup(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !requireAccountGroupManagement(ctx, w) {
		return
	}
	var input struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if decodeUserRequest(w, req, &input) != nil || input.ID < 0 {
		writeAccountGroupError(w, accountgroup.ErrInvalid)
		return
	}
	name, err := accountgroup.NormalizeName(input.Name)
	if err != nil {
		writeAccountGroupError(w, err)
		return
	}
	if db.Instance == nil {
		writeSecurityError(w, response.ServerError, "数据库不可用")
		return
	}
	item := models.AccountGroup{ID: input.ID, Name: name}
	if item.ID == 0 {
		_, err = db.Instance.Insert(&item)
	} else {
		var found bool
		found, err = db.Instance.ID(item.ID).Exist(&models.AccountGroup{})
		if err == nil && !found {
			err = accountgroup.ErrInvalid
		}
		if err == nil {
			_, err = db.Instance.ID(item.ID).Cols("name").Update(&item)
		}
	}
	if err != nil {
		writeAccountGroupError(w, err)
		return
	}
	response.NewSuccessResponse(item).FPrint(w)
}

func SetAccountGroupMembership(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if !requireAccountGroupManagement(ctx, w) {
		return
	}
	var input struct {
		UserID   int   `json:"user_id"`
		GroupIDs []int `json:"group_ids"`
		Version  int   `json:"version"`
	}
	if decodeUserRequest(w, req, &input) != nil || input.UserID <= 0 || input.Version < 1 || input.GroupIDs == nil {
		writeAccountGroupError(w, accountgroup.ErrInvalid)
		return
	}
	if db.Instance == nil {
		writeSecurityError(w, response.ServerError, "数据库不可用")
		return
	}
	var user models.User
	found, err := db.Instance.ID(input.UserID).Get(&user)
	if err != nil || !found {
		writeAccountGroupError(w, accountgroup.ErrInvalid)
		return
	}
	if input.Version != user.SecurityVersion {
		writeAccountGroupError(w, accountgroup.ErrConflict)
		return
	}
	if err := accountgroup.SetMembership(db.Instance, user, input.GroupIDs, accountWriteGuard(ctx, user)); err != nil {
		writeAccountGroupError(w, err)
		return
	}
	InvalidateSecurityRuntimeUser(user.ID, user.Account)
	response.NewSuccessResponse(map[string]any{
		"user_id":            user.ID,
		"membership_version": user.SecurityVersion + 1,
	}).FPrint(w)
}

func writeAccountGroupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, accountgroup.ErrDenied):
		writeSecurityError(w, response.NoAccessPrivileges, "无权查看或管理该分组")
	case errors.Is(err, accountgroup.ErrInvalid):
		writeSecurityError(w, response.ParamsError, "分组参数无效：名称为 1–32 个字符，普通用户只能属于一个分组")
	case errors.Is(err, accountgroup.ErrConflict):
		writeSecurityError(w, response.ParamsError, "账号状态已变化，请刷新列表后重试")
	default:
		writeSecurityError(w, response.ServerError, "分组操作失败，请检查是否重名并刷新后重试")
	}
}
