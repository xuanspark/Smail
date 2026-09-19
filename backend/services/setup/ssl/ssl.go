package ssl

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	stderrors "errors"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/services/setup"
	"git.pcux.cn/xuans/Smail/utils/async"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/dns01"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"
)

type MyUser struct {
	Email        string
	Registration *registration.Resource
	key          crypto.PrivateKey
}

type userCertificateError struct {
	message string
}

func (e *userCertificateError) Error() string {
	return e.message
}

func newUserCertificateError(message string) error {
	return &userCertificateError{message: message}
}

// UserCertificateErrorMessage 只返回可以安全展示给初始化页面的证书错误。
func UserCertificateErrorMessage(err error) string {
	var certificateError *userCertificateError
	if stderrors.As(err, &certificateError) {
		return certificateError.message
	}
	return ""
}

func normalizeUserCertificateImportError(err error) error {
	switch {
	case stderrors.Is(err, config.ErrSSLCertificateMismatch):
		return newUserCertificateError("SSL 证书与私钥不匹配")
	case stderrors.Is(err, config.ErrSSLCertificateDomain):
		return newUserCertificateError("SSL 证书与已配置域名不匹配，请检查主域名、Web 域名和附加域名")
	case stderrors.Is(err, config.ErrSSLCertificateExpired):
		return newUserCertificateError("SSL 证书无效、尚未生效或已经过期")
	case stderrors.Is(err, config.ErrSSLPrivateKeyPermission):
		return newUserCertificateError("SSL 私钥权限过于宽松，请限制为仅 Smail 运行用户可读取")
	case stderrors.Is(err, config.ErrInvalidSSLMaterial), stderrors.Is(err, config.ErrUnsafeSSLFile):
		return newUserCertificateError("SSL 证书或私钥文件无效")
	default:
		return err
	}
}

func (u *MyUser) GetEmail() string {
	return u.Email
}
func (u MyUser) GetRegistration() *registration.Resource {
	return u.Registration
}
func (u *MyUser) GetPrivateKey() crypto.PrivateKey {
	return u.key
}

func GetSSL() (string, error) {
	cfg, err := config.ReadConfig()
	if err != nil {
		return "", errors.Wrap(err)
	}
	if cfg.UsesProxyHTTPS() {
		return config.SSLTypeUser, nil
	}
	if cfg.SSLType == "" && cfg.SSL.Management == "" && cfg.SSL.Mode == "" {
		return config.SSLTypeAutoHTTP, nil
	}

	return cfg.EffectiveSSLType(), nil
}

