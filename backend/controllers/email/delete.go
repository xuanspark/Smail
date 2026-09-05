package email

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/del_email"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"net/http"
)

type emailDeleteRequest struct {
	IDs       []int `json:"ids"`
	ForcedDel bool  `json:"forcedDel"`
}

func EmailDelete(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData emailDeleteRequest
	if err := decodeEmailJSONRequest(w, req, &reqData, maxEmailJSONRequestBytes); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid email delete request")
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	if len(reqData.IDs) <= 0 || len(reqData.IDs) > maxEmailOperationIDs {
		response.NewErrorResponse(response.ParamsError, "ID错误", "").FPrint(w)
		return
	}

	err := del_email.DelEmail(ctx, reqData.IDs, reqData.ForcedDel)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("delete email failed")
		response.NewErrorResponse(response.ServerError, "\u5220\u9664\u90ae\u4ef6\u5931\u8d25", "").FPrint(w)
		return
	}
	response.NewSuccessResponse("success").FPrint(w)

}
