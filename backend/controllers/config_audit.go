package controllers

import (
	"net/http"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// GetConfigAuditDetail 返回一条已脱敏的配置审计事件。
func GetConfigAuditDetail(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityAuditRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "audits" || parts[3] != "config" || strings.TrimSpace(parts[4]) == "" {
		writeConfigError(w, config.ErrInvalidAuditQuery)
		return
	}
	event, err := manager.AuditDetail(req.Context(), parts[4])
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(event).FPrint(w)
}

type configAuditExportRequest struct {
	Format    string `json:"format"`
	ID        string `json:"id"`
	ActorID   string `json:"actorId"`
	Operation string `json:"operation"`
	Section   string `json:"section"`
	Limit     int    `json:"limit"`
	From      string `json:"from"`
	To        string `json:"to"`
}

// ExportConfigAuditFormal 实现正式配置审计导出接口。
func ExportConfigAuditFormal(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityAuditExport, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input configAuditExportRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeConfigError(w, err)
		return
	}
	query, err := auditExportQuery(input)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	format := strings.ToLower(strings.TrimSpace(input.Format))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeConfigError(w, config.ErrInvalidAuditFormat)
		return
	}
	var data []byte
	if format == "csv" {
		data, err = manager.ExportAuditCSV(req.Context(), query)
	} else {
		data, err = manager.ExportAuditJSON(req.Context(), query)
	}
	if err != nil {
		writeConfigError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", `attachment; filename="config-audit.`+format+`"`)
	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	_, _ = w.Write(data)
}

func auditExportQuery(input configAuditExportRequest) (config.ConfigAuditQuery, error) {
	query := config.ConfigAuditQuery{
		ID:        strings.TrimSpace(input.ID),
		ActorID:   strings.TrimSpace(input.ActorID),
		Operation: config.ConfigOperation(strings.TrimSpace(input.Operation)),
		Section:   config.Section(strings.TrimSpace(input.Section)),
		Limit:     input.Limit,
	}
	if raw := strings.TrimSpace(input.From); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return config.ConfigAuditQuery{}, config.ErrInvalidAuditQuery
		}
		query.From = value
	}
	if raw := strings.TrimSpace(input.To); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return config.ConfigAuditQuery{}, config.ErrInvalidAuditQuery
		}
		query.To = value
	}
	if err := config.ValidateConfigAuditQuery(query); err != nil {
		return config.ConfigAuditQuery{}, err
	}
	return query, nil
}