func SetSSL(sslType, priKey, crtKey string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	expected := *cfg
	if sslType == config.SSLTypeAutoHTTP || sslType == config.SSLTypeUser || sslType == config.SSLTypeAutoDNS {
		cfg.SSLType = sslType
	} else {
		return errors.New("SSL 类型错误")
	}
	if cfg.UsesProxyHTTPS() && cfg.SSLType != config.SSLTypeUser {
		return newUserCertificateError("HTTPS 反代模式由反向代理管理证书，Smail 不自动申请或续期；邮件服务请导入外部维护的证书")
	}

	if cfg.SSLType != config.SSLTypeUser {
		clearLegacySSLPaths(cfg)
		cfg.SSL = config.SSLConfig{
			Management:       config.SSLManagementProject,
			Mode:             config.ManagedSSLModeDefault,
			DefaultDirectory: runtimepath.SSLReference(config.ROOT_PATH, "data/ssl"),
		}
		if cfg.SSLType == config.SSLTypeAutoDNS {
			cfg.SSL.Challenge = config.SSLChallengeDNS
			if err := validateWildcardConfiguration(cfg); err != nil {
				return err
			}
		} else {
			cfg.SSL.Challenge = config.SSLChallengeHTTP
		}
	}
	if cfg.SSLType == config.SSLTypeUser {
		if cfg.UsesProxyHTTPS() {
			cfg.SSL.Management = config.SSLManagementWebServer
		} else {
			cfg.SSL.Management = config.SSLManagementCustom
		}
		privatePath, publicPath, err := validateUserCertificatePaths(priKey, crtKey)
		if err != nil {
			return err
		}
		reference, err := config.ValidateDirectSSLPaths(publicPath, privatePath, sslDomains(cfg))
		if err != nil {
			return normalizeUserCertificateImportError(err)
		}
		applyManagedSSLReference(cfg, reference)
		// 导入邮件服务证书不能重新启用反代模式下的内置 HTTPS。
		if cfg.UsesProxyHTTPS() {
			cfg.HttpsEnabled = 2
		} else {
			cfg.HttpsEnabled = 1
		}
	}

	err = config.WriteSSLConfig(&expected, cfg)
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

// clearLegacySSLPaths 只清理新受控模式不再使用的旧聚合路径字段。
func clearLegacySSLPaths(cfg *config.Config) {
	if cfg == nil {
		return
	}
	cfg.SSLPrivateKeyPath = ""
	cfg.SSLPublicKeyPath = ""
}

// applyManagedSSLReference 保存已校验的证书路径，新签发文件直接位于 ssl 下。
func applyManagedSSLReference(cfg *config.Config, reference config.ManagedSSLReference) {
	if cfg == nil {
		return
	}
	clearLegacySSLPaths(cfg)
	sslType := cfg.EffectiveSSLType()
	management := cfg.SSL.Management
	if reference.Mode == config.ManagedSSLModeDefault {
		management = config.SSLManagementProject
	} else if !config.ValidSSLManagement(management) || management == config.SSLManagementProject {
		management = config.SSLManagementCustom
	}
	cfg.SSL = config.SSLConfig{
		Management:       management,
		Mode:             reference.Mode,
		DefaultDirectory: "ssl",
		CertificateRef:   reference.CertificateRef,
		PrivateKeyRef:    reference.PrivateKeyRef,
	}
	if reference.Mode == config.ManagedSSLModeUpload {
		cfg.SSL.CustomDirectory = "ssl"
	} else if sslType == config.SSLTypeAutoDNS {
		cfg.SSL.Challenge = config.SSLChallengeDNS
	} else {
		cfg.SSL.Challenge = config.SSLChallengeHTTP
	}
}

func isAbsoluteUserCertificatePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) {
		return true
	}
	// 非 Windows 主机也要识别盘符，避免把异平台路径静默拼到项目目录。
	if len(value) >= 2 && value[1] == ':' && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) {
		return true
	}
	volume := filepath.VolumeName(value)
	return volume != "" || strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, "//")
}

func validateUserCertificatePaths(privatePath, publicPath string) (string, string, error) {
	rootPath, err := filepath.Abs(filepath.Clean(config.ROOT_PATH))
	if err != nil {
		return "", "", err
	}
	resolve := func(value string) (string, []byte, error) {
		if strings.TrimSpace(value) == "" {
			return "", nil, newUserCertificateError("请填写 SSL 证书和私钥路径")
		}
		path := strings.TrimSpace(value)
		if isAbsoluteUserCertificatePath(path) {
			if !filepath.IsAbs(path) {
				return "", nil, newUserCertificateError("SSL 路径必须是当前服务器支持的完整绝对路径")
			}
		} else {
			path = filepath.Join(rootPath, path)
		}
		path, err = filepath.Abs(filepath.Clean(path))
		if err != nil {
			return "", nil, err
		}

		info, err := os.Stat(path)
		if err != nil {
			return "", nil, newUserCertificateError("SSL 文件不存在或无权读取")
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
			return "", nil, newUserCertificateError("SSL 文件无效或大小超过限制")
		}
		content, err := os.ReadFile(path)
		if err != nil || len(content) == 0 || len(content) > 1<<20 {
			return "", nil, newUserCertificateError("SSL 文件读取失败或内容无效")
		}
		return path, content, nil
	}

	privatePath, privatePEM, err := resolve(privatePath)
	if err != nil {
		return "", "", err
	}
	publicPath, publicPEM, err := resolve(publicPath)
	if err != nil {
		return "", "", err
	}
	keyPair, err := tls.X509KeyPair(publicPEM, privatePEM)
	if err != nil || len(keyPair.Certificate) == 0 {
		return "", "", newUserCertificateError("SSL 证书与私钥不匹配")
	}
	certificate, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil || time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
		return "", "", newUserCertificateError("SSL 证书无效、尚未生效或已经过期")
	}
	switch publicKey := certificate.PublicKey.(type) {
	case *rsa.PublicKey:
		if publicKey.N.BitLen() < 2048 {
			return "", "", newUserCertificateError("SSL 证书密钥强度不能低于 2048 位")
		}
	case *ecdsa.PublicKey:
		if publicKey.Curve == nil || publicKey.Curve.Params().BitSize < 256 {
			return "", "", newUserCertificateError("SSL 证书密钥强度不能低于 256 位 ECDSA")
		}
	default:
		return "", "", newUserCertificateError("不支持当前 SSL 证书密钥类型")
	}
	privateInfo, err := os.Stat(privatePath)
	if err != nil {
		return "", "", newUserCertificateError("SSL 私钥不存在或无权读取")
	}
	if runtime.GOOS != "windows" && privateInfo.Mode().Perm()&0007 != 0 {
		return "", "", newUserCertificateError("SSL 私钥不能允许其他用户读取或写入")
	}
	return privatePath, publicPath, nil
}

