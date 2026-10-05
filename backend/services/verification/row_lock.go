package verification

import (
	"fmt"

	"xorm.io/xorm"
)

// 使用参数化查询保留事务行锁，避免当前 Xorm 仅支持 MySQL 的行锁构造器。
func getVerificationRowForUpdate(session *xorm.Session, bean any, query string, args ...any) (bool, error) {
	statement, err := verificationLockQuery(session.Engine().DriverName(), query)
	if err != nil {
		return false, err
	}
	return session.SQL(statement, args...).Get(bean)
}

func verificationLockQuery(driver, query string) (string, error) {
	switch driver {
	case "postgres", "mysql":
		return query + " FOR UPDATE", nil
	case "sqlite", "sqlite3":
		return query, nil
	default:
		return "", fmt.Errorf("unsupported verification lock driver: %s", driver)
	}
}
