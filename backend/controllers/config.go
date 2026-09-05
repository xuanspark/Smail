package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/session"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// ConfigStatus 返回配置管理状态，不返回任何密文、路径或 DSN。
func ConfigStatus(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityConfigRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	manager := config.CurrentManager()
	if manager == nil {
		response.NewSuccessResponse(map[string]any{
			"available":         false,
			"unavailableReason": "当前使用旧版配置，在线配置管理尚未启用。请由部署管理员完成分区配置迁移后重启服务。",
		}).FPrint(w)
		return
	}
	status := manager.Status()
	response.NewSuccessResponse(map[string]any{
		"available":        true,
		"setupState":       status.SetupState,
		"activeGeneration": status.ActiveGeneration,
		"lastValidAt":      status.LastValidAt,
		"pendingRestart":   status.PendingRestart,
		"degradedReasons":  status.DegradedReasons,
	}).FPrint(w)
}

// GetConfigSection 返回指定分区的脱敏视图。
func GetConfigSection(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) != 4 {
		writeConfigError(w, errors.New("invalid configuration section"))
		return
	}
	manager, section, ok := configRequestManager(w, req)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	public, err := publicConfigSection(manager.Snapshot(), section)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(public).FPrint(w)
}

type configWriteRequest struct {
	ExpectedGeneration uint64                         `json:"expectedGeneration"`
	Payload            json.RawMessage                `json:"payload"`
	SecretChanges      map[string]config.SecretChange `json:"secretChanges,omitempty"`
	Confirmation       config.StepUpConfirmation      `json:"confirmation"`
}

// ValidateConfig 只验证候选配置，不写入磁盘、不改变运行时快照。
func ValidateConfig(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, section, ok := configRequestManager(w, req)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigWriteGlobal, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input configWriteRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ExpectedGeneration == 0 {
		writeConfigError(w, errors.New("invalid configuration request"))
		return
	}
	result, err := manager.Validate(req.Context(), config.ValidateRequest{
		Section: section, ExpectedGeneration: input.ExpectedGeneration,
		Payload: input.Payload, SecretChanges: input.SecretChanges,
	})
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(result).FPrint(w)
}

// UpdateConfig 保存经过授权和校验的候选配置，等待显式应用。
func UpdateConfig(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, section, ok := configRequestManager(w, req)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigWriteGlobal, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input configWriteRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ExpectedGeneration == 0 {
		writeConfigError(w, errors.New("invalid configuration request"))
		return
	}
	result, err := manager.Update(req.Context(), config.UpdateRequest{
		Section: section, ExpectedGeneration: input.ExpectedGeneration,
		Payload: input.Payload, SecretChanges: input.SecretChanges,
		Actor: configActor(ctx, req), Confirmation: input.Confirmation,
	})
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"generation": result.Generation, "pending": result.Pending}).FPrint(w)
}

type configApplyRequest struct {
	Generation uint64 `json:"generation"`
}

// ApplyConfig 应用已保存的候选配置代次。
func ApplyConfig(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, _, ok := configRequestManager(w, req)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigWriteGlobal, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input configApplyRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.Generation == 0 {
		writeConfigError(w, errors.New("invalid configuration generation"))
		return
	}
	result, err := manager.ApplyAuthorized(req.Context(), input.Generation, configActor(ctx, req))
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"generation": result.Generation, "appliedAt": result.AppliedAt}).FPrint(w)
}

