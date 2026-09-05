package dbmigration

import (
	"context"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/credential"
)

// TargetInput 只接受结构化连接字段，不接受任意 DSN、SQL 或密码密文。
type TargetInput struct {
	Type     config.DatabaseType `json:"type"`
	Host     string              `json:"host,omitempty"`
	Port     uint16              `json:"port,omitempty"`
	Username string              `json:"username,omitempty"`
	Name     string              `json:"name,omitempty"`
	LocalRef string              `json:"localRef,omitempty"`
	Password *config.SecretInput `json:"password,omitempty"`
}

func (input TargetInput) Prepare(root string) (config.DatabaseConfig, error) {
	target := config.NormalizeDatabaseConfig(config.DatabaseConfig{
		Type: input.Type, Host: strings.TrimSpace(input.Host), Port: input.Port,
		Username: strings.TrimSpace(input.Username), Name: strings.TrimSpace(input.Name),
		LocalRef: strings.TrimSpace(input.LocalRef),
	})
	if target.Type == config.DatabaseTypeSQLite {
		if input.Password != nil {
			return config.DatabaseConfig{}, ErrInvalidRequest
		}
	} else if target.Type.Valid() {
		vault, err := credential.NewVault(root)
		if err != nil {
			return config.DatabaseConfig{}, config.ErrSecretUnavailable
		}
		secret, err := config.ApplySecretChange(vault, config.StoredSecret{}, config.SecretChange{
			Operation: config.SecretReplace, Value: input.Password,
		}, config.SecretPolicy{Required: true, MaxLength: 4096}, time.Now().UTC())
		if err != nil {
			return config.DatabaseConfig{}, ErrInvalidRequest
		}
		target.Password = secret
	}
	if err := config.ValidateDatabaseConfig(target); err != nil {
		return config.DatabaseConfig{}, ErrInvalidRequest
	}
	return target, nil
}

func databaseIdentity(database config.DatabaseConfig) DatabaseIdentity {
	return DatabaseIdentity{Type: database.Type, Host: database.Host, Port: database.Port,
		Username: database.Username, Name: database.Name, LocalRef: database.LocalRef}
}

func buildTargetPlan(source, target config.DatabaseConfig) (Plan, error) {
	source = config.NormalizeDatabaseConfig(source)
	target = config.NormalizeDatabaseConfig(target)
	if err := config.ValidateDatabaseConfig(source); err != nil {
		return Plan{}, ErrSourceDatabase
	}
	if err := config.ValidateDatabaseConfig(target); err != nil {
		return Plan{}, ErrInvalidRequest
	}
	if target.Type == config.DatabaseTypeSQLite {
		if !sqliteTargetNamePattern.MatchString(target.LocalRef) {
			return Plan{}, ErrInvalidTargetName
		}
	} else if !targetNamePattern.MatchString(target.Name) {
		return Plan{}, ErrInvalidTargetName
	}
	if config.SameDatabaseLocation(source, target) {
		return Plan{}, ErrInvalidTargetName
	}
	return Plan{Source: databaseIdentity(source), Target: databaseIdentity(target), sourceConfig: source, targetConfig: target}, nil
}

// Preview 不创建目标库，只验证源库可读、目标服务可连接且名称未占用。
func (e *DatabaseExecutor) Preview(ctx context.Context, plan Plan) error {
	if e.portable != nil {
		return e.portable.Preview(ctx, plan)
	}
	return nil
}