func writeCertificateFiles(cfg *config.Config, privateKey, certificate, issuer []byte) error {
	if cfg == nil {
		return errors.New("SSL 配置不能为空")
	}
	if err := validateWildcardConfiguration(cfg); err != nil {
		return err
	}
	next := *cfg
	reference, err := config.StageManagedSSL(config.ManagedSSLModeDefault, privateKey, certificate, issuer, certificateDomains(cfg))
	if err != nil {
		return err
	}
	applyManagedSSLReference(&next, reference)
	defer config.ReleaseStagedSSL(reference)
	if err := config.WriteSSLConfig(cfg, &next); err != nil {
		return errors.Wrap(err)
	}
	*cfg = next
	return nil
}

func sslDomains(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	return config.SSLCertificateDomains(cfg.Domain, cfg.WebDomain, cfg.Domains)
}

func certificateMatchesConfiguredDomains(cert *x509.Certificate, cfg *config.Config) bool {
	if cert == nil || cfg == nil {
		return false
	}
	if validateWildcardConfiguration(cfg) != nil {
		return false
	}
	for _, domain := range certificateDomains(cfg) {
		if err := cert.VerifyHostname(domain); err != nil {
			return false
		}
	}
	return true
}

// 自动 DNS 同时申请主域名和泛域名；不覆盖的服务地址不能靠重复续期修复。
func validateWildcardConfiguration(cfg *config.Config) error {
	if cfg == nil || cfg.EffectiveSSLType() != config.SSLTypeAutoDNS {
		return nil
	}
	domains := wildcardCertificateDomains(cfg)
	if len(domains) != 2 {
		return newUserCertificateError("自动 DNS 证书需要有效的主域名")
	}
	base := domains[0]
	covered := func(host string) bool {
		host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
		suffix := "." + strings.ToLower(base)
		label := strings.TrimSuffix(host, suffix)
		return strings.HasSuffix(host, suffix) && label != "" && !strings.Contains(label, ".")
	}
	if cfg.WebDomain != "" && !covered(cfg.WebDomain) {
		webDomain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(cfg.WebDomain), "."))
		if webDomain != base {
			return newUserCertificateError("Web 域名必须是主域名或其一级子域名，泛域名证书不覆盖多级子域名")
		}
	}
	for _, domain := range cfg.Domains {
		if !covered("smtp."+domain) || !covered("imap."+domain) || !covered("pop."+domain) {
			return newUserCertificateError("附加邮箱域名的服务地址不在主域泛域名证书覆盖范围内")
		}
	}
	return nil
}
func wildcardCertificateDomains(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}

	domain := strings.ToLower(strings.TrimSpace(cfg.Domain))
	domain = strings.TrimSuffix(domain, ".")
	domain = strings.TrimPrefix(domain, "*.")
	if domain == "" {
		return nil
	}

	return []string{domain, "*." + domain}
}

