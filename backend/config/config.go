package config

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/credential"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/runtimepath"
	log "github.com/sirupsen/logrus"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var IsInit bool
var legacyInitState atomic.Bool
var DevelopmentMode bool

// 旧配置读取可能迁移凭据，因此读取、整份保存及 SSL 合并必须共用同一把锁。
var legacyConfigMu sync.Mutex

type Config struct {
	LogLevel                string            `json:"logLevel"` // 日志级别
	Domain                  string            `json:"domain"`
	Domains                 []string          `json:"domains"` //多域名设置，把所有收信域名都填进去
	WebDomain               string            `json:"webDomain"`
	AccessHosts             []string          `json:"accessHosts,omitempty"`
	DkimPrivateKeyPath      string            `json:"dkimPrivateKeyPath"`
	SSLType                 string            `json:"sslType"` // 0表示自动生成证书，HTTP挑战模式，1表示用户上传证书，2表示自动-DNS挑战模式
	SSLPrivateKeyPath       string            `json:"SSLPrivateKeyPath"`
	SSLPublicKeyPath        string            `json:"SSLPublicKeyPath"`
	SSL                     SSLConfig         `json:"ssl,omitempty"`
	DbDSN                   string            `json:"dbDSN"`
	DbType                  string            `json:"dbType"`
	HttpsEnabled            int               `json:"httpsEnabled"`    //后台页面是否启用https，0默认（启用），1启用，2不启用
	SpamFilterLevel         int               `json:"spamFilterLevel"` //垃圾邮件过滤级别，0不过滤、1 spf dkim 校验均失败时过滤，2 spf校验不通过时过滤 3,dkim 校验不过的时候过滤
	HttpPort                int               `json:"httpPort"`        //http服务端口设置，默认80
	HttpsPort               int               `json:"httpsPort"`       //https服务端口，默认443
	TLSMode                 string            `json:"tlsMode,omitempty"`
	TrustedProxyCIDRs       []string          `json:"trustedProxyCidrs,omitempty"`
	WeChatPushAppId         string            `json:"weChatPushAppId"`
	WeChatPushSecret        string            `json:"weChatPushSecret"`
	WeChatPushTemplateId    string            `json:"weChatPushTemplateId"`
	WeChatPushUserId        string            `json:"weChatPushUserId"`
	TgBotToken              string            `json:"tgBotToken"`
	TgChatId                string            `json:"tgChatId"`
	IsInit                  bool              `json:"isInit"`
	SetupState              SetupState        `json:"setupState,omitempty"`
	WebPushUrl              string            `json:"webPushUrl"`
	WebPushToken            string            `json:"webPushToken"`
	SMTPHost                string            `json:"smtpHost"`
	SMTPPort                int               `json:"smtpPort"`
	SMTPUsername            string            `json:"smtpUsername"`
	SMTPPassword            string            `json:"smtpPassword"`
	SMTPSecurity            string            `json:"smtpSecurity"`
	SMTPFrom                string            `json:"smtpFrom"`
	SMTPFromDomain          string            `json:"smtpFromDomain"`
	VerificationEnabled     bool              `json:"verificationEnabled"`
	ExternalRecoveryEnabled bool              `json:"externalRecoveryEnabled"`
	LoginPolicy             string            `json:"loginPolicy"`
	PluginAllowList         []string          `json:"pluginAllowList"`
	Tables                  map[string]string `json:"-"`
	TablesInitData          map[string]string `json:"-"`
	setupPort               int               // 初始化阶段端口
}

var ROOT_PATH = ""

func init() {
	envs := os.Environ()
	for _, env := range envs {
		if strings.HasPrefix(env, "SMAIL_ROOT=") {
			ROOT_PATH = strings.TrimSpace(strings.ReplaceAll(env, "SMAIL_ROOT=", ""))
			if !strings.HasSuffix(ROOT_PATH, "/") {
				ROOT_PATH += "/"
			}

			fmt.Println("Env Root Path:", ROOT_PATH)
			return
		}
	}

	ex, err := os.Executable()
	if err != nil {
		ex, err = filepath.Abs(".")
		if err != nil {
			ROOT_PATH = "." + string(os.PathSeparator)
			return
		}
	}
	exPath := filepath.Dir(ex)
	realPath, err := filepath.EvalSymlinks(exPath)
	if err != nil {
		realPath = exPath
	}
	// 如果是Goland运行，不修改根路径
	if strings.Contains(realPath, "GoLand") && strings.Contains(realPath, "JetBrains") {
		return
	}

	if !strings.HasSuffix(realPath, "/") {
		realPath += "/"
	}
	ROOT_PATH = realPath
	fmt.Println("Root Path:", ROOT_PATH)
}

