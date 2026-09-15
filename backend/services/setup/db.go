package setup

import (
	stdcontext "context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/password"
	"git.pcux.cn/xuans/Smail/utils/validation"
	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

type DatabaseSettings struct {
	Type     string
	DSN      string
	Mode     string
	User     string
	Host     string
	Port     string
	Password string
	Name     string
}

type databaseSetupError struct {
	Message string
	Cause   error
}

func (e *databaseSetupError) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *databaseSetupError) Unwrap() error {
	return e.Cause
}

func databaseError(message string, cause error) error {
	return &databaseSetupError{Message: message, Cause: cause}
}

func databaseInitializationError(err error) error {
	if stderrors.Is(err, migration.ErrChecksumMismatch) || stderrors.Is(err, migration.ErrUnknownAppliedMigration) {
		return databaseError("数据库连接成功，但检测到历史迁移记录与当前版本不兼容，请先备份数据库后再执行兼容升级", err)
	}
	return errors.Wrap(err)
}

func DatabaseSetupErrorMessage(err error) string {
	var setupErr *databaseSetupError
	if errors.As(err, &setupErr) {
		return setupErr.Message
	}
	return ""
}

func GetDatabaseSettings(ctx *context.Context) (string, string, error) {
	configData, err := config.ReadConfig()
	if err != nil {
		return "", "", errors.Wrap(err)
	}
	if configData.DbType == "" && configData.DbDSN == "" {
		return config.DBTypeSQLite, config.ROOT_PATH + "./data/smail.db", nil
	}
	return configData.DbType, configData.DbDSN, nil
}

func GetDatabaseSettingsDetail(ctx *context.Context) (map[string]string, error) {
	dbType, dbDSN, err := GetDatabaseSettings(ctx)
	if err != nil {
		return nil, err
	}

	result := map[string]string{
		"db_type": dbType,
		"db_dsn":  dbDSN,
		"db_mode": "dsn",
	}
	if dbType == config.DBTypeSQLite {
		if strings.TrimSpace(result["db_dsn"]) == "" {
			result["db_dsn"] = config.ROOT_PATH + "./data/smail.db"
		}
		return result, nil
	}
	if dbType == config.DBTypePostgres {
		parsed, ok := parsePostgresURL(dbDSN)
		if !ok {
			return result, nil
		}
		host, port := splitDatabaseAddress(parsed.Host)
		result["db_mode"] = "fields"
		result["db_dsn"] = ""
		result["db_user"] = parsed.User.Username()
		result["db_host"] = host
		result["db_port"] = port
		result["db_name"] = strings.TrimPrefix(parsed.Path, "/")
		return result, nil
	}
	if dbType != config.DBTypeMySQL {
		return result, nil
	}

	parsed, err := mysql.ParseDSN(dbDSN)
	if err != nil {
		return map[string]string{
			"db_type": dbType,
			"db_dsn":  "",
			"db_mode": "fields",
			"db_host": "127.0.0.1",
			"db_port": "3306",
			"db_name": "smail",
		}, nil
	}
	host, port := splitMySQLAddress(parsed.Addr)
	result["db_mode"] = "fields"
	// MySQL 密码不能通过初始化接口返回，页面重新提交时由后端尝试复用原密码。
	result["db_dsn"] = ""
	result["db_user"] = parsed.User
	result["db_host"] = host
	result["db_port"] = port
	result["db_name"] = parsed.DBName
	return result, nil
}

func GetAdminPassword(ctx *context.Context) (string, error) {
	if db.Instance == nil {
		return "", databaseError("数据库尚未初始化，请先完成数据库设置", nil)
	}
	users := []*models.User{}
	if err := db.Instance.Where(models.SuperAdminPredicate).Limit(2).Find(&users); err != nil {
		return "", errors.Wrap(err)
	}
	if len(users) > 1 {
		return "", db.ErrSuperAdminInvariant
	}
	if len(users) == 1 {
		return users[0].Account, nil
	}
	return "", nil
}