func certificateDomains(cfg *config.Config) []string {
	if cfg != nil && cfg.EffectiveSSLType() == config.SSLTypeAutoDNS {
		return wildcardCertificateDomains(cfg)
	}
	return sslDomains(cfg)
}

func renewCertificate(privateKey *ecdsa.PrivateKey, cfg *config.Config) error {

	myUser := MyUser{
		Email: "SSLMaster@" + cfg.Domain,
		key:   privateKey,
	}

	conf := lego.NewConfig(&myUser)
	conf.UserAgent = "Smail"
	conf.Certificate.KeyType = certcrypto.RSA2048

	// A client facilitates communication with the CA server.
	client, err := lego.NewClient(conf)
	if err != nil {
		return errors.Wrap(err)
	}

	if cfg.EffectiveSSLType() == config.SSLTypeAutoHTTP {
		err = client.Challenge.SetHTTP01Provider(GetHttpChallengeInstance())
		if err != nil {
			return errors.Wrap(err)
		}
	} else if cfg.EffectiveSSLType() == config.SSLTypeAutoDNS {
		err = client.Challenge.SetDNS01Provider(GetDnsChallengeInstance(), dns01.AddDNSTimeout(60*time.Minute))
		if err != nil {
			return errors.Wrap(err)
		}

		log.Errorf("Please Set DNS Record/请将以下内容添加到DNS记录中:\n")
		for _, item := range GetDnsChallengeInstance().GetDNSSettings(nil) {
			log.Errorf("Type:%s\tHost:%s\tValue generated\n", item.Type, item.Host)
		}

	}

	var reg *registration.Resource

	reg, err = client.Registration.ResolveAccountByKey()
	if err != nil {
		return errors.Wrap(err)
	}

	myUser.Registration = reg

	domains := certificateDomains(cfg)

	request := certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	}

	log.WithFields(log.Fields{"action": "续期 TLS 证书", "result": "started"}).Info("开始请求续期证书")
	certificates, err := client.Certificate.Obtain(request)
	if err != nil {
		return errors.Wrap(err)
	}
	if err = writeCertificateFiles(cfg, certificates.PrivateKey, certificates.Certificate, certificates.IssuerCertificate); err != nil {
		return err
	}

	return nil
}

func generateCertificate(privateKey *ecdsa.PrivateKey, cfg *config.Config, newAccount bool) error {

	myUser := MyUser{
		Email: "i@" + cfg.Domain,
		key:   privateKey,
	}

	conf := lego.NewConfig(&myUser)
	conf.UserAgent = "Smail"
	conf.Certificate.KeyType = certcrypto.RSA2048

	// A client facilitates communication with the CA server.
	client, err := lego.NewClient(conf)
	if err != nil {
		return errors.Wrap(err)
	}

	if cfg.EffectiveSSLType() == config.SSLTypeAutoHTTP {
		err = client.Challenge.SetHTTP01Provider(GetHttpChallengeInstance())
		if err != nil {
			return errors.Wrap(err)
		}
	} else if cfg.EffectiveSSLType() == config.SSLTypeAutoDNS {
		err = client.Challenge.SetDNS01Provider(GetDnsChallengeInstance(), dns01.AddDNSTimeout(60*time.Minute))
		if err != nil {
			return errors.Wrap(err)
		}
	}

	var reg *registration.Resource

	if newAccount {
		reg, err = client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return errors.Wrap(err)
		}
	} else {
		reg, err = client.Registration.ResolveAccountByKey()
		if err != nil {
			return errors.Wrap(err)
		}
	}

	myUser.Registration = reg

	domains := certificateDomains(cfg)

	request := certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	}

	as := async.New(nil)

	as.Process(func(params any) {
		log.WithFields(log.Fields{"action": "申请 TLS 证书", "result": "started"}).Info("开始向证书颁发机构申请证书")
		certificates, err := client.Certificate.Obtain(request)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{"action": "申请 TLS 证书", "result": "failure"}).Error("证书申请失败")
			return
		}
		log.WithFields(log.Fields{"action": "申请 TLS 证书", "result": "success"}).Info("已取得证书，开始校验并保存")
		if err := writeCertificateFiles(cfg, certificates.PrivateKey, certificates.Certificate, certificates.IssuerCertificate); err != nil {
			log.WithError(err).WithFields(log.Fields{"action": "保存 TLS 证书", "result": "failure"}).Error("证书保存失败")
			return
		}

		if err := setup.Finish(); err != nil {
			log.WithError(err).WithFields(log.Fields{"action": "完成证书初始化", "result": "failure"}).Error("证书已保存，但完成初始化失败")
			return
		}
		log.WithFields(log.Fields{"action": "完成证书初始化", "result": "success"}).Info("证书已保存并完成初始化")

	}, nil)

	return nil
}