// ListConfigBackups 返回脱敏备份代次列表。
func ListConfigBackups(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	// 读取脱敏代次不执行回滚；预览、票据和执行仍要求高风险授权。
	if err := authorizeCapability(ctx, access.CapabilityConfigRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	items, err := manager.ListBackups()
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(items).FPrint(w)
}

type rollbackRequest struct {
	TargetGeneration uint64                    `json:"targetGeneration"`
	Ticket           string                    `json:"ticket"`
	Confirmation     config.StepUpConfirmation `json:"confirmation"`
}

// PreviewConfigRollback 生成回滚预览，不执行回滚。
func PreviewConfigRollback(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigRollback, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input rollbackRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.TargetGeneration == 0 {
		writeConfigError(w, errors.New("invalid rollback request"))
		return
	}
	preview, err := manager.PreviewRollback(req.Context(), input.TargetGeneration, configActor(ctx, req))
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(preview).FPrint(w)
}

// IssueConfigRollbackTicket 为预览结果签发一次性回滚票据。
func IssueConfigRollbackTicket(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigRollback, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input rollbackRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.TargetGeneration == 0 {
		writeConfigError(w, errors.New("invalid rollback request"))
		return
	}
	ticket, err := manager.IssueRollbackTicket(req.Context(), input.TargetGeneration, configActor(ctx, req), input.Confirmation)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"ticket": ticket}).FPrint(w)
}

// RollbackConfig 使用一次性票据执行回滚。
func RollbackConfig(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigRollback, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input rollbackRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.Ticket) == "" {
		writeConfigError(w, errors.New("invalid rollback ticket"))
		return
	}
	result, err := manager.RollbackWithTicket(req.Context(), input.Ticket, configActor(ctx, req))
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"generation": result.Generation, "appliedAt": result.AppliedAt}).FPrint(w)
}

func ConfigAuditQuery(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityAuditRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	query, err := parseAuditQuery(req)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	items, err := manager.AuditQuery(req.Context(), query)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(items).FPrint(w)
}

