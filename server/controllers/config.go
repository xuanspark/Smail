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
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	status := manager.Status()
	response.NewSuccessResponse(map[string]any{
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
	if err := authorizeCapability(ctx, access.CapabilityConfigRollback, "", false); err != nil {
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
	query := parseAuditQuery(req)
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
	if ctx.EffectiveRole() != access.RoleSuperAdmin || !ctx.RecentAuth {
		writeConfigAccessError(w, errors.New("audit export requires recent super administrator authentication"))
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
	query := parseAuditQuery(req)
	query.Limit = input.Limit
	var data []byte
	var err error
	if strings.EqualFold(input.Format, "csv") {
		data, err = manager.ExportAuditCSV(req.Context(), query)
	} else {
		data, err = manager.ExportAuditJSON(req.Context(), query)
	}
	if err != nil {
		writeConfigError(w, err)
		return
	}
	contentType := "application/json; charset=utf-8"
	if strings.EqualFold(input.Format, "csv") {
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
		writeConfigError(w, errors.New("configuration manager unavailable"))
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

func parseAuditQuery(req *http.Request) config.ConfigAuditQuery {
	query := req.URL.Query()
	result := config.ConfigAuditQuery{ActorID: strings.TrimSpace(query.Get("actorId")), Operation: config.ConfigOperation(strings.TrimSpace(query.Get("operation"))), Section: config.Section(strings.TrimSpace(query.Get("section")))}
	result.Limit, _ = strconv.Atoi(query.Get("limit"))
	if value, err := time.Parse(time.RFC3339, query.Get("from")); err == nil {
		result.From = value
	}
	if value, err := time.Parse(time.RFC3339, query.Get("to")); err == nil {
		result.To = value
	}
	return result
}

func publicConfigSection(runtime config.RuntimeConfig, section config.Section) (any, error) {
	publicSecret := func(value config.StoredSecret) config.SecretStatus { return value.Status() }
	switch section {
	case config.SectionRoot:
		return map[string]any{"configVersion": runtime.Root.ConfigVersion, "schemaVersion": runtime.Root.SchemaVersion, "generation": runtime.Root.Generation, "setupState": runtime.Root.SetupState, "logLevel": runtime.Root.LogLevel, "domains": runtime.Root.Domains, "http": runtime.Root.HTTP, "database": map[string]any{"type": runtime.Root.Database.Type, "host": runtime.Root.Database.Host, "port": runtime.Root.Database.Port, "username": runtime.Root.Database.Username, "name": runtime.Root.Database.Name, "sqliteLocalRef": runtime.Root.Database.SQLiteLocalRef, "password": publicSecret(runtime.Root.Database.Password)}, "security": runtime.Root.Security}, nil
	case config.SectionSMTP:
		return map[string]any{"meta": runtime.SMTP.Meta, "relay": map[string]any{"relayHost": runtime.SMTP.Relay.RelayHost, "relayPort": runtime.SMTP.Relay.RelayPort, "username": runtime.SMTP.Relay.Username, "security": runtime.SMTP.Relay.Security, "fromDomain": runtime.SMTP.Relay.FromDomain, "verificationEnabled": runtime.SMTP.Relay.VerificationEnabled, "loginPolicy": runtime.SMTP.Relay.LoginPolicy, "password": publicSecret(runtime.SMTP.Relay.Password)}}, nil
	case config.SectionEmail:
		return runtime.Email, nil
	case config.SectionSSL:
		return runtime.SSL, nil
	case config.SectionPlugins:
		plugins := make(map[string]any, len(runtime.Plugins.Plugins))
		for id, item := range runtime.Plugins.Plugins {
			plugins[id] = map[string]any{"enabled": item.Enabled, "executable": item.Executable, "permissions": item.Permissions, "settingsConfigured": len(item.Settings) > 0}
		}
		return map[string]any{"meta": runtime.Plugins.Meta, "plugins": plugins}, nil
	default:
		return nil, errors.New("invalid configuration section")
	}
}

func writeConfigAccessError(w http.ResponseWriter, err error) { writeConfigError(w, err) }
func writeConfigError(w http.ResponseWriter, err error) {
	status := response.NoAccessPrivileges
	message := "无权访问或请求不满足安全条件"
	if errors.Is(err, config.ErrManagerStorage) {
		status = response.ServerError
		message = "配置服务不可用"
	}
	if errors.Is(err, config.ErrGenerationConflict) {
		status = response.ParamsError
		message = "配置已发生变化，请刷新后重试"
	}
	response.NewErrorResponse(status, message, "").FPrint(w)
}
