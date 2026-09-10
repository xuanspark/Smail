package email

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/detail"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"net/http"
)

type emailDetailRequest struct {
	ID int `json:"id"`
}

func EmailDetail(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var retData emailDetailRequest
	if err := decodeEmailJSONRequest(w, req, &retData, maxEmailJSONRequestBytes); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid email detail request")
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}

	if retData.ID <= 0 {
		response.NewErrorResponse(response.ParamsError, "ID错误", "").FPrint(w)
		return
	}

	email, err := detail.GetEmailDetail(ctx, retData.ID, true)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("load email detail failed")
		response.NewErrorResponse(response.ServerError, "读取邮件失败", "").FPrint(w)
		return
	}

	response.NewSuccessResponse(email).FPrint(w)

}
