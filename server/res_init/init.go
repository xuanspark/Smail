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
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks"
	"git.pcux.cn/xuans/Smail/listen/http_server"
	"git.pcux.cn/xuans/Smail/listen/imap_server"
	"git.pcux.cn/xuans/Smail/listen/pop3_server"
	"git.pcux.cn/xuans/Smail/listen/smtp_server"
	"git.pcux.cn/xuans/Smail/services/adminreset"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/services/externalemail"
	"git.pcux.cn/xuans/Smail/services/recovery"
	"git.pcux.cn/xuans/Smail/services/setup/ssl"
	"git.pcux.cn/xuans/Smail/services/verification"
	webauthnservice "git.pcux.cn/xuans/Smail/services/webauthn"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/signal"
	"git.pcux.cn/xuans/Smail/utils/file"
	log "github.com/sirupsen/logrus"
	// 闁哄倹婢橀·鍐晬濞嗙睒TP 閻忓繗浜崡搴ㄥ箳閵忥紕銈?
	"net/http"
)

var adminResetControl adminreset.ControlServer
var adminResetCancel context.CancelFunc
var verificationRuntime *verification.Runtime
var credentialService *credentials.Service
var webAuthnRuntimeService *webauthnservice.Service

func startConfigManager() error {
	root := filepath.Join(config.ROOT_PATH, "config")
	rootConfig := filepath.Join(root, "config.json")
	if _, err := os.Stat(rootConfig); err != nil {
		if os.IsNotExist(err) {
			config.SetRuntimeManager(nil)
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
		// 闁哄唲鍛暭闁告娲橀弸鍐╃閸洖甯崇紓鍐惧枙缁盯鎮介崡鐐叉倯閻庡湱顢婄换宥囨偘鐏炵偓顦ч柟浣冨劵濞村洭鏁嶇仦鑲╃憹闁艰櫕鍨濋崥澶愭焻?V2 缂佺媴绱曢幃濠囧闯閵婏絺鍋?		config.SetRuntimeManager(nil)
		return nil
	}
	manager, err := config.NewConfigManager(root, config.ConfigLoaderOptions{Validation: config.RuntimeValidationOptions{
		HasSuperAdmin: true,
		Production:    !config.DevelopmentMode,
	}})
	if err != nil {
		return fmt.Errorf("load partitioned configuration: %w", err)
	}
	config.SetRuntimeManager(manager)
	return nil
}

func stopConfigManager() {
	config.SetRuntimeManager(nil)
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
		// 缂佸顭峰▍搴ㄦ晬濮橆剚鍎欓柛鏂诲妼婢х姷绮╃€ｎ亜绁柡鍥х摠閺屽﹦鎷犳担宄板闁汇劌瀚惃鐔兼偨椤帞绀夐梺顒€鐏濋崢?HTTP 闁哄牜浜滈幆搴ㄥ礉閵婏附顦ч悷娆欑畱瑜?ACME
		// ssl.Update(false)

		if err := parsemail.Init(); err != nil {
			return err
		}
		if err := db.Init(serverVersion); err != nil {
			return err
		}
		if err := startConfigManager(); err != nil {
			return err
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
		startAdminResetControl()
		hooks.Init(serverVersion)
		serviceErrors := make(chan error, 8)
		// smtp server start
		startService(serviceErrors, "SMTP", smtp_server.Start)
		startService(serviceErrors, "SMTP TLS", smtp_server.StartWithTLS)
		startService(serviceErrors, "SMTP STARTTLS", smtp_server.StartWithTLSNew)
		// http server start
		startService(serviceErrors, "HTTPS", http_server.HttpsStart)
		startService(serviceErrors, "HTTP", http_server.HttpStart)

		// 缂佹稑顦欢?HTTP 闁哄牆绉存慨鐔轰焊鏉堚晛宕曢柛姘閸熲偓闁圭瑳鍡╂斀閻犲洣妞掗崝鐔肺涢埀顒勫蓟?缂備緡鍘藉﹢锟犳晬瀹€鍕級闁?ACME 濡ょ姴鐭侀惁澶嬪緞鏉堫偉袝
		if err := waitHTTPReady(serviceErrors); err != nil {
			stopServices()
			return err
		}

		// 闁告凹鍨版慨鈺呭触鎼淬埄姊鹃柡灞诲劙缁旀挳鏌嗗鍫㈡濞戞棑璁ｇ槐娆撴閻愬搫娅㈤柛姘煎灡鑶╃€殿喖楠忕槐?
		ssl.Update(false)

		// pop3 server start
		startService(serviceErrors, "POP3", pop3_server.Start)
		startService(serviceErrors, "POP3 TLS", pop3_server.StartWithTls)
		// imap server start
		startService(serviceErrors, "IMAP TLS", imap_server.StarTLS)

		log.Info("管理员重置通道已启动")

		select {
		case err := <-serviceErrors:
			log.WithError(err).Error("管理员重置通道启动失败")
			stopServices()
			return err
		case <-signal.RestartChan:
			log.Infof("Server Restart!")
			stopServices()
		case <-signal.StopChan:
			log.Infof("Server Stop!")
			stopServices()
			return nil
		}
		log.Infof("Server Stop Success!")
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

func stopServices() {
	stopConfigManager()
	stopAdminResetControl()
	stopVerificationRuntime()
	smtp_server.Stop()
	http_server.HttpsStop()
	http_server.HttpStop()
	pop3_server.Stop()
	imap_server.Stop()
	hooks.Stop()
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
	cfg := config.CurrentConfig()
	if cfg == nil {
		runtime.Stop()
		return fmt.Errorf("runtime configuration is unavailable")
	}
	externalRecoveryEnabled := cfg.ExternalRecoveryEnabled
	if manager := config.CurrentManager(); manager != nil {
		runtimeConfig := manager.Snapshot()
		externalRecoveryEnabled = runtimeConfig.Email.Policy.ExternalRecoveryEnabled
	}
	if !cfg.VerificationEnabled && !externalRecoveryEnabled {
		recover, recoverErr := recovery.NewService(db.Instance, nil, external, credential)
		if recoverErr != nil {
			runtime.Stop()
			return fmt.Errorf("start recovery service: %w", recoverErr)
		}
		recover.SetWebAuthnVerifier(webAuthnRuntimeService)
		controllers.ConfigureSecurityRuntime(nil, external, recover, credential)
		credentialService = credential
		verificationRuntime = runtime
		log.Info("邮箱验证码服务已禁用")
		return nil
	}
	security, err := smtpSecurityMode(cfg.SMTPSecurity)
	if err != nil {
		runtime.Stop()
		return err
	}
	delivery, err := verification.NewSMTPDelivery(verification.SMTPSettings{
		Host:       cfg.SMTPHost,
		Port:       cfg.SMTPPort,
		Username:   cfg.SMTPUsername,
		Password:   cfg.SMTPPassword,
		Security:   security,
		From:       cfg.SMTPFrom,
		FromDomain: cfg.SMTPFromDomain,
	})
	if err != nil {
		runtime.Stop()
		return fmt.Errorf("SMTP 濡ょ姴鐭侀惁澶愭煀瀹ュ洨鏋傞柡鍐У閺? %w", err)
	}
	service, err := verification.NewService(runtime.Store, delivery.Delivery(), runtime.Events)
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
	controllers.ConfigureSecurityRuntime(service, external, recover, credential)
	credentialService = credential
	verificationRuntime = runtime
	log.Info("邮箱验证码服务已启动")
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
		return "", fmt.Errorf("SMTP 閻庣懓顦崣蹇撐熼垾宕囩濞戞挸绉甸弫顕€骞? %s", value)
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
	security := snapshot.Root.Security
	if !security.WebAuthnEnabled {
		return nil
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
	log.Info("WebAuthn service started")
	return nil
}

func stopVerificationRuntime() {
	controllers.ConfigureWebAuthnRuntime(nil)
	webAuthnRuntimeService = nil
	if verificationRuntime == nil {
		return
	}
	verificationRuntime.Stop()
	verificationRuntime = nil
	credentialService = nil
}

// startAdminResetControl 闁告瑯浜滃﹢顏呯▔閼姐倐鏌ら幖鏉戠箰閸炴挳宕ラ姘楅柡鍫墯濠р偓闂侇喓鍔庣拋鑼博椤栫偛娅㈢紓鍐惧櫍閳ь剚宀告禍楣冨Υ?
func startAdminResetControl() {
	if adminResetControl != nil {
		return
	}
	path := adminreset.DefaultControlPath
	if config.DevelopmentMode {
		if candidate := os.Getenv("SMAIL_ADMIN_CONTROL_PATH"); candidate != "" {
			path = candidate
		}
	}
	if path == "" {
		log.Warn("管理员重置通道路径未配置，跳过启动")
		return
	}
	control, err := adminreset.ListenControl(path, []uint32{0})
	if err != nil {
		log.WithError(err).Error("管理员重置通道启动失败")
		return
	}
	adminResetControl = control
	ctx, cancel := context.WithCancel(context.Background())
	adminResetCancel = cancel
	go func() {
		err := control.Serve(ctx, func(password string) error {
			return adminreset.ResetSuperAdmin(db.Instance, password, controllers.InvalidateSecurityRuntimeUser)
		}, adminreset.NewSessionGuard(2*time.Minute))
		if err != nil {
			log.WithError(err).Error("管理员重置通道启动失败")
		}
	}()
	log.Info("管理员重置通道已启动")
}

func stopAdminResetControl() {
	if adminResetCancel != nil {
		adminResetCancel()
		adminResetCancel = nil
	}
	if adminResetControl != nil {
		if err := adminResetControl.Close(); err != nil {
			log.WithError(err).Warn("闁稿繑濞婂Λ瀵告惥閸涱垶鐛撶紒鐙呯磿閹﹪宕ㄥΟ缁樻嫳闁哄牊妞介崳鍝ョ磾椤曗偓閳ь剚宀告禍鐐緞鏉堫偉袝")
		}
		adminResetControl = nil
	}
}

func dirInit() error {
	if !file.PathExist("./config") {
		err := os.MkdirAll("./config", 0700)
		if err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
	}

	if !file.PathExist("./config/dkim") {
		err := os.MkdirAll("./config/dkim", 0700)
		if err != nil {
			return fmt.Errorf("create DKIM directory: %w", err)
		}
	}

	if !file.PathExist("./config/ssl") {
		err := os.MkdirAll("./config/ssl", 0700)
		if err != nil {
			return fmt.Errorf("create SSL directory: %w", err)
		}
	}
	return nil
}

// 闁哄倹婢橀·鍐晬濞嗙睒TP 閻忓繗浜崡搴ㄥ箳閵忥紕銈撮柨娑樼墛濞撹埖寰勫杈╂惣鐎?~90 缂佸甯槐?
func waitHTTPReady(serviceErrors <-chan error) error {
	port := 80
	if cfg := config.CurrentConfig(); cfg != nil && cfg.HttpPort > 0 {
		port = cfg.HttpPort
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/ping", port)

	client := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 90; i++ {
		resp, err := client.Get(url)
		if err == nil && resp != nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			log.Infof("HTTP ready: %s", url)
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
	log.Warnf("HTTP not ready after 90s, skipping immediate SSL update")
	return nil
}
