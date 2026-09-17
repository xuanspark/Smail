package res_init

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fmt"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/controllers"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/db/migration"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/listen/http_server"
	"git.pcux.cn/xuans/Smail/listen/imap_server"
	"git.pcux.cn/xuans/Smail/listen/pop3_server"
	"git.pcux.cn/xuans/Smail/listen/smtp_server"
	"git.pcux.cn/xuans/Smail/services/adminreset"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/dbmigration"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/services/outbox"
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/restart"
	"git.pcux.cn/xuans/Smail/services/setup/ssl"
	"git.pcux.cn/xuans/Smail/services/verification"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/signal"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	log "github.com/sirupsen/logrus"
	// 导入 HTTP 客户端，用于等待服务启动。
	"net/http"
)

var adminResetControl adminreset.ControlServer
var adminResetCancel context.CancelFunc
var adminResetServeErrors chan error
var verificationRuntime *verification.Runtime
var credentialService *credentials.Service
var webAuthnRuntimeService *webauthnservice.Service
var logRuntime *logstore.Runtime
var outboxRuntime *outbox.Runtime

func startConfigManager() error {
	root := filepath.Join(config.ROOT_PATH, "config")
	rootConfig := filepath.Join(root, "config.json")
	if _, err := os.Stat(rootConfig); err != nil {
		if os.IsNotExist(err) {
			config.SetRuntimeManager(nil)
			dbmigration.SetCurrent(nil)
			return nil
		}
		return err
	}
	partitionFound := false
	for _, section := range config.RequiredPartitionSections() {
		name, _ := config.FixedSectionFile(section)
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			partitionFound = true
			break
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if !partitionFound {
		// 未发现分区配置时继续使用旧版配置读取路径。
		config.SetRuntimeManager(nil)
		dbmigration.SetCurrent(nil)
		return nil
	}
	security := config.ConfigSecurity{}
	manager, err := config.NewConfigManagerWithSecurity(config.ROOT_PATH, config.ConfigLoaderOptions{Validation: config.RuntimeValidationOptions{
		HasSuperAdmin: true,
		Production:    !config.DevelopmentMode,
	}}, security)
	if err != nil {
		return fmt.Errorf("load partitioned configuration: %w", err)
	}
	config.SetRuntimeManager(manager)
	authorizedSwitch := func(ctx context.Context, expectedGeneration uint64, target config.DatabaseConfig, actor config.AuditActor) error {
		return manager.SwitchDatabase(ctx, expectedGeneration, target, actor)
	}
	factory, factoryErr := dbmigration.NewRuntimeSQLConnectionFactory(config.ROOT_PATH)
	if factoryErr != nil {
		return fmt.Errorf("initialize database migration: %w", factoryErr)
	}
	dbmigration.SetCurrent(
		dbmigration.NewService(
			dbmigration.NewPortableDatabaseExecutor(config.ROOT_PATH, factory, authorizedSwitch),
		),
	)

	return nil
}

func stopConfigManager() {
	logRuntime.Stop()
	logRuntime = nil
	dbmigration.Current().Close()
	config.SetRuntimeManager(nil)
	dbmigration.SetCurrent(nil)
}
func startPluginConfigMigration() error {
	hooks.SetPluginConfigService(nil)
	manager := config.CurrentManager()
	if manager == nil {
		return nil
	}
	vault, err := credential.NewVault(config.ROOT_PATH)
	if err != nil {
		return err
	}
	service, err := pluginservice.NewConfigService(db.Instance, vault)
	if err != nil {
		return err
	}
	if _, err = manager.MigrateLegacyPluginSecrets(context.Background(), service.ImportLegacySecrets); err != nil {
		return err
	}
	hooks.SetPluginConfigService(service)
	return nil
}
func Init(serverVersion string) error {
	cfg := config.CurrentConfig()
	if cfg == nil || !cfg.IsSetupReady() {
		if err := dirInit(); err != nil {
			return err
		}

		setupErrors := make(chan error, 1)
		go func() {
			setupErrors <- http_server.SetupStart()
		}()
		select {
		case <-signal.InitChan:
			http_server.SetupStop()
		case <-signal.StopChan:
			http_server.SetupStop()
			return nil
		case err := <-setupErrors:
			if err != nil {
				return fmt.Errorf("setup service failed: %w", err)
			}
			return fmt.Errorf("setup service stopped before initialization completed")
		}
	}

	for {
		if err := config.Init(); err != nil {
			return err
		}
		cfg = config.CurrentConfig()
		if cfg == nil {
			return fmt.Errorf("runtime configuration is unavailable")
		}
		// 等待 HTTP 服务就绪后再执行需要 HTTP 回调的 SSL 更新。
		// ssl.Update(false)

		if _, err := auth.DkimGen(); err != nil {
			return err
		}
		if err := parsemail.Init(); err != nil {
			return err
		}
		if err := db.Init(serverVersion); err != nil {
			return err
		}
		if err := db.ValidateSuperAdmin(db.Instance); err != nil {
			return err
		}
		dialect, err := migration.ParseDialect(cfg.DbType)
		if err != nil {
			return err
		}
		store, err := logstore.New(db.Instance.DB().DB, dialect)
		if err != nil {
			return err
		}
		logRuntime = logstore.Start(store)
		if err := startConfigManager(); err != nil {
			logRuntime.Stop()
			logRuntime = nil
			return err
		}
		if err := startPluginConfigMigration(); err != nil {
			log.WithError(err).WithFields(log.Fields{"action": "迁移插件凭据", "result": "failure"}).Warn("插件凭据迁移失败，受影响的插件保持禁用")
		}
		if err := session.Init(); err != nil {
			return err
		}
		if err := startWebAuthnRuntime(); err != nil {
			return err
		}
		if err := startVerificationRuntime(); err != nil {
			return err
		}
		if err := startAdminResetControl(); err != nil {
			stopServices()
			return err
		}
		serviceErrors := make(chan error, 9)
		adminResetErrors := adminResetServeErrors
		if adminResetErrors != nil {
			go func() {
				if err := <-adminResetErrors; err != nil {
					serviceErrors <- fmt.Errorf("administrator reset control failed: %w", err)
				}
			}()
		}
		hooks.Init(serverVersion)
		outboxRuntime, err = outbox.Start(db.Instance)
		if err != nil {
			stopServices()
			return fmt.Errorf("start persistent outbox: %w", err)
		}
		httpRun, httpsRun, err := http_server.PrepareWebServers()
		if err != nil {
			stopServices()
			return fmt.Errorf("prepare web services: %w", err)
		}
		mailSecurity := config.CurrentMailSecurity()
		// SMTP 25 用于服务器间投递，始终保留并支持机会性 STARTTLS。
		startService(serviceErrors, "SMTP", smtp_server.Start)
		if mailSecurity == config.MailSecurityBoth || mailSecurity == config.MailSecurityImplicitTLS {
			startService(serviceErrors, "SMTP TLS", smtp_server.StartWithTLS)
		}
		if mailSecurity == config.MailSecurityBoth || mailSecurity == config.MailSecuritySTARTTLS {
			startService(serviceErrors, "SMTP STARTTLS", smtp_server.StartWithTLSNew)
		}
		// http server start
		startService(serviceErrors, "HTTPS", httpsRun)
		startService(serviceErrors, "HTTP", httpRun)

		// 等待 HTTP 服务启动后再执行需要 HTTP 回调的 ACME 操作。
		if err := waitHTTPReady(serviceErrors); err != nil {
			stopServices()
			return err
		}

		// 更新 SSL 配置。
		ssl.Update(false)

		if mailSecurity == config.MailSecurityBoth || mailSecurity == config.MailSecuritySTARTTLS {
			startService(serviceErrors, "POP3 STLS", pop3_server.Start)
		}
		if mailSecurity == config.MailSecurityBoth || mailSecurity == config.MailSecurityImplicitTLS {
			startService(serviceErrors, "POP3 TLS", pop3_server.StartWithTls)
		}
		if mailSecurity == config.MailSecuritySTARTTLS {
			startService(serviceErrors, "IMAP STARTTLS", imap_server.Start)
		} else {
			startService(serviceErrors, "IMAP TLS", imap_server.StarTLS)
		}

		log.WithFields(log.Fields{"action": "启动邮箱监听", "object": "SMTP / IMAP / POP3", "result": "started"}).Info("邮箱监听任务已提交，具体监听结果由各服务报告")
		restart.SetCurrent(restart.NewSignalTrigger(signal.RestartChan))

		select {
		case err := <-serviceErrors:
			log.WithError(err).WithFields(log.Fields{"action": "运行邮箱服务", "result": "failure"}).Error("邮箱服务运行失败")
			stopServices()
			return err
		case <-signal.RestartChan:
			log.WithFields(log.Fields{"action": "重启服务", "result": "started"}).Info("已收到服务重启请求")
			if err := stopServices(); err != nil {
				return err
			}
		case <-signal.StopChan:
			log.WithFields(log.Fields{"action": "停止服务", "result": "started"}).Info("已收到服务停止请求")
			return stopServices()
		}
		log.WithFields(log.Fields{"action": "停止旧服务", "result": "success"}).Info("旧服务已停止，等待重新初始化")
		time.Sleep(5 * time.Second)

	}

}

func startService(serviceErrors chan<- error, name string, service func() error) {
	go func() {
		if err := service(); err != nil {
			serviceErrors <- fmt.Errorf("%s service failed: %w", name, err)
		}
	}()
}

func stopServices() error {
	restart.SetCurrent(nil)
	stopAdminResetControl()
	smtp_server.Stop()
	http_server.HttpsStop()
	http_server.HttpStop()
	pop3_server.Stop()
	imap_server.Stop()
	// 停止接收新请求后等待正在投递的任务写回，超时则保留数据库，避免提前关闭连接池。
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 25*time.Second)
	err := outboxRuntime.Stop(drainCtx)
	drainCancel()
	if err != nil {
		return fmt.Errorf("outbox did not drain; database retained: %w", err)
	}
	outboxRuntime = nil
	hooks.Stop()
	// 先关闭业务入口，再撤下安全服务及会话清理器，避免新请求读取停止中的依赖。
	stopVerificationRuntime()
	session.Stop()
	logRuntime.Stop()
	logRuntime = nil
	// 关闭连接池后还要等待借出的连接归还，不能提前解除迁移源锁。
	if db.Instance != nil {
		_ = db.Instance.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
		defer cancel()
		if err := dbmigration.WaitForDatabaseDrain(ctx, db.Instance.DB().DB); err != nil {
			return fmt.Errorf("database operations did not drain; migration lock retained: %w", err)
		}
	}
	stopConfigManager()
	return nil
}

