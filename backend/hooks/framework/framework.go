package framework

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type EmailHook interface {
	// SendBefore 邮件发送前的数据 同步执行
	SendBefore(ctx *context.Context, email *parsemail.Email)
	// SendAfter 邮件发送后的数据，err是每个收信服务器的错误信息 异步执行
	SendAfter(ctx *context.Context, email *parsemail.Email, err map[string]error)
	// ReceiveParseBefore 接收到邮件，解析之前的原始数据 同步执行
	ReceiveParseBefore(ctx *context.Context, email *[]byte)
	// ReceiveParseAfter 接收到邮件，解析之后的结构化数据 (收信规则前，写数据库前执行)  同步执行
	ReceiveParseAfter(ctx *context.Context, email *parsemail.Email)
	// ReceiveSaveAfter 邮件落库以后执行（收信规则后执行） 异步执行
	ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail)
	// GetName 获取插件名称
	GetName(ctx *context.Context) string
	// SettingsHtml 插件页面
	SettingsHtml(ctx *context.Context, url string, requestData string) string
}

// HookDTO Smail 主程序和插件通信的结构体
type HookDTO struct {
	ServerVersion string           // 服务端程序版本
	Ctx           *context.Context // 上下文
	Email         *parsemail.Email // 邮件内容
	EmailByte     *[]byte          // 未解析前的文件内容
	ErrMap        map[string]error // 错误信息
	UserEmail     []*models.UserEmail
}

type SettingsHtmlRequest struct {
	Ctx         *context.Context // 上下文
	URL         string
	RequestData string
}
type UserSettingsRequest struct {
	UserID   int             `json:"userId"`
	Settings json.RawMessage `json:"settings"`
}

// 插件能力宣告
type Capabilities struct {
	PluginID     string   `json:"pluginId"`
	Capabilities []string `json:"capabilities,omitempty"`
	Settings     []string `json:"settings,omitempty"`
	Protocol     uint32   `json:"protocol"`
}

// 插件能力提供接口
type PluginRuntimeConfig struct {
	PluginID     string                `json:"pluginId"`
	Generation   uint64                `json:"generation"`
	Settings     json.RawMessage       `json:"settings,omitempty"`
	Secrets      map[string]string     `json:"secrets,omitempty"`
	Capabilities []string              `json:"capabilities,omitempty"`
	Network      []PluginNetworkTarget `json:"network,omitempty"`
}

type PluginNetworkTarget struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
}

type CapabilityProvider interface {
	Capabilities() Capabilities
}

// 可配置插件接口
type ConfigurableHook interface {
	ValidateConfig(json.RawMessage) error
	Configure(json.RawMessage) error
	Health() error
}

// UserSettingsConfigurer 接收主程序下发的已授权用户设置。
type UserSettingsConfigurer interface {
	ConfigureUserSettings(userID int, settings json.RawMessage) error
}

// BotInfoProvider 提供不包含密钥的机器人公开信息。
type BotInfoProvider interface {
	GetBotInfo() (BotInfo, error)
}

// TestMessageProvider 仅负责发送主程序已校验的测试消息。
type TestMessageProvider interface {
	TestMessage(json.RawMessage) error
}

type BotInfo struct {
	Username string `json:"username"`
	BotLink  string `json:"botLink"`
}

type Plugin struct {
	name      string
	hook      EmailHook
	authToken string
}

func CreatePlugin(name string, hook EmailHook) *Plugin {
	return &Plugin{
		name:      name,
		hook:      hook,
		authToken: os.Getenv("SMAIL_PLUGIN_RPC_TOKEN"),
	}
}

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

	b.WriteString("\n")
	return b.Bytes(), nil
}

const maxPluginRPCBytes int64 = 8 << 20
const pluginRPCConcurrencyLimit = 32

func decodePluginRequest(w http.ResponseWriter, request *http.Request, target any) bool {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	request.Body = http.MaxBytesReader(w, request.Body, maxPluginRPCBytes)
	defer request.Body.Close()
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return false
	}
	return true
}

func writePluginResponse(w http.ResponseWriter, contentType string, body []byte) error {
	if int64(len(body)) > maxPluginRPCBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return errors.New("plugin response exceeds the allowed size")
	}
	w.Header().Set("Content-Type", contentType)
	n, err := w.Write(body)
	if err != nil {
		return err
	}
	if n != len(body) {
		return io.ErrShortWrite
	}
	return nil
}

