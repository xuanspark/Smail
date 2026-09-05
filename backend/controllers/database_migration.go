package controllers

import (
	"net/http"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/dbmigration"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

type databaseMigrationPreviewRequest struct {
	TargetName         string                   `json:"targetName"`
	Target             *dbmigration.TargetInput `json:"target,omitempty"`
	ExpectedGeneration uint64                   `json:"expectedGeneration"`
}

type databaseMigrationExecuteRequest struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// PreviewDatabaseMigration 验证三种数据库的结构化目标连接，不创建或修改目标库。
func PreviewDatabaseMigration(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityDatabaseMigrate, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	service := dbmigration.Current()
	if service == nil {
		writeConfigError(w, dbmigration.ErrExecutorUnavailable)
		return
	}
	var input databaseMigrationPreviewRequest
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ExpectedGeneration == 0 {
		writeConfigError(w, dbmigration.ErrInvalidRequest)
		return
	}
	status := manager.Status()
	if input.ExpectedGeneration != status.ActiveGeneration || len(status.PendingRestart) > 0 {
		writeConfigError(w, config.ErrGenerationConflict)
		return
	}
	var record dbmigration.Record
	var token string
	var err error
	if input.Target != nil {
		if input.TargetName != "" {
			writeConfigError(w, dbmigration.ErrInvalidRequest)
			return
		}
		target, prepareErr := input.Target.Prepare(config.ROOT_PATH)
		if prepareErr != nil {
			writeConfigError(w, prepareErr)
			return
		}
		record, token, err = service.PreviewTarget(req.Context(), manager.Snapshot().Root.Database, status.ActiveGeneration, target)
	} else {
		record, token, err = service.Preview(req.Context(), manager.Snapshot().Root.Database, status.ActiveGeneration, input.TargetName)
	}
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"migration": record, "token": token}).FPrint(w)
}

// ExecuteDatabaseMigration 执行预览票据对应的固定迁移计划，不接受 DSN、SQL 或连接参数。
func ExecuteDatabaseMigration(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityDatabaseMigrate, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	if dbmigration.Current() == nil {
		writeConfigError(w, dbmigration.ErrExecutorUnavailable)
		return
	}
	var input databaseMigrationExecuteRequest
	if err := decodeJSONRequest(w, req, &input); err != nil {
		writeConfigError(w, dbmigration.ErrInvalidRequest)
		return
	}
	// 迁移最长十分钟，不能沿用普通接口的九十秒响应写入期限。
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(11 * time.Minute))
	record, err := dbmigration.Current().ExecuteAuthorized(req.Context(), input.ID, input.Token, configActor(ctx, req))
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(record).FPrint(w)
}

// GetDatabaseMigration 返回迁移状态，不返回密码、密钥或底层数据库错误。
func GetDatabaseMigration(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityDatabaseMigrate, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	service := dbmigration.Current()
	if service == nil {
		writeConfigError(w, dbmigration.ErrExecutorUnavailable)
		return
	}
	parts := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "database" || parts[3] != "migrations" || strings.TrimSpace(parts[4]) == "" {
		writeConfigError(w, dbmigration.ErrInvalidRequest)
		return
	}
	record, err := service.Get(parts[4])
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(record).FPrint(w)
}