func startVerificationRuntime() error {
	if verificationRuntime != nil {
		return nil
	}
	runtime, err := verification.NewRuntimeWithEventStore(0, db.Instance)
	if err != nil {
		return fmt.Errorf("start verification runtime: %w", err)
	}
	credential, err := credentials.NewService(db.Instance)
	if err != nil {
		runtime.Stop()
		return fmt.Errorf("start credential service: %w", err)
	}
	credential.SetVaultRoot(config.ROOT_PATH)
	vault, err := externalemail.NewVault(config.ROOT_PATH)
	if err != nil {
		runtime.Stop()
		return fmt.Errorf("start external email vault: %w", err)
	}
	external, err := externalemail.NewRepository(db.Instance, vault)
	if err != nil {
		runtime.Stop()
		return fmt.Errorf("start external email repository: %w", err)
	}
	delivery := newVerificationDelivery()
	service, err := verification.NewService(runtime.Store, delivery, runtime.Events)
	if err != nil {
		runtime.Stop()
		return fmt.Errorf("start verification service: %w", err)
	}
	service.SetRateLimitStore(runtime.RateLimits)
	recover, err := recovery.NewService(db.Instance, service, external, credential)
	if err != nil {
		runtime.Stop()
		return fmt.Errorf("start recovery service: %w", err)
	}
	recover.SetWebAuthnVerifier(webAuthnRuntimeService)
	recover.SetPublicLinkDelivery(newPublicRecoveryLinkDelivery())
	controllers.ConfigureSecurityRuntime(service, external, recover, credential)
	credentialService = credential
	verificationRuntime = runtime
	log.WithFields(log.Fields{"action": "初始化验证码服务", "result": "success"}).Info("安全验证码服务已启动，已配置中继时优先使用中继，否则使用项目自身邮件投递")
	return nil
}