func (c *Config) GetSetupPort() int {
	if port := RuntimeSetupPort(); port > 0 {
		return port
	}
	return c.setupPort
}

func (c *Config) SetSetupPort(setupPort int) {
	SetRuntimeSetupPort(setupPort)
	c.setupPort = setupPort
}

const DBTypeMySQL = "mysql"
const DBTypeSQLite = "sqlite"
const DBTypePostgres = "postgres"

const (
	SSLChallengeHTTP = "http"
	SSLChallengeDNS  = "dns"
)
const SSLTypeAutoHTTP = "0" //自动生成证书
const SSLTypeAutoDNS = "2"  //自动生成证书，DNS api验证
const SSLTypeUser = "1"     //用户上传证书

var DBTypes []string = []string{DBTypeMySQL, DBTypeSQLite, DBTypePostgres}

var Instance *Config = &Config{}

type logFormatter struct {
}

// Format 定义日志输出格式
func (l *logFormatter) Format(entry *log.Entry) ([]byte, error) {
	b := bytes.Buffer{}

	b.WriteString(fmt.Sprintf("[%s]", entry.Level.String()))
	b.WriteString(fmt.Sprintf("[%s]", entry.Time.Format("2006-01-02 15:04:05")))
	if entry.Context != nil {
		ctx := entry.Context.(*context.Context)
		if ctx != nil {
			b.WriteString(fmt.Sprintf("[%s]", ctx.GetValue(context.LogID)))
		}
	}
	b.WriteString(fmt.Sprintf("[%s:%d]", entry.Caller.File, entry.Caller.Line))
	b.WriteString(entry.Message)
	// 错误字段也是有效内容，不能在消息为空时被格式器丢弃。
	if len(entry.Data) > 0 {
		fields := make(map[string]any, len(entry.Data))
		for key, value := range entry.Data {
			if cause, ok := value.(error); ok {
				fields[key] = cause.Error()
			} else if _, err := json.Marshal(value); err != nil {
				fields[key] = fmt.Sprintf("无法序列化的字段（%T）", value)
			} else {
				fields[key] = value
			}
		}
		payload, err := json.Marshal(fields)
		if err != nil {
			return nil, err
		}
		b.WriteByte(' ')
		b.Write(payload)
	}

	b.WriteString("\n")
	return b.Bytes(), nil
}
func Init() error {
	legacyConfigMu.Lock()
	defer legacyConfigMu.Unlock()
	var cfgData []byte
	var err error
	args := os.Args
	DevelopmentMode = len(args) >= 2 && args[len(args)-1] == "dev"
	configName := "config.json"

	if DevelopmentMode {
		configName = "config.dev.json"
		cfgData, err = readConfigFile(configName)
		if err != nil {
			return nil
		}
	} else {
		cfgData, err = readConfigFile(configName)
		if err != nil {
			log.Errorf("config file not found,%s", err.Error())
			return nil
		}
	}

	loaded := &Config{}
	partitioned, err := isPartitionedConfig(cfgData)
	if err != nil {
		return err
	}
	if partitioned {
		if DevelopmentMode {
			return fmt.Errorf("分区配置请通过 config.json 启动，不支持 config.dev.json")
		}
		loaded, err = loadPartitionedCompatibility()
		if err != nil {
			return err
		}
	} else {
		if err = json.Unmarshal(cfgData, loaded); err != nil {
			return errors.Wrap(err)
		}
		applyLegacySecurityDefaults(cfgData, loaded)
		migrated, err := unprotectCredentials(loaded)
		if err != nil {
			return errors.Wrap(err)
		}
		if migrated {
			if err = writeProtectedConfigFile(configName, loaded); err != nil {
				return errors.Wrap(err)
			}
		}
	}
	Instance = loaded
	Instance.fixPath()
	if !Instance.SetupState.Valid() {
		if Instance.IsInit {
			Instance.SetupState = SetupStateReady
		} else {
			Instance.SetupState = SetupStateNew
		}
	}

	if len(Instance.Domains) == 0 && Instance.Domain != "" {
		Instance.Domains = []string{Instance.Domain}
	}

	SetInitState(Instance.IsSetupReady())
	PublishConfigSnapshot(Instance)

	// 设置日志格式为json格式
	log.SetFormatter(&logFormatter{})
	log.SetReportCaller(true)

	// 设置将日志输出到标准输出（默认的输出为stderr,标准错误）
	// 日志消息输出可以是任意的io.writer类型
	log.SetOutput(os.Stdout)

	var cstZone = time.FixedZone("CST", 8*3600)
	time.Local = cstZone
	if Instance != nil {
		switch Instance.LogLevel {
		case "":
			log.SetLevel(log.InfoLevel)
		case "debug":
			log.SetLevel(log.DebugLevel)
		case "info":
			log.SetLevel(log.InfoLevel)
		case "warn":
			log.SetLevel(log.WarnLevel)
		case "error":
			log.SetLevel(log.ErrorLevel)
		default:
			log.SetLevel(log.InfoLevel)
		}
	} else {
		log.SetLevel(log.InfoLevel)
	}

	return nil
}