var adminSetupMutex sync.Mutex

func SetAdminPassword(ctx *context.Context, account, pwd string) error {
	adminSetupMutex.Lock()
	defer adminSetupMutex.Unlock()
	if db.Instance == nil {
		return databaseError("数据库尚未初始化，请先完成数据库设置", nil)
	}

	if err := validateAdminPassword(pwd); err != nil {
		return err
	}

	account, err := validation.NormalizeAccount(account)
	if err != nil {
		return errors.Wrap(err)
	}
	user := models.User{Account: account, Name: "admin", SessionVersion: 1, SecurityVersion: 1}
	user.ApplyRole(access.RoleSuperAdmin)
	if err := auth.SetPassword(&user, pwd); err != nil {
		return errors.Wrap(err)
	}
	tx := db.Instance.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		return errors.Wrap(err)
	}
	defer tx.Rollback()
	// 锁定必然存在的首条迁移记录，跨进程串行化初始化；三种数据库均支持此写锁。
	// 先获得写锁再读取用户数，避免两个初始化进程同时看到空库。
	if _, err := tx.Exec("UPDATE schema_migration SET execution_ms = execution_ms WHERE version = 1"); err != nil {
		return errors.Wrap(err)
	}
	lockRows, err := tx.QueryString("SELECT version FROM schema_migration WHERE version = 1")
	if err != nil {
		return errors.Wrap(err)
	}
	if len(lockRows) != 1 {
		return databaseError("数据库迁移记录不完整，不能初始化超级管理员", nil)
	}
	count, err := tx.Count(new(models.User))
	if err != nil {
		return errors.Wrap(err)
	}
	if count != 0 {
		return databaseError("账号已初始化，不能再次创建超级管理员", nil)
	}
	if _, err := tx.Insert(&user); err != nil {
		return errors.Wrap(err)
	}
	return tx.Commit()
}

func validateAdminPassword(pwd string) error {
	if err := password.ValidateUserPassword(pwd); err != nil {

		return databaseError("管理员密码长度必须为 8 到 12 个字节", nil)
	}
	return nil
}

func SetDatabaseSettings(ctx *context.Context, dbType, dbDSN string) error {
	return SetDatabaseSettingsWithOptions(ctx, DatabaseSettings{
		Type: dbType,
		DSN:  dbDSN,
	})
}

var databaseSetupMutex sync.Mutex

func SetDatabaseSettingsWithOptions(ctx *context.Context, settings DatabaseSettings) error {
	databaseSetupMutex.Lock()
	defer databaseSetupMutex.Unlock()

	configData, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	dbType := strings.TrimSpace(settings.Type)
	if !array.InArray(dbType, config.DBTypes) {
		return errors.New("dbtype error")
	}

	// 兼容未携带模式字段的旧调用：只要提交了结构化数据库字段，就按字段模式处理。
	if strings.TrimSpace(settings.Mode) == "" && (dbType == config.DBTypeMySQL || dbType == config.DBTypePostgres) {
		settings.Mode = "fields"
	}
	dbDSN := strings.TrimSpace(settings.DSN)
	if settings.Mode == "fields" {
		switch dbType {
		case config.DBTypeMySQL:
			dbDSN, err = buildMySQLDSN(settings, configData.DbDSN)
		case config.DBTypePostgres:
			dbDSN, err = buildPostgresDSN(settings, configData.DbDSN)
		}
		if err != nil {
			return databaseError(strings.SplitN(err.Error(), " at ", 2)[0], err)
		}
	}
	if dbType == config.DBTypeSQLite {
		if dbDSN == "" {
			dbDSN = config.ROOT_PATH + "./data/smail.db"
		}
		if err = prepareSQLiteDatabase(dbDSN); err != nil {
			return databaseError("准备 SQLite 数据文件失败", err)
		}
	}
	if dbDSN == "" {
		return errors.New("DSN error")
	}
	switch dbType {
	case config.DBTypeMySQL:
		if err = ensureMySQLDatabase(dbDSN); err != nil {
			return err
		}
	case config.DBTypePostgres:
		if err = ensurePostgresDatabase(dbDSN); err != nil {
			return err
		}
	}

	previousConfig := *configData
	candidateConfig := *configData
	markSetupInProgress(&candidateConfig)
	candidateConfig.DbType = dbType
	candidateConfig.DbDSN = dbDSN
	if err = config.WriteConfig(&candidateConfig); err != nil {
		return errors.Wrap(err)
	}
	if err = config.Init(); err != nil {
		restoreDatabaseConfig(&previousConfig)
		return errors.Wrap(err)
	}
	closeDatabaseInstance()
	if err = db.Init(""); err != nil {
		restoreDatabaseConfig(&previousConfig)
		return databaseInitializationError(err)
	}
	return nil
}

