package session

import (
	"fmt"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"github.com/alexedwards/scs/mysqlstore"
	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"net/http"

	"time"
)

var Instance *scs.SessionManager

func Init() error {
	cfg := config.CurrentConfig()
	if cfg == nil {
		return fmt.Errorf("runtime configuration is unavailable")
	}
	Instance = scs.New()
	Instance.Lifetime = 7 * 24 * time.Hour
	Instance.IdleTimeout = 24 * time.Hour
	Instance.Cookie.HttpOnly = true
	Instance.Cookie.SameSite = http.SameSiteLaxMode
	Instance.Cookie.Secure = !config.DevelopmentMode
	// 使用db存储session数据，目前为了架构简单，
	// 暂不引入redis存储，如果日后性能存在瓶颈，可以将session迁移到redis

	switch cfg.DbType {
	case config.DBTypeMySQL:
		Instance.Store = mysqlstore.New(db.Instance.DB().DB)
	case config.DBTypeSQLite:
		Instance.Store = sqlite3store.New(db.Instance.DB().DB)
	case config.DBTypePostgres:
		Instance.Store = postgresstore.New(db.Instance.DB().DB)
	default:
		return fmt.Errorf("unsupported database type: %s", cfg.DbType)
	}

	return nil
}
