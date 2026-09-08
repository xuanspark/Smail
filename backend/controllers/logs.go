package controllers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/services/runtimelog"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

func logQueryError(w http.ResponseWriter, err error) {
	if errors.Is(err, logstore.ErrInvalid) || errors.Is(err, runtimelog.ErrInvalid) {
		writeConfigError(w, config.ErrInvalidAuditQuery)
		return
	}
	log.WithError(err).Warn("日志查询失败")
	response.NewErrorResponse(response.ServerError, "日志查询失败，请检查日志存储状态", "LOG_QUERY_FAILED").FPrint(w)
}

// GetLogs 只读日志入口，授权在服务端检查，不依赖导航隐藏。
func GetLogs(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := authorizeCapability(ctx, access.CapabilityAuditRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	values := req.URL.Query()
	limit, offset := 10, 0
	var err error
	if values.Has("limit") {
		limit, err = strconv.Atoi(values.Get("limit"))
	}
	if err == nil && values.Has("offset") {
		offset, err = strconv.Atoi(values.Get("offset"))
	}
	if err != nil || limit < 1 || limit > 500 || offset < 0 || offset > 100000 {
		writeConfigError(w, config.ErrInvalidAuditQuery)
		return
	}
	bounded, cancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer cancel()
	query := logstore.Query{Kind: values.Get("kind"), ActorID: values.Get("actorId"), Limit: limit + 1, Offset: offset}
	for _, item := range []struct {
		name   string
		target *time.Time
	}{{"from", &query.From}, {"to", &query.To}} {
		if values.Has(item.name) {
			*item.target, err = time.Parse(time.RFC3339Nano, values.Get(item.name))
			if err != nil {
				writeConfigError(w, config.ErrInvalidAuditQuery)
				return
			}
		}
	}
	if query.To.IsZero() {
		query.To = time.Now()
	}
	if query.From.IsZero() {
		query.From = query.To.Add(-24 * time.Hour)
	}
	if query.From.After(query.To) {
		writeConfigError(w, config.ErrInvalidAuditQuery)
		return
	}
	if values.Get("kind") == "runtime" {
		writer := runtimelog.Current()
		if writer == nil {
			logQueryError(w, logstore.ErrUnavailable)
			return
		}
		// 显式保留旧文件列表入口，新页面默认跨文件查询。
		if values.Get("listFiles") == "true" {
			files, err := writer.Files()
			if err != nil {
				logQueryError(w, err)
				return
			}
			response.NewSuccessResponse(map[string]any{"files": files}).FPrint(w)
			return
		}
		page, err := writer.Query(bounded, runtimelog.Query{File: values.Get("file"), From: query.From, To: query.To, Offset: offset, Limit: limit})
		if err != nil {
			logQueryError(w, err)
			return
		}
		response.NewSuccessResponse(page).FPrint(w)
		return
	}
	if query.Kind != "config" && query.Kind != "operation" && query.Kind != "mail" {
		writeConfigError(w, config.ErrInvalidAuditQuery)
		return
	}
	store := logstore.Current()
	items, err := store.Query(bounded, query)
	if err != nil {
		logQueryError(w, err)
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	response.NewSuccessResponse(map[string]any{"items": items, "hasMore": hasMore, "retentionDays": 30}).FPrint(w)
}
