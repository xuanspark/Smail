package controllers

import (
	"git.pcux.cn/xuans/Smail/dto/response"
	"net/http"
)

func Ping(w http.ResponseWriter, req *http.Request) {
	response.NewSuccessResponse("pong").FPrint(w)
}
