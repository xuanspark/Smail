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
	"git.pcux.cn/xuans/Smail/signal"
	"git.pcux.cn/xuans/Smail/utils/async"
	"git.pcux.cn/xuans/Smail/utils/errors"
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
	if cfg.SSLType == "" {
		return config.SSLTypeAutoHTTP, nil
	}

	return cfg.SSLType, nil
}

func SetSSL(sslType, priKey, crtKey string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
	}
	if sslType == config.SSLTypeAutoHTTP || sslType == config.SSLTypeUser || sslType == config.SSLTypeAutoDNS {
		cfg.SSLType = sslType
	} else {
		return errors.New("SSL 类型错误")
	}

	if cfg.SSLType != config.SSLTypeUser {
		clearLegacySSLPaths(cfg)
		cfg.SSL = config.SSLConfig{
			Mode:             config.ManagedSSLModeDefault,
			DefaultDirectory: "config/ssl",
		}
	}
	if cfg.SSLType == config.SSLTypeUser {
		privatePath, publicPath, err := validateUserCertificatePaths(priKey, crtKey)
		if err != nil {
			return err
		}
		privatePEM, err := os.ReadFile(privatePath)
		if err != nil {
			return newUserCertificateError("SSL 私钥无法读取，请确认文件路径和 smail 用户权限")
		}
		publicPEM, err := os.ReadFile(publicPath)
		if err != nil {
			return newUserCertificateError("SSL 证书无法读取，请确认文件路径和 smail 用户权限")
		}
		reference, err := config.ImportManagedSSL(config.ManagedSSLModeUpload, privatePEM, publicPEM, nil, sslDomains(cfg))
		if err != nil {
			return normalizeUserCertificateImportError(err)
		}
		applyManagedSSLReference(cfg, reference)
		// 手动设置证书的情况下后台地址默认不启用https
		cfg.HttpsEnabled = 1
	}

	err = config.WriteConfig(cfg)
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

// applyManagedSSLReference 将证书切换结果写入内部引用，不持久化服务器路径。
func applyManagedSSLReference(cfg *config.Config, reference config.ManagedSSLReference) {
	if cfg == nil {
		return
	}
	clearLegacySSLPaths(cfg)
	cfg.SSL = config.SSLConfig{
		Mode:             reference.Mode,
		DefaultDirectory: "config/ssl",
		CertificateRef:   reference.CertificateRef,
		PrivateKeyRef:    reference.PrivateKeyRef,
	}
	if reference.Mode == config.ManagedSSLModeUpload {
		cfg.SSL.CustomDirectory = "config/ssl/managed/upload"
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
			if filepath.VolumeName(path) != "" && !filepath.IsAbs(path) {
				return "", nil, newUserCertificateError("SSL 路径必须是完整的绝对路径")
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
	reference, err := config.ImportManagedSSL(config.ManagedSSLModeDefault, privateKey, certificate, issuer, certificateDomains(cfg))
	if err != nil {
		return err
	}
	applyManagedSSLReference(cfg, reference)
	if err := config.WriteConfig(cfg); err != nil {
		return errors.Wrap(err)
	}
	return nil
}

func sslDomains(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	domains := make([]string, 0, len(cfg.Domains)*3+1)
	appendDomain := func(domain string) {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			return
		}
		if _, exists := seen[domain]; exists {
			return
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	appendDomain(cfg.WebDomain)
	appendDomain(cfg.Domain)
	for _, domain := range cfg.Domains {
		appendDomain(domain)
		appendDomain("smtp." + domain)
		appendDomain("pop." + domain)
		appendDomain("imap." + domain)
	}
	return domains
}

func certificateMatchesConfiguredDomains(cert *x509.Certificate, cfg *config.Config) bool {
	if cert == nil || cfg == nil {
		return false
	}
	for _, domain := range sslDomains(cfg) {
		if err := cert.VerifyHostname(domain); err != nil {
			return false
		}
	}
	return true
}
func wildcardCertificateDomains(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}

	domain := strings.TrimSpace(cfg.Domain)
	domain = strings.TrimSuffix(domain, ".")
	domain = strings.TrimPrefix(domain, "*.")
	if domain == "" {
		return nil
	}

	return []string{"*." + domain}
}

func certificateDomains(cfg *config.Config) []string {
	if cfg != nil && cfg.SSLType == config.SSLTypeAutoDNS {
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

	if cfg.SSLType == config.SSLTypeAutoHTTP {
		err = client.Challenge.SetHTTP01Provider(GetHttpChallengeInstance())
		if err != nil {
			return errors.Wrap(err)
		}
	} else if cfg.SSLType == config.SSLTypeAutoDNS {
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

	log.Infof("wait ssl renew")
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

	if cfg.SSLType == config.SSLTypeAutoHTTP {
		err = client.Challenge.SetHTTP01Provider(GetHttpChallengeInstance())
		if err != nil {
			return errors.Wrap(err)
		}
	} else if cfg.SSLType == config.SSLTypeAutoDNS {
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
		log.Infof("wait ssl")
		certificates, err := client.Certificate.Obtain(request)
		if err != nil {
			log.WithError(err).Error("certificate issuance failed")
			return
		}
		log.Infof("证书校验通过！")
		if err := writeCertificateFiles(cfg, certificates.PrivateKey, certificates.Certificate, certificates.IssuerCertificate); err != nil {
			log.WithError(err).Error("certificate storage failed")
			return
		}

		if err := setup.Finish(); err != nil {
			log.WithError(err).Error("finish setup after certificate issuance failed")
			return
		}

	}, nil)

	return nil
}

func GenSSL(update bool) error {

	cfg, err := config.ReadConfig()
	if err != nil {
		return errors.Wrap(err)
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

func Update(needRestart bool) {
	cfg := config.CurrentConfig()
	if cfg != nil && cfg.IsSetupReady() && (cfg.SSLType == config.SSLTypeAutoHTTP || cfg.SSLType == config.SSLTypeAutoDNS) {
		days, _, nameMatchFail, err := CheckSSLCrtInfo()

		if days < 30 || err != nil || nameMatchFail {
			if err != nil {
				log.Errorf("SSL Check Error, Update SSL Certificate. Error Info :%+v", err)
			} else {
				log.Infof("SSL certificate remaining time is only %d days, renew SSL certificate.", days)
			}
			err = GenSSL(true)
			if err != nil {
				log.Errorf("SSL Update Error! %+v", err)
				return
			}
			if needRestart {
				// 更新完证书，重启服务
				signal.RestartChan <- true
			}
		} else {
			log.Debugf("SSL Check.")
		}
	}

}
