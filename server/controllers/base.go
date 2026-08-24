package controllers

import (
	"git.pcux.cn/xuans/Smail/utils/context"
	"net/http"
)

type HandlerFunc func(*context.Context, http.ResponseWriter, *http.Request)
