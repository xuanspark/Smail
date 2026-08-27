package ssl

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
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
		return errors.New("SSL Type Error!")
	}

	if cfg.SSLType == config.SSLTypeUser {
		privatePath, publicPath, err := validateUserCertificatePaths(priKey, crtKey)
		if err != nil {
			return err
		}
		cfg.SSLPrivateKeyPath = privatePath
		cfg.SSLPublicKeyPath = publicPath
		// 手动设置证书的情况下后台地址默认不启用https
		cfg.HttpsEnabled = 1
	}

	err = config.WriteConfig(cfg)
	if err != nil {
		return errors.Wrap(err)
	}

	return nil
}

func validateUserCertificatePaths(privatePath, publicPath string) (string, string, error) {
	rootPath, err := filepath.Abs(filepath.Clean(config.ROOT_PATH))
	if err != nil {
		return "", "", err
	}
	base := filepath.Join(rootPath, "config", "ssl")
	baseRelative, err := filepath.Rel(rootPath, base)
	if err != nil {
		return "", "", err
	}
	workspaceRoot, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", "", errors.Wrap(err)
	}
	defer workspaceRoot.Close()
	baseInfo, err := workspaceRoot.Lstat(baseRelative)
	if err != nil || baseInfo.Mode()&os.ModeSymlink != 0 || !baseInfo.IsDir() {
		return "", "", errors.New("SSL directory is invalid")
	}
	sslRoot, err := workspaceRoot.OpenRoot(baseRelative)
	if err != nil {
		return "", "", errors.New("SSL directory is invalid")
	}
	defer sslRoot.Close()

	resolve := func(value string) (string, string, []byte, error) {
		if strings.TrimSpace(value) == "" {
			return "", "", nil, errors.New("SSL path is required")
		}
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(rootPath, path)
		}
		path, err = filepath.Abs(filepath.Clean(path))
		if err != nil {
			return "", "", nil, err
		}
		relativePath, err := filepath.Rel(base, path)
		if err != nil || relativePath == "." || filepath.IsAbs(relativePath) || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(os.PathSeparator)) {
			return "", "", nil, errors.New("SSL path must stay inside config/ssl")
		}
		info, err := sslRoot.Lstat(relativePath)
		if err != nil {
			return "", "", nil, errors.Wrap(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
			return "", "", nil, errors.New("SSL file is invalid")
		}
		content, err := sslRoot.ReadFile(relativePath)
		if err != nil || len(content) == 0 || len(content) > 1<<20 {
			return "", "", nil, errors.New("SSL file is invalid")
		}
		return filepath.Join(base, relativePath), relativePath, content, nil
	}

	privatePath, privateRelative, privatePEM, err := resolve(privatePath)
	if err != nil {
		return "", "", err
	}
	publicPath, _, publicPEM, err := resolve(publicPath)
	if err != nil {
		return "", "", err
	}
	keyPair, err := tls.X509KeyPair(publicPEM, privatePEM)
	if err != nil || len(keyPair.Certificate) == 0 {
		return "", "", errors.New("certificate and private key do not match")
	}
	certificate, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil || time.Now().Before(certificate.NotBefore) || !time.Now().Before(certificate.NotAfter) {
		return "", "", errors.New("certificate is invalid")
	}
	switch publicKey := certificate.PublicKey.(type) {
	case *rsa.PublicKey:
		if publicKey.N.BitLen() < 2048 {
			return "", "", errors.New("certificate key is too weak")
		}
	case *ecdsa.PublicKey:
		if publicKey.Curve == nil || publicKey.Curve.Params().BitSize < 256 {
			return "", "", errors.New("certificate key is too weak")
		}
	default:
		return "", "", errors.New("certificate key type is unsupported")
	}
	if err := sslRoot.Chmod(privateRelative, 0600); err != nil {
		return "", "", errors.New("private key permissions are invalid")
	}
	return privatePath, publicPath, nil
}

func writeCertificateFiles(cfg *config.Config, privateKey, certificate, issuer []byte) error {
	return writeCertificateBundle(cfg, privateKey, certificate, issuer)
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

	domains := []string{cfg.WebDomain}
	for _, domain := range cfg.Domains {
		domains = append(domains, "smtp."+domain)
		domains = append(domains, "pop."+domain)
		domains = append(domains, "imap."+domain)
	}

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

	domains := []string{cfg.WebDomain}
	for _, domain := range cfg.Domains {
		domains = append(domains, "smtp."+domain)
		domains = append(domains, "pop."+domain)
		domains = append(domains, "imap."+domain)
	}

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
		privateFile, errpi := os.ReadFile(cfg.SSLPrivateKeyPath)
		public, errpu := os.ReadFile(cfg.SSLPublicKeyPath)
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
	tlsCert, err := tls.LoadX509KeyPair(cfg.SSLPublicKeyPath, cfg.SSLPrivateKeyPath)
	if err != nil {
		return -1, time.Now(), true, errors.Wrap(err)
	}

	cert, err := x509.ParseCertificate(tlsCert.Certificate[0])

	if err != nil {
		return -1, time.Now(), true, errors.Wrap(err)
	}

	nameMatchFail := true
	for _, name := range cert.DNSNames {
		if strings.Contains(name, "imap") {
			nameMatchFail = false
			break
		}
	}

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