// IsSetupReady 是启动流程使用的唯一初始化状态判断。
// IsInit 仅保留用于兼容旧配置和旧测试，不再作为新业务读路径。
func (c *Config) IsSetupReady() bool {
	return c != nil && c.SetupState == SetupStateReady
}

func SetInitState(value bool) {
	legacyInitState.Store(value)
	IsInit = value
}

func legacyInitReady() bool {
	return legacyInitState.Load()
}

func ReadPrivateKey() (*ecdsa.PrivateKey, bool, error) {
	privateKey, created, err := loadOrCreateACMEPrivateKey(runtimepath.SSLDirectory(ROOT_PATH), "account_private.pem")
	if err != nil {
		return nil, false, err
	}
	return privateKey, created, nil
}
func WriteConfig(cfg *Config) error {
	legacyConfigMu.Lock()
	defer legacyConfigMu.Unlock()
	return writeConfig(cfg)
}

func writeConfig(cfg *Config) error {
	if err := writeProtectedConfigFile("config.json", cfg); err != nil {
		return err
	}
	PublishConfigSnapshot(cfg)
	return nil
}

func writeProtectedConfigFile(name string, cfg *Config) error {
	if data, err := readConfigFile(name); err == nil {
		partitioned, parseErr := isPartitionedConfig(data)
		if parseErr != nil {
			return parseErr
		}
		if partitioned {
			return fmt.Errorf("分区配置不能通过旧版写入接口覆盖，请使用配置管理")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	stored, err := protectCredentials(cfg)
	if err != nil {
		return errors.Wrap(err)
	}
	stored.SSLPrivateKeyPath = persistControlledSSLPath(stored.SSLPrivateKeyPath)
	stored.SSLPublicKeyPath = persistControlledSSLPath(stored.SSLPublicKeyPath)
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return errors.Wrap(err)
	}
	if err = writeConfigFile(name, data); err != nil {
		return errors.Wrap(err)
	}
	return nil
}

func ReadConfig() (*Config, error) {
	legacyConfigMu.Lock()
	defer legacyConfigMu.Unlock()
	return readConfig()
}

func readConfig() (*Config, error) {
	configData := Config{
		DkimPrivateKeyPath:      filepath.Join(runtimepath.DKIMDirectory(ROOT_PATH), "dkim.priv"),
		SSLPrivateKeyPath:       filepath.Join(runtimepath.SSLDirectory(ROOT_PATH), "private.key"),
		SSLPublicKeyPath:        filepath.Join(runtimepath.SSLDirectory(ROOT_PATH), "public.crt"),
		VerificationEnabled:     true,
		ExternalRecoveryEnabled: true,
		LoginPolicy:             "new_device",
	}
	cfgData, err := readConfigFile("config.json")
	if os.IsNotExist(err) {
		data, marshalErr := json.MarshalIndent(configData, "", "  ")
		if marshalErr != nil {
			return nil, errors.Wrap(marshalErr)
		}
		if writeErr := writeConfigFile("config.json", data); writeErr != nil {
			log.Errorf("Write Config Error:%s", writeErr.Error())
			return nil, errors.Wrap(writeErr)
		}
		return &configData, nil
	}
	if err != nil {
		log.Errorf("Read Config Error:%s", err.Error())
		return nil, errors.Wrap(err)
	}
	partitioned, err := isPartitionedConfig(cfgData)
	if err != nil {
		return nil, err
	}
	if partitioned {
		return loadPartitionedCompatibility()
	}
	if err = json.Unmarshal(cfgData, &configData); err != nil {
		log.Error("Read Config Unmarshal Error")
		return nil, errors.Wrap(err)
	}
	applyLegacySecurityDefaults(cfgData, &configData)
	migrated, err := unprotectCredentials(&configData)
	if err != nil {
		log.Error("Read Config Credential Error")
		return nil, errors.Wrap(err)
	}
	if migrated {
		if err = writeProtectedConfigFile("config.json", &configData); err != nil {
			log.Error("Migrate Config Credential Error")
			return nil, errors.Wrap(err)
		}
	}
	configData.fixPath()
	return &configData, nil
}

func applyLegacySecurityDefaults(data []byte, cfg *Config) {
	if cfg == nil {
		return
	}
	var legacy struct {
		VerificationEnabled     *bool   `json:"verificationEnabled"`
		ExternalRecoveryEnabled *bool   `json:"externalRecoveryEnabled"`
		LoginPolicy             *string `json:"loginPolicy"`
		VerificationLoginPolicy string  `json:"verificationLoginPolicy"`
	}
	if json.Unmarshal(data, &legacy) != nil {
		return
	}
	if legacy.VerificationEnabled == nil {
		cfg.VerificationEnabled = true
	}
	if legacy.ExternalRecoveryEnabled == nil {
		cfg.ExternalRecoveryEnabled = true
	}
	if legacy.LoginPolicy != nil && strings.TrimSpace(*legacy.LoginPolicy) != "" {
		cfg.LoginPolicy = strings.TrimSpace(*legacy.LoginPolicy)
		return
	}
	if policy := strings.TrimSpace(legacy.VerificationLoginPolicy); policy != "" {
		cfg.LoginPolicy = policy
		return
	}
	cfg.LoginPolicy = "new_device"
}

func (c *Config) fixPath() {
	if c.DbType == DBTypeSQLite {
		c.DbDSN = resolvePath(c.DbDSN)
	}
	c.SSLPublicKeyPath = resolvePath(c.SSLPublicKeyPath)
	c.SSLPrivateKeyPath = resolvePath(c.SSLPrivateKeyPath)
	c.DkimPrivateKeyPath = resolvePath(c.DkimPrivateKeyPath)
}

func persistControlledSSLPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	root, err := filepath.Abs(filepath.Clean(ROOT_PATH))
	if err != nil {
		return ""
	}
	target := value
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil || !pathWithinRoot(root, target) {
		return ""
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(relative) || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return ""
	}
	return filepath.ToSlash(relative)
}

