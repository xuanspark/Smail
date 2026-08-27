package setup

import (
	"sync"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/validation"
)

func GetDatabaseSettings(ctx *context.Context) (string, string, error) {
	configData, err := config.ReadConfig()
	if err != nil {
		return "", "", errors.Wrap(err)
	}
	if configData.DbType == "" && configData.DbDSN == "" {
		return config.DBTypeSQLite, config.ROOT_PATH + "./config/smail.db", nil
	}
	return configData.DbType, configData.DbDSN, nil
}

func GetAdminPassword(ctx *context.Context) (string, error) {
	users := []*models.User{}
	if err := db.Instance.Find(&users); err != nil {
		return "", errors.Wrap(err)
	}
	if len(users) > 0 {
		return users[0].Account, nil
	}
	return "", nil
}

var adminSetupMutex sync.Mutex

func SetAdminPassword(ctx *context.Context, account, pwd string) error {
	adminSetupMutex.Lock()
	defer adminSetupMutex.Unlock()

	account, err := validation.NormalizeAccount(account)
	if err != nil {
		return errors.Wrap(err)
	}
	count, err := db.Instance.Count(new(models.User))
	if err != nil {
		return errors.Wrap(err)
	}
	if count != 0 {
		return errors.New("administrator already exists")
	}
	user := models.User{Account: account, Name: "admin", IsAdmin: 1, SessionVersion: 1}
	if err := auth.SetPassword(&user, pwd); err != nil {
		return errors.Wrap(err)
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		return errors.Wrap(err)
	}
	return nil
}

func SetDatabaseSettings(ctx *context.Context, dbType, dbDSN string) error {
	configData, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	if !array.InArray(dbType, config.DBTypes) {
		return errors.New("dbtype error")
	}
	if dbDSN == "" {
		return errors.New("DSN error")
	}
	configData.DbType = dbType
	configData.DbDSN = dbDSN
	if err = config.WriteConfig(configData); err != nil {
		return errors.Wrap(err)
	}
	if err = config.Init(); err != nil {
		return errors.Wrap(err)
	}
	if err = db.Init(""); err != nil {
		return errors.Wrap(err)
	}
	return nil
}
