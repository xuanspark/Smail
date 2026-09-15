package db

import (
	stdcontext "context"
	"fmt"
	"os"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/models"
	requestcontext "git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"xorm.io/xorm"
)

var Instance *xorm.Engine

func Init(version string) error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return errors.New("runtime configuration is unavailable")
	}
	dsn := cfg.DbDSN
	var err error
	if cfg.DbType == config.DBTypeSQLite {
		dsn, err = sqliteDSNWithReliabilityOptions(dsn)
		if err != nil {
			return errors.Wrap(err)
		}
	}

	switch cfg.DbType {
	case "mysql":
		Instance, err = xorm.NewEngine("mysql", dsn)
	case "sqlite":
		Instance, err = xorm.NewEngine("sqlite", dsn)
	case "postgres":
		Instance, err = xorm.NewEngine("postgres", dsn)
	default:
		return errors.New("Database Type Error!")
	}
	if err != nil {
		log.Errorf("DB init Error! %s", err.Error())
		return errors.Wrap(err)
	}
	if cfg.DbType == config.DBTypeSQLite {
		Instance.SetMaxOpenConns(1)
		Instance.SetMaxIdleConns(1)
	} else {
		Instance.SetMaxOpenConns(100)
		Instance.SetMaxIdleConns(10)
	}

	Instance.SetConnMaxLifetime(30 * time.Minute)
	Instance.ShowSQL(cfg.LogLevel == "debug")
	dialect, err := migration.ParseDialect(cfg.DbType)
	if err != nil {
		return errors.Wrap(err)
	}
	if legacySyncEnabled(version) {
		// 先完成兼容期表结构引导，再由有版本迁移记录的迁移器执行数据迁移。
		// 新版业务迁移不得在此处继续增加无版本的结构变更。
		if err := syncTables(); err != nil {
			return errors.Wrap(err)
		}
	}
	report, err := (migration.Runner{
		DB:         Instance.DB().DB,
		Dialect:    dialect,
		Migrations: migration.DefaultMigrations(),
	}).Run(stdcontext.Background())
	if err != nil {
		return errors.Wrap(err)
	}
	log.Infof("当前数据库结构版本: %d", report.CurrentVersion)
	// 更新历史数据
	if err := fixHistoryData(); err != nil {
		return errors.Wrap(err)
	}

	// 在数据库中记录程序版本
	var v models.Version
	_, err = Instance.Get(&v)
	if err != nil {
		return errors.Wrap(err)
	}

	if version != "" && v.Info != version && version != "test" {
		v.Info = version
		if v.Id == 0 {
			if _, err := Instance.Insert(&v); err != nil {
				return errors.Wrap(err)
			}
		} else {
			if _, err := Instance.ID(v.Id).Cols("info").Update(&v); err != nil {
				return errors.Wrap(err)
			}
		}
	}

	//if config.Instance.LogLevel == "debug" {
	//	Instance.ShowSQL(true)
	//}

	return nil
}

func WithContext(ctx *requestcontext.Context, sql string) string {
	if ctx != nil {
		logId := ctx.GetValue(requestcontext.LogID)
		return fmt.Sprintf("/* %s */ %s", logId, sql)
	}
	return sql
}

func legacySyncEnabled(version string) bool {
	value := strings.TrimSpace(os.Getenv("SMAIL_ENABLE_LEGACY_SYNC2"))
	if value != "1" && !strings.EqualFold(value, "true") {
		return false
	}
	if version != "test" && !config.DevelopmentMode {
		log.Warn("生产环境已拒绝启用旧 Sync2 兼容模式")
		return false
	}
	return true
}

func syncTables() error {
	tables := []any{
		&models.User{},
		&models.Email{},
		&models.Group{},
		&models.Rule{},
		&models.Sessions{},
		&models.UserEmail{},
		&models.ExternalEmail{},
		&models.VerificationEvent{},
		&models.SecurityQuestion{},
		&models.RecoveryCode{},
		&models.WebAuthnCredential{},
		&models.ProtocolCredential{},
		&models.Version{},
		&models.UserContact{},
		&models.UserTrustedSender{},
	}
	for _, table := range tables {
		if err := Instance.Sync2(table); err != nil {
			return fmt.Errorf("sync database table %T: %w", table, err)
		}
	}
	return nil
}

func fixHistoryData() error {
	var ueNum int
	_, err := Instance.Table(&models.UserEmail{}).Select("count(1)").Get(&ueNum)
	if err != nil {
		return errors.Wrap(err)
	}
	if ueNum > 0 {
		return nil
	}

	// 只有一个管理员用户
	var user []models.User
	err = Instance.Table(&models.User{}).OrderBy("id asc").Find(&user)
	if err != nil {
		return errors.Wrap(err)
	}

	// 历史邮件只能归入唯一超级管理员，不能把仅存的普通账号自动提权。
	if len(user) != 1 || ValidateSuperAdmin(Instance) != nil {
		return nil
	}

	// 以前有邮件
	var emails []*models.Email
	err = Instance.Table(&models.Email{}).Select("id,status").OrderBy("id asc").Find(&emails)
	if err != nil {
		return errors.Wrap(err)
	}
	if len(emails) == 0 {
		return nil
	}

	log.Infof("Sync History Data！Please Wait！")

	// 把以前的邮件，全部分到管理员账号下面去
	for _, email := range emails {
		ue := models.UserEmail{
			UserID:  user[0].ID,
			EmailID: email.Id,
			Status:  email.Status,
		}
		_, err = Instance.Insert(&ue)
		if err != nil {
			return errors.Wrap(err)
		}
	}
	log.Infof("Sync History Data Finished. Num: %d", len(emails))
	return nil
}