func markSetupInProgress(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.IsInit = false
	if !cfg.SetupState.Valid() {
		cfg.SetupState = config.SetupStateBootstrap
	}
}

func closeDatabaseInstance() {
	if db.Instance == nil {
		return
	}
	_ = db.Instance.Close()
	db.Instance = nil
}

func buildPostgresDSN(settings DatabaseSettings, previousDSN string) (string, error) {
	user := strings.TrimSpace(settings.User)
	if user == "" {
		return "", errors.New("PostgreSQL 用户不能为空")
	}
	if !validDatabaseField(user, 128) {
		return "", errors.New("PostgreSQL 用户格式无效")
	}
	host := strings.TrimSpace(settings.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	host = normalizeDatabaseHost(host)
	if !validDatabaseHost(host) {
		return "", errors.New("PostgreSQL 主机地址格式无效")
	}
	portText := strings.TrimSpace(settings.Port)
	if portText == "" {
		portText = "5432"
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("PostgreSQL 端口必须是 1 到 65535 之间的数字")
	}
	databaseName := strings.TrimSpace(settings.Name)
	if databaseName == "" {
		databaseName = "smail"
	}
	if !validPostgresIdentifier(databaseName) {
		return "", errors.New("数据库名只能包含字母、数字和下划线")
	}

	password := settings.Password
	previous, previousOK := parsePostgresURL(previousDSN)
	if password == "" && previousOK && postgresSettingsMatch(previous, user, host, strconv.Itoa(port), databaseName) {
		if previousPassword, ok := previous.User.Password(); ok {
			password = previousPassword
		}
	}
	query := url.Values{}
	if previousOK && previous.RawQuery != "" {
		query = previous.Query()
	}
	if query.Get("sslmode") == "" {
		query.Set("sslmode", "disable")
	}
	parsed := &url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + databaseName,
		RawQuery: query.Encode(),
	}
	return parsed.String(), nil
}

func parsePostgresURL(dsn string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" || parsed.User == nil {
		return nil, false
	}
	name := strings.TrimPrefix(parsed.Path, "/")
	return parsed, name != ""
}

func postgresSettingsMatch(parsed *url.URL, user, host, port, name string) bool {
	parsedHost, parsedPort := splitDatabaseAddress(parsed.Host)
	return parsed.User.Username() == user && parsedHost == host && parsedPort == port && strings.TrimPrefix(parsed.Path, "/") == name
}

func splitDatabaseAddress(address string) (string, string) {
	address = strings.TrimSpace(address)
	if host, port, err := net.SplitHostPort(address); err == nil {
		return strings.Trim(host, "[]"), port
	}
	if address == "" {
		return "127.0.0.1", "5432"
	}
	return strings.Trim(address, "[]"), "5432"
}

