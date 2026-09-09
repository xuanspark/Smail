package controllers

import (
	"net/http"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/validation"
)

// UpdateProfile 只接受本人显示名称，不允许传入目标账号、角色、禁用状态或密码。
func UpdateProfile(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if ctx == nil || ctx.UserID <= 0 {
		response.NewErrorResponse(response.NoAccessPrivileges, "无权修改个人资料", "").FPrint(w)
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityProfileWriteOwn, "", true); err != nil {
		writeUserAuthorizationError(w, err, "")
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if err := decodeUserRequest(w, req, &input); err != nil {
		response.NewErrorResponse(response.ParamsError, "个人资料参数错误", "").FPrint(w)
		return
	}
	name, err := validation.NormalizeDisplayName(input.Name)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "请输入有效的显示名称", "").FPrint(w)
		return
	}
	if db.Instance == nil {
		response.NewErrorResponse(response.ServerError, "个人资料保存失败", "").FPrint(w)
		return
	}
	affected, err := db.Instance.Context(req.Context()).ID(ctx.UserID).Cols("name").Update(&models.User{Name: name})
	if err != nil {
		response.NewErrorResponse(response.ServerError, "个人资料保存失败", "").FPrint(w)
		return
	}
	// MySQL 重复保存相同名称时可能返回零行，必须区分无变化和账号已不存在。
	if affected == 0 {
		found, err := db.Instance.Context(req.Context()).ID(ctx.UserID).Exist(&models.User{})
		if err != nil || !found {
			response.NewErrorResponse(response.ServerError, "账号已不可用，请重新登录", "").FPrint(w)
			return
		}
	}
	ctx.UserName = name
	response.NewSuccessResponse(map[string]any{"name": name}).FPrint(w)
}
