package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/dbmigration"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// EnsureConfigRequestID 为配置 API 设置可回溯但不包含敏感信息的请求 ID.
func EnsureConfigRequestID(w http.ResponseWriter, req *http.Request) string {
	requestID := ""
	if req != nil {
		requestID = strings.TrimSpace(req.Header.Get("X-Request-ID"))
	}
	if !requestIDPattern.MatchString(requestID) {
		requestID = newConfigRequestID()
	}
	w.Header().Set("X-Request-ID", requestID)
	return requestID
}

func newConfigRequestID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "config-request"
	}
	return hex.EncodeToString(value)
}

func configErrorDetails(err error) (string, int, string, map[string]string) {
	code := "CONFIG_REQUEST_FAILED"
	status := http.StatusBadRequest
	message := "配置请求未完成"
	fields := map[string]string(nil)

	var denial *access.Denial
	switch {
	case errors.As(err, &denial):
		code = string(denial.Code)
		status = http.StatusForbidden
		message = "无权访问或请求不满足安全条件"
		if denial.Code == access.CodeRecentAuthRequired {
			message = "此操作需要最近十分钟内完成登录，请重新登录后再试"
		}
	case errors.Is(err, config.ErrManagerStorage):
		code = "CONFIG_MANAGER_UNAVAILABLE"
		status = http.StatusServiceUnavailable
		message = "配置服务暂时不可用，请稍后重试"
	case errors.Is(err, config.ErrRollbackInProgress):
		code = "CONFIG_ROLLBACK_IN_PROGRESS"
		status = http.StatusConflict
		message = "配置回滚正在执行，请稍后查看结果"
	case errors.Is(err, config.ErrRuntimeSnapshotUnavailable):
		code = "CONFIG_RUNTIME_SNAPSHOT_UNAVAILABLE"
		status = http.StatusServiceUnavailable
		message = "配置已保存，但运行时配置尚未切换，请重试应用"
	case errors.Is(err, config.ErrGenerationConflict):
		code = "CONFIG_GENERATION_CONFLICT"
		status = http.StatusConflict
		message = "配置已发生变化，请刷新后重试"
	case errors.Is(err, config.ErrAuthorizationRequired):
		code = "CONFIG_AUTHORIZATION_REQUIRED"
		status = http.StatusForbidden
		message = "当前账户没有执行此配置操作的权限"
	case errors.Is(err, config.ErrStepUpRequired):
		code = "CONFIG_STEP_UP_REQUIRED"
		status = http.StatusPreconditionRequired
		message = "请完成近期重新认证或 WebAuthn 二次确认"
	case errors.Is(err, config.ErrAuditWrite):
		code = "CONFIG_AUDIT_WRITE_FAILED"
		status = http.StatusServiceUnavailable
		message = "配置审计暂时不可用，操作未完成"
	case errors.Is(err, config.ErrBackupListingUnavailable), errors.Is(err, config.ErrAuditQueryUnavailable), errors.Is(err, config.ErrAuditStorageUnavailable), errors.Is(err, config.ErrConfigRecoveryRequired):
		code = "CONFIG_READ_UNAVAILABLE"
		status = http.StatusServiceUnavailable
		message = "配置历史数据暂时不可用，请稍后重试"
	case errors.Is(err, config.ErrNoBackup):
		code = "CONFIG_BACKUP_NOT_FOUND"
		status = http.StatusNotFound
		message = "未找到可用的配置备份"
	case errors.Is(err, config.ErrAuditNotFound):
		code = "CONFIG_AUDIT_NOT_FOUND"
		status = http.StatusNotFound
		message = "未找到指定的配置审计记录"
	case errors.Is(err, dbmigration.ErrMigrationNotFound):
		code = "DATABASE_MIGRATION_NOT_FOUND"
		status = http.StatusNotFound
		message = "未找到指定的数据库迁移记录"
	case errors.Is(err, dbmigration.ErrPreviewExpired):
		code = "DATABASE_MIGRATION_PREVIEW_EXPIRED"
		status = http.StatusConflict
		message = "数据库迁移预览已过期，请重新生成预览"
	case errors.Is(err, dbmigration.ErrExecutorUnavailable):
		code = "DATABASE_MIGRATION_UNAVAILABLE"
		status = http.StatusServiceUnavailable
		message = "数据库迁移执行器暂未配置，请先完成服务部署"
	case errors.Is(err, dbmigration.ErrSwitchUnavailable):
		code = "DATABASE_MIGRATION_SWITCH_UNAVAILABLE"
		status = http.StatusServiceUnavailable
		message = "数据库迁移已完成校验，但配置切换通道暂不可用，当前配置未切换"
	case errors.Is(err, dbmigration.ErrSwitchOutcomeUnknown):
		code = "SWITCH_OUTCOME_UNKNOWN"
		status = http.StatusServiceUnavailable
		message = "数据库迁移配置切换结果暂不确定，请先确认当前配置和目标数据库状态"
	case errors.Is(err, dbmigration.ErrTargetExists):
		code = "DATABASE_MIGRATION_TARGET_EXISTS"
		status = http.StatusConflict
		message = "目标数据库已存在，请更换数据库名称"
	case errors.Is(err, dbmigration.ErrSQLTargetExists):
		code = "DATABASE_MIGRATION_TARGET_EXISTS"
		status = http.StatusConflict
		message = "目标数据库已存在，请更换数据库名称"
	case errors.Is(err, dbmigration.ErrSQLCopyUnavailable):
		code = "DATABASE_MIGRATION_COPIER_UNAVAILABLE"
		status = http.StatusServiceUnavailable
		message = "数据库迁移复制器暂不可用，请先完成服务部署"
	case errors.Is(err, dbmigration.ErrSQLConnectionUnavailable):
		code = "DATABASE_MIGRATION_CONNECTION_UNAVAILABLE"
		status = http.StatusServiceUnavailable
		message = "数据库连接暂不可用，请检查数据库配置和服务状态"
	case errors.Is(err, dbmigration.ErrSQLUnsupportedObject):
		code = "DATABASE_MIGRATION_UNSUPPORTED_OBJECT"
		status = http.StatusUnprocessableEntity
		message = "源数据库包含暂不支持迁移的对象，请先处理后重试"
	case errors.Is(err, dbmigration.ErrSQLDataVerification):
		code = "SQL_DATA_VERIFICATION_FAILED"
		status = http.StatusConflict
		message = "数据库迁移数据校验未通过，源数据库可能在迁移期间发生变化"
	case errors.Is(err, dbmigration.ErrSQLSourceBusy):
		code = "SQL_SOURCE_BUSY"
		status = http.StatusConflict
		message = "源数据库存在活动连接，请停止相关连接后重试迁移"
	case errors.Is(err, dbmigration.ErrUnsupportedDatabase):
		code = "DATABASE_MIGRATION_UNSUPPORTED"
		status = http.StatusUnprocessableEntity
		message = "当前数据库类型暂不支持在线迁移"
	case errors.Is(err, dbmigration.ErrMigrationState):
		code = "DATABASE_MIGRATION_STATE_INVALID"
		status = http.StatusConflict
		message = "数据库迁移当前状态不允许执行该操作"
	case errors.Is(err, dbmigration.ErrPreviewInvalid):
		code = "DATABASE_MIGRATION_PREVIEW_INVALID"
		status = http.StatusForbidden
		message = "数据库迁移预览票据无效"
	case errors.Is(err, config.ErrDatabaseMigrationTarget), errors.Is(err, dbmigration.ErrInvalidRequest), errors.Is(err, dbmigration.ErrInvalidTargetName), errors.Is(err, dbmigration.ErrImmutableSetting), errors.Is(err, dbmigration.ErrSourceDatabase):
		code = "DATABASE_MIGRATION_VALIDATION_FAILED"
		status = http.StatusUnprocessableEntity
		message = "数据库迁移参数校验未通过"
	case errors.Is(err, config.ErrInvalidRuntimeConfig), errors.Is(err, config.ErrPortConflict), errors.Is(err, config.ErrInvalidDomainConfig), errors.Is(err, config.ErrPluginCapability), errors.Is(err, config.ErrInvalidSetupState), errors.Is(err, config.ErrInvalidDatabase), errors.Is(err, config.ErrInvalidSMTP), errors.Is(err, config.ErrInvalidSSL), errors.Is(err, config.ErrInvalidPlugin), errors.Is(err, config.ErrInvalidHTTP), errors.Is(err, config.ErrInvalidEmail), errors.Is(err, config.ErrInvalidLogLevel), errors.Is(err, config.ErrInvalidSecretChange), errors.Is(err, config.ErrSecretClearDenied), errors.Is(err, config.ErrSecretUnavailable), errors.Is(err, config.ErrInvalidSection), errors.Is(err, config.ErrInvalidSectionReference), errors.Is(err, config.ErrSectionGenerationMismatch), errors.Is(err, config.ErrInvalidSSLMode), errors.Is(err, config.ErrInvalidSSLMaterial), errors.Is(err, config.ErrInvalidSSLReference), errors.Is(err, config.ErrUnsafeSSLFile), errors.Is(err, config.ErrSSLCertificateMismatch), errors.Is(err, config.ErrSSLCertificateDomain), errors.Is(err, config.ErrSSLCertificateExpired), errors.Is(err, config.ErrSSLPrivateKeyPermission), errors.Is(err, config.ErrInvalidAuditQuery), errors.Is(err, config.ErrInvalidAuditFormat):
		code = "CONFIG_VALIDATION_FAILED"
		status = http.StatusUnprocessableEntity
		message = "配置校验未通过，请检查填写内容"
	case errors.Is(err, config.ErrTransactionInvalid), errors.Is(err, config.ErrTransactionPhase), errors.Is(err, config.ErrConfigDigestMismatch), errors.Is(err, config.ErrConfigGenerationMismatch):
		code = "CONFIG_TRANSACTION_FAILED"
		status = http.StatusServiceUnavailable
		message = "配置事务未完成，系统仍保持原有配置"
	default:
		fields = map[string]string{"request": "invalid"}
	}

	return code, status, message, fields
}

func writeConfigError(w http.ResponseWriter, err error) {
	requestID := strings.TrimSpace(w.Header().Get("X-Request-ID"))
	if !requestIDPattern.MatchString(requestID) {
		requestID = EnsureConfigRequestID(w, nil)
	}
	code, status, message, fields := configErrorDetails(err)
	data := response.ConfigErrorData{Code: code, RequestID: requestID, Fields: fields}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	response.NewErrorResponse(response.ConfigErrorNumber(status), message, data).FPrint(w)
}

func writeConfigAccessError(w http.ResponseWriter, err error) {
	writeConfigError(w, err)
}
