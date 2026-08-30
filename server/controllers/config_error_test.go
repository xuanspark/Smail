package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.pcux.cn/xuans/Smail/config"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/dbmigration"
)

func TestWriteConfigErrorKeepsStableAuthorizationCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeConfigError(recorder, &access.Denial{Code: access.CodeRecoveryCodeRequired})

	var payload struct {
		ErrorNo int            `json:"errorNo"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode authorization error response: %v", err)
	}
	if payload.ErrorNo != response.NoAccessPrivileges {
		t.Fatalf("unexpected authorization error number: %d", payload.ErrorNo)
	}
	if payload.Data["code"] != string(access.CodeRecoveryCodeRequired) {
		t.Fatalf("stable authorization code was not exposed: %#v", payload.Data)
	}
}

func TestWriteConfigErrorDoesNotExposeInternalErrorText(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeConfigError(recorder, &access.Denial{Code: access.CodeRecentAuthRequired})

	body := strings.ToLower(recorder.Body.String())
	for _, forbidden := range []string{"password", "secret", "token", "nonce", "ticket", "challenge"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("authorization response contains sensitive response text: %s", body)
		}
	}
}

func TestEnsureConfigRequestIDPreservesSafeHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/config/status", nil)
	request.Header.Set("X-Request-ID", "ui-request-42")
	if got := EnsureConfigRequestID(recorder, request); got != "ui-request-42" {
		t.Fatalf("safe request id was not preserved: %q", got)
	}
	if recorder.Header().Get("X-Request-ID") != "ui-request-42" {
		t.Fatalf("response request id header was not preserved: %q", recorder.Header().Get("X-Request-ID"))
	}
}

func TestEnsureConfigRequestIDReplacesUnsafeHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/config/status", nil)
	request.Header.Set("X-Request-ID", "../../secret")
	requestID := EnsureConfigRequestID(recorder, request)
	if requestID == "../../secret" || !requestIDPattern.MatchString(requestID) {
		t.Fatalf("unsafe request id was not replaced: %q", requestID)
	}
}

func TestWriteConfigErrorUsesStableCodeAndHTTPStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Request-ID", "request-99")
	writeConfigError(recorder, config.ErrGenerationConflict)

	if recorder.Code != 409 {
		t.Fatalf("unexpected HTTP status: %d", recorder.Code)
	}
	var payload struct {
		ErrorMsg string                   `json:"errorMsg"`
		Data     response.ConfigErrorData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode configuration error response: %v", err)
	}
	if payload.Data.Code != "CONFIG_GENERATION_CONFLICT" {
		t.Fatalf("unexpected stable error code: %q", payload.Data.Code)
	}
	if payload.Data.RequestID != "request-99" {
		t.Fatalf("unexpected request id: %q", payload.Data.RequestID)
	}
	if payload.ErrorMsg == "" || strings.Contains(payload.ErrorMsg, "generation conflict") {
		t.Fatalf("configuration error message is not a Chinese public message: %q", payload.ErrorMsg)
	}
}

func TestConfigErrorDetailsMapsDatabaseMigrationFailures(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		code    string
		status  int
		message string
	}{
		{
			name:    "invalid target",
			err:     config.ErrDatabaseMigrationTarget,
			code:    "DATABASE_MIGRATION_VALIDATION_FAILED",
			status:  422,
			message: "数据库迁移参数校验未通过",
		},
		{
			name:    "structured target exists",
			err:     dbmigration.ErrSQLTargetExists,
			code:    "DATABASE_MIGRATION_TARGET_EXISTS",
			status:  409,
			message: "目标数据库已存在，请更换数据库名称",
		},
		{
			name:    "copier unavailable",
			err:     dbmigration.ErrSQLCopyUnavailable,
			code:    "DATABASE_MIGRATION_COPIER_UNAVAILABLE",
			status:  503,
			message: "数据库迁移复制器暂不可用，请先完成服务部署",
		},
		{
			name:    "connection unavailable",
			err:     dbmigration.ErrSQLConnectionUnavailable,
			code:    "DATABASE_MIGRATION_CONNECTION_UNAVAILABLE",
			status:  503,
			message: "数据库连接暂不可用，请检查数据库配置和服务状态",
		},
		{
			name:    "unsupported object",
			err:     dbmigration.ErrSQLUnsupportedObject,
			code:    "DATABASE_MIGRATION_UNSUPPORTED_OBJECT",
			status:  422,
			message: "源数据库包含暂不支持迁移的对象，请先处理后重试",
		},
		{
			name:    "data verification failed",
			err:     dbmigration.ErrSQLDataVerification,
			code:    "SQL_DATA_VERIFICATION_FAILED",
			status:  409,
			message: "数据库迁移数据校验未通过，源数据库可能在迁移期间发生变化",
		},
		{
			name:    "source busy",
			err:     dbmigration.ErrSQLSourceBusy,
			code:    "SQL_SOURCE_BUSY",
			status:  409,
			message: "源数据库存在活动连接，请停止相关连接后重试迁移",
		},
		{
			name:    "switch outcome unknown",
			err:     dbmigration.ErrSwitchOutcomeUnknown,
			code:    "SWITCH_OUTCOME_UNKNOWN",
			status:  503,
			message: "数据库迁移配置切换结果暂不确定，请先确认当前配置和目标数据库状态",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			code, status, message, fields := configErrorDetails(test.err)
			if code != test.code || status != test.status || message != test.message || fields != nil {
				t.Fatalf("config error details = (%q, %d, %q, %#v), want (%q, %d, %q, nil)", code, status, message, fields, test.code, test.status, test.message)
			}
		})
	}
}

func TestConfigErrorDetailsMapsRuntimeSnapshotUnavailable(t *testing.T) {
	code, status, message, fields := configErrorDetails(config.ErrRuntimeSnapshotUnavailable)
	if code != "CONFIG_RUNTIME_SNAPSHOT_UNAVAILABLE" {
		t.Fatalf("error code = %q", code)
	}
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", status)
	}
	if message != "配置已保存，但运行时配置尚未切换，请重试应用" {
		t.Fatalf("message = %q", message)
	}
	if fields != nil {
		t.Fatalf("fields = %+v, want nil", fields)
	}
}

func TestConfigErrorDetailsMapsRollbackInProgress(t *testing.T) {
	code, status, message, _ := configErrorDetails(config.ErrRollbackInProgress)
	if code != "CONFIG_ROLLBACK_IN_PROGRESS" || status != http.StatusConflict {
		t.Fatalf("rollback in progress mapping = %q/%d, want conflict", code, status)
	}
	if message == "" || strings.Contains(message, "rollback in progress") {
		t.Fatalf("rollback in progress message is not a Chinese public message: %q", message)
	}
}