func smtpSecurityMode(value string) (verification.SecurityMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(verification.SecurityStartTLS):
		return verification.SecurityStartTLS, nil
	case string(verification.SecurityImplicitTLS), "implicit_tls":
		return verification.SecurityImplicitTLS, nil
	case string(verification.SecurityPlain):
		return verification.SecurityPlain, nil
	default:
		return "", fmt.Errorf("不支持的 SMTP 安全模式: %s", value)
	}
}

func startWebAuthnRuntime() error {
	controllers.ConfigureWebAuthnRuntime(nil)
	webAuthnRuntimeService = nil
	manager := config.CurrentManager()
	if manager == nil {
		return nil
	}
	snapshot := manager.Snapshot()
	security, err := config.ResolveWebAuthnSecurity(snapshot.Root)
	if err != nil {
		return fmt.Errorf("加载安全密钥站点配置失败: %w", err)
	}
	service, err := webauthnservice.NewService(db.Instance, webauthnservice.Config{
		RPID:          security.WebAuthnRPID,
		RPDisplayName: security.WebAuthnRPDisplayName,
		RPOrigins:     append([]string(nil), security.WebAuthnRPOrigins...),
		VaultRoot:     config.ROOT_PATH,
	})
	if err != nil {
		return fmt.Errorf("start WebAuthn service: %w", err)
	}
	webAuthnRuntimeService = service
	controllers.ConfigureWebAuthnRuntime(service)
	log.WithFields(log.Fields{"action": "初始化通行密钥服务", "result": "success"}).Info("通行密钥服务已启动")
	return nil
}