func GenSSL(update bool) error {

	cfg, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	if cfg.UsesProxyHTTPS() {
		return newUserCertificateError("HTTPS 反代模式由反向代理管理证书，Smail 不自动申请或续期；邮件服务请导入外部维护的证书")
	}
	if !cfg.AutomaticSSLEnabled() {
		return newUserCertificateError("当前证书由外部维护，不能自动申请或续期")
	}
	if err := validateWildcardConfiguration(cfg); err != nil {
		return err
	}

	if !update {
		privatePath, publicPath, pathErr := config.ResolveRuntimeSSLPaths(cfg)
		privateFile, errpi := os.ReadFile(privatePath)
		public, errpu := os.ReadFile(publicPath)
		if pathErr != nil {
			errpi = pathErr
			errpu = pathErr
		}
		// 当前存在证书数据，就不生成了
		if errpi == nil && errpu == nil && len(privateFile) > 0 && len(public) > 0 {
			return nil
		}
	}

	privateKey, newAccount, err := config.ReadPrivateKey()
	if err != nil {
		return errors.Wrap(err)
	}

	if !update {
		return generateCertificate(privateKey, cfg, newAccount)
	}

	return renewCertificate(privateKey, cfg)
}

// CheckSSLCrtInfo 返回证书过期剩余天数
func CheckSSLCrtInfo() (int, time.Time, bool, error) {

	cfg, err := config.ReadConfig()
	if err != nil {
		return -1, time.Now(), true, errors.Wrap(err)
	}
	// load cert and key by tls.LoadX509KeyPair
	tlsCert, err := config.LoadRuntimeTLSCertificate(cfg)
	if err != nil {
		return -1, time.Now(), true, errors.Wrap(err)
	}

	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])

	if err != nil {
		return -1, time.Now(), true, errors.Wrap(err)
	}

	nameMatchFail := !certificateMatchesConfiguredDomains(cert, cfg)

	// 检查过期时间
	hours := cert.NotAfter.Sub(time.Now()).Hours()

	if hours <= 0 {
		return -1, time.Now(), nameMatchFail, errors.New("Certificate has expired")
	}

	return cast.ToInt(hours / 24), cert.NotAfter, nameMatchFail, nil
}

func Update(_ bool) {
	cfg := config.CurrentCertificateConfig()
	if cfg.AutomaticSSLEnabled() && cfg.IsSetupReady() {
		days, _, nameMatchFail, err := CheckSSLCrtInfo()

		if days < 30 || err != nil || nameMatchFail {
			if err != nil {
				log.WithError(err).WithFields(log.Fields{"action": "检查 TLS 证书", "result": "failure"}).Warn("证书检查失败，准备重新申请")
			} else {
				log.WithFields(log.Fields{"action": "检查 TLS 证书", "result": "recorded", "remainingDays": days}).Info("证书需要更新，准备重新申请")
			}
			err = GenSSL(true)
			if err != nil {
				log.WithError(err).WithFields(log.Fields{"action": "更新 TLS 证书", "result": "failure"}).Error("证书更新失败")
				return
			}
			log.WithFields(log.Fields{"action": "更新 TLS 证书", "result": "success"}).Info("证书已更新并保存")
			if err := config.ReloadRuntimeTLSCertificate(); err != nil {
				log.WithFields(log.Fields{"action": "热加载 TLS 证书", "result": "failure"}).Warn("续期配置已保存，运行证书切换失败，请检查证书文件")
			}
		} else {
			log.Debugf("SSL Check.")
		}
	}

}