func resolvePath(value string) string {
	value = runtimepath.SSLReference(ROOT_PATH, strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	root, err := filepath.Abs(filepath.Clean(ROOT_PATH))
	if err != nil {
		return ""
	}
	if isWindowsAbsolute(value) && filepath.VolumeName(value) == "" {
		return ""
	}
	target := value
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil || !pathWithinRoot(root, target) {
		return ""
	}
	realRoot, rootErr := filepath.EvalSymlinks(root)
	realTarget, targetErr := evalWithExistingAncestor(target)
	if rootErr == nil && targetErr == nil && !pathWithinRoot(realRoot, realTarget) {
		return ""
	}
	return target
}

func pathWithinRoot(root string, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && !filepath.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isWindowsAbsolute(value string) bool {
	if strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, "//") {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func evalWithExistingAncestor(target string) (string, error) {
	current := target
	parts := make([]string, 0)
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(parts) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, parts[i])
			}
			return filepath.Abs(resolved)
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		parts = append(parts, filepath.Base(current))
		current = parent
	}
}

func protectCredentials(cfg *Config) (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("invalid configuration")
	}
	stored := *cfg
	vault, err := credential.NewVault(ROOT_PATH)
	if err != nil {
		return nil, fmt.Errorf("credential protection failed")
	}
	values := []*string{&stored.WeChatPushSecret, &stored.TgBotToken, &stored.WebPushToken, &stored.SMTPPassword}
	for _, value := range values {
		encrypted, err := vault.Encrypt(*value)
		if err != nil {
			return nil, fmt.Errorf("credential protection failed")
		}
		*value = encrypted
	}
	return &stored, nil
}

func unprotectCredentials(cfg *Config) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("invalid configuration")
	}
	vault, err := credential.NewVault(ROOT_PATH)
	if err != nil {
		return false, fmt.Errorf("credential decryption failed")
	}
	values := []*string{&cfg.WeChatPushSecret, &cfg.TgBotToken, &cfg.WebPushToken, &cfg.SMTPPassword}
	for _, value := range values {
		if *value == "" {
			continue
		}
		if !credential.IsEncrypted(*value) {
			return false, fmt.Errorf("credential decryption failed")
		}
		plaintext, err := vault.Decrypt(credential.CurrentVersion, *value)
		if err != nil {
			return false, fmt.Errorf("credential decryption failed")
		}
		*value = plaintext
	}
	return false, nil
}