func ExportConfigAudit(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityAuditExport, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input struct {
		Format string `json:"format"`
		Limit  int    `json:"limit"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeConfigError(w, err)
		return
	}
	query, err := parseAuditQuery(req)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	query.Limit = input.Limit
	if err := config.ValidateConfigAuditQuery(query); err != nil {
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
	contentType := "application/json; charset=utf-8"
	if format == "csv" {
		contentType = "text/csv; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func configRequestManager(w http.ResponseWriter, req *http.Request) (*config.ConfigManager, config.Section, bool) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return nil, "", false
	}
	section, valid := configSectionFromPath(req.URL.Path)
	if !valid {
		writeConfigError(w, errors.New("invalid configuration section"))
		return nil, "", false
	}
	return manager, section, true
}

func configManagerOrError(w http.ResponseWriter) (*config.ConfigManager, bool) {
	manager := config.CurrentManager()
	if manager == nil {
		writeConfigError(w, config.ErrManagerStorage)
		return nil, false
	}
	return manager, true
}

func configSectionFromPath(path string) (config.Section, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "config" {
		return "", false
	}
	switch parts[3] {
	case "base":
		return config.SectionRoot, true
	case "smtp":
		return config.SectionSMTP, true
	case "email":
		return config.SectionEmail, true
	case "ssl":
		return config.SectionSSL, true
	case "plugins":
		return config.SectionPlugins, true
	default:
		return "", false
	}
}

func configActor(ctx *appcontext.Context, req *http.Request) config.AuditActor {
	actor := config.AuditActor{UserID: strconv.Itoa(ctx.UserID), Role: string(ctx.EffectiveRole()), RecentAuth: ctx.RecentAuth, RecoveryCodeConfigured: ctx.RecoveryCodeConfigured}
	if sessionManager := session.Instance; sessionManager != nil {
		if cookie, err := req.Cookie(sessionManager.Cookie.Name); err == nil {
			actor.SessionID = cookie.Value
		}
	}
	return actor
}

func parseAuditQuery(req *http.Request) (config.ConfigAuditQuery, error) {
	query := req.URL.Query()
	for key, values := range query {
		if len(values) != 1 {
			return config.ConfigAuditQuery{}, config.ErrInvalidAuditQuery
		}
		switch key {
		case "actorId", "operation", "section", "limit", "from", "to":
		default:
			return config.ConfigAuditQuery{}, config.ErrInvalidAuditQuery
		}
	}
	result := config.ConfigAuditQuery{
		ActorID:   strings.TrimSpace(query.Get("actorId")),
		Operation: config.ConfigOperation(strings.TrimSpace(query.Get("operation"))),
		Section:   config.Section(strings.TrimSpace(query.Get("section"))),
	}
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil {
			return config.ConfigAuditQuery{}, config.ErrInvalidAuditQuery
		}
		result.Limit = limit
	}
	if rawFrom := strings.TrimSpace(query.Get("from")); rawFrom != "" {
		value, err := time.Parse(time.RFC3339, rawFrom)
		if err != nil {
			return config.ConfigAuditQuery{}, config.ErrInvalidAuditQuery
		}
		result.From = value
	}
	if rawTo := strings.TrimSpace(query.Get("to")); rawTo != "" {
		value, err := time.Parse(time.RFC3339, rawTo)
		if err != nil {
			return config.ConfigAuditQuery{}, config.ErrInvalidAuditQuery
		}
		result.To = value
	}
	if err := config.ValidateConfigAuditQuery(result); err != nil {
		return config.ConfigAuditQuery{}, err
	}
	return result, nil
}

func publicConfigSection(runtime config.RuntimeConfig, section config.Section) (any, error) {
	publicSecret := func(value config.StoredSecret) config.SecretStatus { return value.Status() }
	switch section {
	case config.SectionRoot:
		return map[string]any{
			"configVersion": runtime.Root.ConfigVersion,
			"schemaVersion": runtime.Root.SchemaVersion,
			"generation":    runtime.Root.Generation,
			"setupState":    runtime.Root.SetupState,
			"logLevel":      runtime.Root.LogLevel,
			"domains":       runtime.Root.Domains,
			"http":          runtime.Root.HTTP,
			"database": map[string]any{
				"type":     runtime.Root.Database.Type,
				"host":     runtime.Root.Database.Host,
				"port":     runtime.Root.Database.Port,
				"username": runtime.Root.Database.Username,
				"name":     runtime.Root.Database.Name,
				"localRef": runtime.Root.Database.LocalRef,
				"password": nil,
			},
			"databasePasswordStatus": publicSecret(runtime.Root.Database.Password),
			"security":               runtime.Root.Security,
		}, nil
	case config.SectionSMTP:
		return map[string]any{
			"meta": runtime.SMTP.Meta,
			"relay": map[string]any{
				"relayHost":           runtime.SMTP.Relay.RelayHost,
				"relayPort":           runtime.SMTP.Relay.RelayPort,
				"username":            runtime.SMTP.Relay.Username,
				"security":            runtime.SMTP.Relay.Security,
				"from":                runtime.SMTP.Relay.From,
				"fromDomain":          runtime.SMTP.Relay.FromDomain,
				"verificationEnabled": runtime.SMTP.Relay.VerificationEnabled,
				"loginPolicy":         runtime.SMTP.Relay.LoginPolicy,
				"password":            nil,
			},
			"relayPasswordStatus": publicSecret(runtime.SMTP.Relay.Password),
		}, nil
	case config.SectionEmail:
		return runtime.Email, nil
	case config.SectionSSL:
		return runtime.SSL, nil
	case config.SectionPlugins:
		plugins := make(map[string]any, len(runtime.Plugins.Plugins))
		for id, item := range runtime.Plugins.Plugins {
			settings := any(nil)
			if len(item.Settings) > 0 {
				var value any
				if err := config.ParseStrict(item.Settings, &value); err == nil {
					settings = value
				}
			}
			plugins[id] = map[string]any{
				"enabled":      item.Enabled,
				"executable":   item.Executable,
				"permissions":  item.Permissions,
				"settings":     settings,
				"secretStatus": publicSecret(item.Secret),
			}
		}
		return map[string]any{"meta": runtime.Plugins.Meta, "plugins": plugins}, nil
	default:
		return nil, errors.New("invalid configuration section")
	}
}
