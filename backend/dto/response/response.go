package response

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
)

const (
	NeedSetup          = 402
	NeedLogin          = 403
	NoAccessPrivileges = 405
	ParamsError        = 100
	ServerError        = 500
)

type Response struct {
	ErrorNo  int    `json:"errorNo"`
	ErrorMsg string `json:"errorMsg"`
	Data     any    `json:"data"`
}

func (p *Response) FPrint(w http.ResponseWriter) {
	bytesData, err := json.Marshal(p)
	if err != nil {
		log.WithError(err).Error("serialize HTTP response failed")
		http.Error(w, "\u54cd\u5e94\u5e8f\u5217\u5316\u5931\u8d25", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(bytesData); err != nil {
		log.WithError(err).Error("write HTTP response failed")
	}
}

func NewSuccessResponse(data any) *Response {
	return &Response{
		Data: data,
	}
}

func NewErrorResponse(errorNo int, errorMsg string, data any) *Response {
	return &Response{
		ErrorNo:  errorNo,
		ErrorMsg: errorMsg,
		Data:     data,
	}
}
