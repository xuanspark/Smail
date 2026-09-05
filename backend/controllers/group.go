package controllers

import (
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/i18n"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"net/http"
)

func GetUserGroupList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	defaultGroup := []*models.Group{
		{ID: models.INBOX, Name: i18n.GetText(ctx.Lang, "inbox"), ParentId: 0, UserId: 0, FullPath: "/"},     // 收件箱
		{ID: models.Junk, Name: i18n.GetText(ctx.Lang, "junk"), ParentId: 0, UserId: 0, FullPath: "/"},       //垃圾邮件
		{ID: models.Deleted, Name: i18n.GetText(ctx.Lang, "deleted"), ParentId: 0, UserId: 0, FullPath: "/"}, //已删除
	}

	infos, err := group.GetGroupList(ctx)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("query group list failed")
		response.NewErrorResponse(response.ServerError, "\u83b7\u53d6\u5206\u7ec4\u5217\u8868\u5931\u8d25", "").FPrint(w)
		return
	}

	response.NewSuccessResponse(append(defaultGroup, infos...)).FPrint(w)
}

func GetUserGroup(ctx *context.Context, w http.ResponseWriter, req *http.Request) {

	retData := []*group.GroupItem{
		{
			Label: i18n.GetText(ctx.Lang, "inbox"),
			Tag:   dto.SearchTag{Type: 0, Status: -1, GroupId: 0}.ToString(),
		},
		{
			Label: i18n.GetText(ctx.Lang, "outbox"),
			Tag:   dto.SearchTag{Type: 1, Status: -1}.ToString(),
		},
		{
			Label: i18n.GetText(ctx.Lang, "sketch"),
			Tag:   dto.SearchTag{Type: 0, Status: 4}.ToString(),
		},
		{
			Label: i18n.GetText(ctx.Lang, "junk"),
			Tag:   dto.SearchTag{Type: -1, Status: 5}.ToString(),
		},
		{
			Label: i18n.GetText(ctx.Lang, "deleted"),
			Tag:   dto.SearchTag{Type: -1, Status: 3}.ToString(),
		},
	}

	infos, err := group.GetGroupInfoList(ctx)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("query group tree failed")
		response.NewErrorResponse(response.ServerError, "\u83b7\u53d6\u5206\u7ec4\u5217\u8868\u5931\u8d25", "").FPrint(w)
		return
	}
	retData = array.Merge(retData, infos)

	response.NewSuccessResponse(retData).FPrint(w)
}

type addGroupRequest struct {
	Name     string `json:"name"`
	ParentId int    `json:"parent_id"`
}

func AddGroup(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData addGroupRequest
	if err := decodeJSONRequest(w, req, &reqData); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid group create request")
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	newGroup, err := group.CreateGroup(ctx, reqData.Name, reqData.ParentId)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("create group failed")
		response.NewErrorResponse(response.ServerError, "\u521b\u5efa\u5206\u7ec4\u5931\u8d25", "").FPrint(w)
		return
	}

	response.NewSuccessResponse(newGroup.ID).FPrint(w)
}

type delGroupRequest struct {
	Id int `json:"id"`
}

func DelGroup(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData delGroupRequest
	if err := decodeJSONRequest(w, req, &reqData); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid group delete request")
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}
	succ, err := group.DelGroup(ctx, reqData.Id)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("delete group failed")
		response.NewErrorResponse(response.ServerError, "\u5220\u9664\u5206\u7ec4\u5931\u8d25", "").FPrint(w)
		return
	}
	response.NewSuccessResponse(succ).FPrint(w)
}