func (p *Plugin) handleHookDTO(endpoint string, action func(*HookDTO)) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		log.Debugf("[%s] %s Start", p.name, endpoint)
		var dto HookDTO
		if !decodePluginRequest(w, request, &dto) {
			return
		}
		action(&dto)
		body, err := json.Marshal(dto)
		if err != nil {
			log.WithError(err).Errorf("[%s] %s response encoding failed", p.name, endpoint)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := writePluginResponse(w, "application/json", body); err != nil {
			log.WithError(err).Errorf("[%s] %s response write failed", p.name, endpoint)
			return
		}
		log.Debugf("[%s] %s End", p.name, endpoint)
	}
}

func (p *Plugin) capabilities() Capabilities {
	if provider, ok := p.hook.(CapabilityProvider); ok {
		capabilities := provider.Capabilities()
		if capabilities.PluginID == "" {
			capabilities.PluginID = p.name
		}
		if capabilities.Protocol == 0 {
			capabilities.Protocol = 1
		}
		return capabilities
	}
	return Capabilities{PluginID: p.name, Protocol: 1}
}

func (p *Plugin) handleConfig(action func(json.RawMessage) error) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		var payload json.RawMessage
		if !decodePluginRequest(w, request, &payload) {
			return
		}
		if err := action(payload); err != nil {
			http.Error(w, "plugin configuration rejected", http.StatusBadRequest)
			return
		}
		_ = writePluginResponse(w, "application/json", []byte(`{"status":"ok"}`))
	}
}

func (p *Plugin) authenticatedHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		token := request.Header.Get("X-Smail-Plugin-Token")
		if p.authToken == "" || token == "" || len(token) != len(p.authToken) || subtle.ConstantTimeCompare([]byte(token), []byte(p.authToken)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, request)
	})
}

func (p *Plugin) limitedHandler(next http.Handler) http.Handler {
	semaphore := make(chan struct{}, pluginRPCConcurrencyLimit)
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		select {
		case semaphore <- struct{}{}:
			defer func() { <-semaphore }()
		case <-request.Context().Done():
			return
		}
		next.ServeHTTP(w, request)
	})
}

