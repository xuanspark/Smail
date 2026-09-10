package email

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"net/http"
)

type moveRequest struct {
	GroupId   int    `json:"group_id"`
	GroupName string `json:"group_name"`
	IDs       []int  `json:"ids"`
}

func Move(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData moveRequest
	if err := decodeEmailJSONRequest(w, req, &reqData, maxEmailJSONRequestBytes); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid email move request")
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}

	if len(reqData.IDs) <= 0 || len(reqData.IDs) > maxEmailOperationIDs {
		response.NewErrorResponse(response.ParamsError, "ID错误", "").FPrint(w)
		return
	}

	if name, ok := models.GroupCodeToName[reqData.GroupId]; ok {
		err := group.Move2DefaultBox(ctx, reqData.IDs, name)
		if err != nil {
			log.WithContext(ctx).WithError(err).Error("move email failed")
			response.NewErrorResponse(response.ServerError, "移动邮件失败", "").FPrint(w)
			return
		}
	} else if err := group.MoveMailToGroup(ctx, reqData.IDs, reqData.GroupId); err != nil {
		log.WithContext(ctx).WithError(err).Error("move email to user mailbox failed")
		response.NewErrorResponse(response.ServerError, "移动邮件失败", "").FPrint(w)
		return
	}
	response.NewSuccessResponse("success").FPrint(w)

}