func normalizeDatabaseHost(host string) string {
	if len(host) >= 2 && strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

func validPostgresIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 63 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func prepareSQLiteDatabase(dsn string) error {
	dsn = strings.TrimSpace(dsn)
	if dsn == ":memory:" || strings.HasPrefix(dsn, "file::memory:") {
		return nil
	}
	path := dsn
	if strings.HasPrefix(path, "file:") {
		path = strings.TrimPrefix(path, "file:")
		path = strings.SplitN(path, "?", 2)[0]
	}
	if path == "" || path == ":memory:" {
		return nil
	}
	directory := filepath.Dir(path)
	if directory == "." || directory == "" {
		return nil
	}
	return os.MkdirAll(directory, 0750)
}

func buildMySQLDSN(settings DatabaseSettings, previousDSN string) (string, error) {
	user := strings.TrimSpace(settings.User)
	if user == "" {
		return "", errors.New("MySQL 用户不能为空")
	}
	if !validDatabaseField(user, 128) {
		return "", errors.New("MySQL 用户格式无效")
	}
	host := strings.TrimSpace(settings.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	host = normalizeMySQLHost(host)
	if !validDatabaseHost(host) {
		return "", errors.New("MySQL 主机地址格式无效")
	}
	portText := strings.TrimSpace(settings.Port)
	if portText == "" {
		portText = "3306"
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("MySQL 端口必须是 1 到 65535 之间的数字")
	}
	databaseName := strings.TrimSpace(settings.Name)
	if databaseName == "" {
		databaseName = "smail"
	}
	if !validMySQLIdentifier(databaseName) {
		return "", errors.New("数据库名只能包含字母、数字和下划线")
	}

	password := settings.Password
	if password == "" {
		password = previousMySQLPassword(previousDSN, user, host, strconv.Itoa(port), databaseName)
	}
	dsnConfig := mysql.NewConfig()
	dsnConfig.User = user
	dsnConfig.Passwd = password
	dsnConfig.Net = "tcp"
	dsnConfig.Addr = net.JoinHostPort(host, strconv.Itoa(port))
	dsnConfig.DBName = databaseName
	dsnConfig.ParseTime = true
	// 使用跨平台稳定的时区，避免 Windows 交叉编译后的 CST 在 Linux 上无法解析。
	dsnConfig.Loc = time.UTC
	dsnConfig.Params = map[string]string{"charset": "utf8mb4"}
	return dsnConfig.FormatDSN(), nil
}

func previousMySQLPassword(previousDSN, user, host, port, databaseName string) string {
	previous, err := mysql.ParseDSN(previousDSN)
	if err != nil || previous.User != user || previous.DBName != databaseName {
		return ""
	}
	previousHost, previousPort := splitMySQLAddress(previous.Addr)
	if previousHost != host || previousPort != port {
		return ""
	}
	return previous.Passwd
}

func splitMySQLAddress(address string) (string, string) {
	address = strings.TrimSpace(address)
	if host, port, err := net.SplitHostPort(address); err == nil {
		return strings.Trim(host, "[]"), port
	}
	if address == "" {
		return "127.0.0.1", "3306"
	}
	return strings.Trim(address, "[]"), "3306"
}

func normalizeMySQLHost(host string) string {
	host = strings.TrimSpace(host)
	if len(host) >= 2 && strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host[1 : len(host)-1]
	}
	return host
}

func validDatabaseField(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength {
		return false
	}
	for _, char := range value {
		if char == '\r' || char == '\n' || char == '\t' || char == ' ' {
			return false
		}
	}
	return true
}

func validDatabaseHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	if strings.ContainsAny(host, "\r\n\t /\\?@#") {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if strings.Contains(host, ":") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func validMySQLIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

var openMySQLSetupDatabase = func(dsn string) (setupDatabase, error) {
	return sql.Open("mysql", dsn)
}

var openPostgresSetupDatabase = func(dsn string) (setupDatabase, error) {
	return sql.Open("postgres", dsn)
}

func ensurePostgresDatabase(dsn string) error {
	parsed, ok := parsePostgresURL(dsn)
	if !ok {
		return nil
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !validPostgresIdentifier(databaseName) {
		return databaseError("PostgreSQL 数据库名无效", nil)
	}

	requestContext, cancel := stdcontext.WithTimeout(stdcontext.Background(), 15*time.Second)
	defer cancel()
	target, err := openPostgresSetupDatabase(dsn)
	if err != nil {
		return databaseError("连接 PostgreSQL 数据库失败", err)
	}
	defer func() { _ = target.Close() }()
	if err = target.PingContext(requestContext); err == nil {
		return nil
	} else {
		var postgresErr *pq.Error
		if !stderrors.As(err, &postgresErr) || string(postgresErr.Code) != "3D000" {
			return databaseError("连接 PostgreSQL 数据库失败，请检查用户、密码、主机、端口和数据库名", err)
		}
	}

	adminURL := *parsed
	adminURL.Path = "/postgres"
	adminURL.RawPath = ""
	admin, err := openPostgresSetupDatabase(adminURL.String())
	if err != nil {
		return databaseError("连接 PostgreSQL 服务器失败", err)
	}
	defer func() { _ = admin.Close() }()
	if err = admin.PingContext(requestContext); err != nil {
		return databaseError("连接 PostgreSQL 服务器失败，请检查用户、密码、主机和端口", err)
	}
	query := `CREATE DATABASE "` + databaseName + `"`
	if _, err = admin.ExecContext(requestContext, query); err != nil {
		var postgresErr *pq.Error
		if !stderrors.As(err, &postgresErr) || string(postgresErr.Code) != "42P04" {
			return databaseError("自动创建 PostgreSQL 数据库失败，请确认当前用户具有建库权限", err)
		}
	}
	if err = target.PingContext(requestContext); err != nil {
		return databaseError("PostgreSQL 数据库创建后仍无法连接", err)
	}
	return nil
}

type setupDatabase interface {
	PingContext(stdcontext.Context) error
	ExecContext(stdcontext.Context, string, ...any) (sql.Result, error)
	Close() error
}

func ensureMySQLDatabase(dsn string) error {
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		return databaseError("MySQL 连接信息格式错误", err)
	}
	if parsed.DBName == "" || !validMySQLIdentifier(parsed.DBName) {
		return databaseError("MySQL 数据库名无效", nil)
	}

	requestContext, cancel := stdcontext.WithTimeout(stdcontext.Background(), 15*time.Second)
	defer cancel()

	target, err := openMySQLSetupDatabase(dsn)
	if err != nil {
		return databaseError("连接 MySQL 数据库失败", err)
	}
	defer func() { _ = target.Close() }()
	if err = target.PingContext(requestContext); err == nil {
		return nil
	} else {
		var mysqlErr *mysql.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1049 {
			return databaseError("连接 MySQL 数据库失败，请检查用户、密码、主机、端口和数据库名", err)
		}
	}

	adminConfig := parsed.Clone()
	adminConfig.DBName = ""
	admin, err := openMySQLSetupDatabase(adminConfig.FormatDSN())
	if err != nil {
		return databaseError("连接 MySQL 服务器失败", err)
	}
	defer func() { _ = admin.Close() }()
	if err = admin.PingContext(requestContext); err != nil {
		return databaseError("连接 MySQL 服务器失败，请检查用户、密码、主机和端口", err)
	}
	query := "CREATE DATABASE IF NOT EXISTS `" + parsed.DBName + "` DEFAULT CHARACTER SET utf8mb4"
	if _, err = admin.ExecContext(requestContext, query); err != nil {
		return databaseError("自动创建 MySQL 数据库失败，请确认当前用户具有建库权限", err)
	}
	if err = target.PingContext(requestContext); err != nil {
		return databaseError("MySQL 数据库创建后仍无法连接", err)
	}
	return nil
}

func restoreDatabaseConfig(previous *config.Config) {
	if previous == nil {
		return
	}
	closeDatabaseInstance()
	if err := config.WriteConfig(previous); err != nil {
		return
	}
	_ = config.Init()
	if previous.DbType != "" && previous.DbDSN != "" {
		_ = db.Init("")
	}
}