func stopVerificationRuntime() {
	controllers.ClearSecurityRuntime()
	webAuthnRuntimeService = nil
	credentialService = nil
	if verificationRuntime == nil {
		return
	}
	verificationRuntime.Stop()
	verificationRuntime = nil
}

// startAdminResetControl 启动受保护的本机超级管理员重置通道。
func startAdminResetControl() error {
	development := adminResetDisabledForDevelopment(config.DevelopmentMode, os.Args[1:])
	path := adminreset.DefaultControlPath
	return startAdminResetControlWithListener(development, path, adminreset.ListenControl)
}

func startAdminResetControlWithListener(development bool, path string, listen func(string, []uint32) (adminreset.ControlServer, error)) error {
	if adminResetControl != nil {
		return nil
	}
	if development {
		log.WithFields(log.Fields{"action": "启动管理员恢复通道", "result": "skipped"}).Warn("开发模式：已停用本机超级管理员密码重置通道，后端无需为此提升权限；正式部署请移除开发参数")
		return nil
	}
	if path == "" {
		return fmt.Errorf("administrator reset control path is unavailable")
	}
	control, err := listen(path, []uint32{0})
	if err != nil {
		return fmt.Errorf("start administrator reset control: %w", err)
	}
	if control == nil {
		return fmt.Errorf("administrator reset control is unavailable")
	}
	adminResetControl = control
	ctx, cancel := context.WithCancel(context.Background())
	adminResetCancel = cancel
	serveErrors := make(chan error, 1)
	adminResetServeErrors = serveErrors
	go func() {
		err := control.Serve(ctx, func(password string) error {
			return adminreset.ResetSuperAdmin(db.Instance, password, controllers.InvalidateSecurityRuntimeUser)
		}, adminreset.NewSessionGuard(2*time.Minute))
		serveErrors <- err
		if err != nil {
			log.WithError(err).WithFields(log.Fields{"action": "运行管理员恢复通道", "result": "failure"}).Error("管理员恢复通道运行失败")
		}
	}()
	log.WithFields(log.Fields{"action": "启动管理员恢复通道", "result": "started"}).Info("管理员恢复通道已创建，开始处理本机请求")
	return nil
}

func stopAdminResetControl() {
	if adminResetCancel != nil {
		adminResetCancel()
		adminResetCancel = nil
	}
	if adminResetControl != nil {
		if err := adminResetControl.Close(); err != nil {
			log.WithError(err).WithFields(log.Fields{"action": "停止管理员恢复通道", "result": "failure"}).Warn("关闭管理员恢复通道失败")
		}
		adminResetControl = nil
		adminResetServeErrors = nil
	}
}

func dirInit() error {
	// 初始化与配置读写使用同一个运行根目录，不依赖启动时的工作目录。
	root := filepath.Join(config.ROOT_PATH, "data")
	for _, directory := range []string{filepath.Join(config.ROOT_PATH, "config"), root, runtimepath.DKIMDirectory(config.ROOT_PATH), runtimepath.SSLDirectory(config.ROOT_PATH)} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			return fmt.Errorf("创建运行配置目录失败: %w", err)
		}
		info, err := os.Lstat(directory)
		if err != nil {
			return fmt.Errorf("检查运行配置目录失败: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("运行配置目录不能是符号链接或普通文件")
		}
	}
	return nil
}

// 等待 HTTP 服务启动，最长约 90 秒。
func waitHTTPReady(serviceErrors <-chan error) error {
	port := config.CurrentHTTPConfig().Port
	url := fmt.Sprintf("http://127.0.0.1:%d/api/ping", port)

	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 90; i++ {
		resp, err := client.Get(url)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			log.WithFields(log.Fields{"action": "检查 HTTP 就绪状态", "result": "success"}).Infof("HTTP 服务健康检查通过：%s", url)
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		select {
		case err := <-serviceErrors:
			return err
		default:
		}
		time.Sleep(1 * time.Second)
	}
	log.WithFields(log.Fields{"action": "检查 HTTP 就绪状态", "result": "failure"}).Warn("HTTP 服务九十秒内未就绪，跳过即时证书更新")
	return nil
}
