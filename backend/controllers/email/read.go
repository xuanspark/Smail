package email

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/detail"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"net/http"
)

type markReadRequest struct {
	IDs []int `json:"ids"`
}

func MarkRead(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData markReadRequest
	if err := decodeEmailJSONRequest(w, req, &reqData, maxEmailJSONRequestBytes); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid email read request")
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}

	if len(reqData.IDs) <= 0 || len(reqData.IDs) > maxEmailOperationIDs {
		response.NewErrorResponse(response.ParamsError, "ID错误", "").FPrint(w)
		return
	}

	for _, id := range reqData.IDs {
		if _, err := detail.GetEmailDetail(ctx, id, true); err != nil {
			log.WithContext(ctx).WithError(err).Error("mark email read failed")
			response.NewErrorResponse(response.ServerError, "更新邮件失败", "").FPrint(w)
			return
		}
	}
	response.NewSuccessResponse("success").FPrint(w)

}