func (p *Plugin) Run() error {

	// 设置日志格式为json格式
	log.SetFormatter(&logFormatter{})
	log.SetReportCaller(true)

	// 设置将日志输出到标准输出（默认的输出为stderr,标准错误）
	// 日志消息输出可以是任意的io.writer类型
	log.SetOutput(os.Stdout)

	if len(os.Args) < 2 {
		return fmt.Errorf("plugin listener argument is required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/SendBefore", p.handleHookDTO("SendBefore", func(dto *HookDTO) {
		p.hook.SendBefore(dto.Ctx, dto.Email)
	}))
	mux.HandleFunc("/SendAfter", p.handleHookDTO("SendAfter", func(dto *HookDTO) {
		p.hook.SendAfter(dto.Ctx, dto.Email, dto.ErrMap)
	}))
	mux.HandleFunc("/ReceiveParseBefore", p.handleHookDTO("ReceiveParseBefore", func(dto *HookDTO) {
		p.hook.ReceiveParseBefore(dto.Ctx, dto.EmailByte)
	}))
	mux.HandleFunc("/ReceiveParseAfter", p.handleHookDTO("ReceiveParseAfter", func(dto *HookDTO) {
		p.hook.ReceiveParseAfter(dto.Ctx, dto.Email)
	}))
	mux.HandleFunc("/ReceiveSaveAfter", p.handleHookDTO("ReceiveSaveAfter", func(dto *HookDTO) {
		p.hook.ReceiveSaveAfter(dto.Ctx, dto.Email, dto.UserEmail)
	}))
	mux.HandleFunc("/GetName", func(w http.ResponseWriter, request *http.Request) {
		var dto HookDTO
		if !decodePluginRequest(w, request, &dto) {
			return
		}
		name := strings.ReplaceAll(p.hook.GetName(dto.Ctx), " ", "")
		if err := writePluginResponse(w, "text/plain; charset=utf-8", []byte(name)); err != nil {
			log.WithError(err).Errorf("[%s] GetName response write failed", p.name)
		}
	})
	mux.HandleFunc("/SettingsHtml", func(w http.ResponseWriter, request *http.Request) {
		var dto SettingsHtmlRequest
		if !decodePluginRequest(w, request, &dto) {
			return
		}
		html := p.hook.SettingsHtml(dto.Ctx, dto.URL, dto.RequestData)
		if err := writePluginResponse(w, "text/html; charset=utf-8", []byte(html)); err != nil {
			log.WithError(err).Errorf("[%s] SettingsHtml response write failed", p.name)
		}
	})
	mux.HandleFunc("/Capabilities", func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := json.Marshal(p.capabilities())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = writePluginResponse(w, "application/json", body)
	})
	if configurable, ok := p.hook.(ConfigurableHook); ok {
		mux.Handle("/ValidateConfig", p.handleConfig(configurable.ValidateConfig))
		mux.Handle("/Configure", p.handleConfig(configurable.Configure))
		mux.HandleFunc("/Health", func(w http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if err := configurable.Health(); err != nil {
				http.Error(w, "plugin health check failed", http.StatusServiceUnavailable)
				return
			}
			_ = writePluginResponse(w, "application/json", []byte(`{"status":"ok"}`))
		})
	}

	if configurable, ok := p.hook.(UserSettingsConfigurer); ok {
		mux.HandleFunc("/UserSettings", func(w http.ResponseWriter, request *http.Request) {
			var payload UserSettingsRequest
			if !decodePluginRequest(w, request, &payload) {
				return
			}
			if payload.UserID <= 0 || len(payload.Settings) == 0 || int64(len(payload.Settings)) > maxPluginRPCBytes {
				http.Error(w, "用户设置请求无效", http.StatusBadRequest)
				return
			}
			if err := configurable.ConfigureUserSettings(payload.UserID, payload.Settings); err != nil {
				http.Error(w, "用户设置下发失败", http.StatusBadRequest)
				return
			}
			_ = writePluginResponse(w, "application/json", []byte(`{"status":"ok"}`))
		})
	}
	if provider, ok := p.hook.(BotInfoProvider); ok {
		mux.HandleFunc("/BotInfo", func(w http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			info, err := provider.GetBotInfo()
			if err != nil {
				http.Error(w, "获取机器人信息失败", http.StatusServiceUnavailable)
				return
			}
			body, err := json.Marshal(info)
			if err != nil {
				http.Error(w, "机器人信息无效", http.StatusInternalServerError)
				return
			}
			_ = writePluginResponse(w, "application/json", body)
		})
	}
	if provider, ok := p.hook.(TestMessageProvider); ok {
		mux.HandleFunc("/TestMessage", func(w http.ResponseWriter, request *http.Request) {
			var payload json.RawMessage
			if !decodePluginRequest(w, request, &payload) {
				return
			}
			if err := provider.TestMessage(payload); err != nil {
				http.Error(w, "发送测试消息失败", http.StatusBadRequest)
				return
			}
			_ = writePluginResponse(w, "application/json", []byte(`{"status":"ok"}`))
		})
	}
	server := http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		Handler:      p.authenticatedHandler(p.limitedHandler(mux)),
	}

	unixListener, err := pluginListener(os.Args[1])
	if err != nil {
		return err
	}
	defer unixListener.Close()
	if err = server.Serve(unixListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve plugin: %w", err)
	}
	return nil
}

func pluginListener(socketArgument string) (net.Listener, error) {
	if strings.HasPrefix(socketArgument, "fd:") {
		fd, err := strconv.ParseUint(strings.TrimPrefix(socketArgument, "fd:"), 10, 32)
		if err != nil || fd < 3 {
			return nil, fmt.Errorf("invalid plugin listener descriptor")
		}
		file := os.NewFile(uintptr(fd), "smail-plugin-listener")
		if file == nil {
			return nil, fmt.Errorf("plugin listener descriptor is unavailable")
		}
		listener, err := net.FileListener(file)
		_ = file.Close()
		if err != nil {
			return nil, fmt.Errorf("open plugin listener: %w", err)
		}
		return listener, nil
	}

	socketAddress := socketArgument
	if !strings.HasPrefix(socketAddress, "@") {
		exePath, err := getExePath()
		if err != nil {
			return nil, err
		}
		socketAddress = exePath + "/" + socketAddress
	}
	return net.Listen("unix", socketAddress)
}

func getExePath() (string, error) {
	//return "/Smail/server/hooks/spam_block" //debug socket path

	ex, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve plugin executable: %w", err)
	}
	exePath := filepath.Dir(ex)
	return exePath, nil
}
