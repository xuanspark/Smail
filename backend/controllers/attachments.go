package controllers

import (
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/attachments"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

func GetAttachments(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	urlInfos := strings.Split(req.RequestURI, "/")
	if len(urlInfos) != 4 {
		response.NewErrorResponse(response.ParamsError, "", "").FPrint(w)
		return
	}
	emailId := cast.ToInt(urlInfos[2])
	cid := urlInfos[3]

	contentType, content, err := attachments.GetAttachments(ctx, emailId, cid)
	if err != nil {
		writeAttachmentError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if _, err := w.Write(content); err != nil {
		log.WithContext(ctx).WithError(err).Error("write attachment response failed")
	}
}

func Download(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	urlInfos := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(urlInfos) != 4 || urlInfos[0] != "attachments" || urlInfos[1] != "download" {
		response.NewErrorResponse(response.ParamsError, "", "").FPrint(w)
		return
	}
	emailId := cast.ToInt(urlInfos[2])
	index := cast.ToInt(urlInfos[3])

	fileName, content, err := attachments.GetAttachmentsByIndex(ctx, emailId, index)
	if err != nil {
		writeAttachmentError(ctx, w, err)
		return
	}
	fileName = safeDownloadName(fileName)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"attachment\"; filename*=UTF-8''%s", url.PathEscape(fileName)))
	if _, err := w.Write(content); err != nil {
		log.WithContext(ctx).WithError(err).Error("write attachment download failed")
	}
}

func writeAttachmentError(ctx *context.Context, w http.ResponseWriter, err error) {
	log.WithContext(ctx).WithError(err).Error("load attachment failed")
	if errors.Is(err, attachments.ErrAttachmentUnavailable) {
		response.NewErrorResponse(response.ParamsError, "\u9644\u4ef6\u4e0d\u53ef\u7528", "").FPrint(w)
		return
	}
	response.NewErrorResponse(response.ServerError, "\u8bfb\u53d6\u9644\u4ef6\u5931\u8d25", "").FPrint(w)
}

func safeDownloadName(name string) string {
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '\r' || r == '\n' || r == 0 || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	runes := []rune(name)
	if len(runes) > 180 {
		name = strings.TrimSpace(string(runes[:180]))
	}
	if name == "" {
		return "attachment"
	}
	return name
}
