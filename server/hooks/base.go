package hooks

import (
	"bytes"
	oContext "context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/atomicfile"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// HookList
var HookList map[string]framework.EmailHook

type HookSender struct {
	httpc  http.Client
	name   string
	socket string
}

const (
	maxPluginRPCBytes                  int64 = 8 << 20
	maxPluginSettingsPageResponseBytes       = 1 << 20
	maxPluginSettingsSaveResponseBytes       = 4 << 10
)

var errPluginRPCResponseTooLarge = errors.New("plugin response exceeds the allowed size")

func (h *HookSender) call(endpoint string, payload any) ([]byte, error) {
	return h.callWithResponseLimit(endpoint, payload, maxPluginRPCBytes)
}

func (h *HookSender) callWithResponseLimit(endpoint string, payload any, maxResponseBytes int64) ([]byte, error) {
	if maxResponseBytes <= 0 || maxResponseBytes > maxPluginRPCBytes {
		return nil, errors.New("plugin response limit is invalid")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxPluginRPCBytes {
		return nil, errors.New("plugin request exceeds the allowed size")
	}

	req, err := http.NewRequest(http.MethodPost, "http://unix/"+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	ret, err := h.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer ret.Body.Close()
	if ret.StatusCode < http.StatusOK || ret.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("plugin returned status %d", ret.StatusCode)
	}
	if ret.ContentLength > maxResponseBytes {
		return nil, errPluginRPCResponseTooLarge
	}
	responseBody, err := io.ReadAll(io.LimitReader(ret.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(responseBody)) > maxResponseBytes {
		return nil, errPluginRPCResponseTooLarge
	}
	return responseBody, nil
}

func (h *HookSender) callHook(ctx *context.Context, endpoint string, dto framework.HookDTO) (framework.HookDTO, bool) {
	body, err := h.call(endpoint, dto)
	if err != nil {
		log.WithContext(ctx).Errorf("[%s] Plugin call failed: %v", h.name, err)
		return dto, false
	}
	if len(body) == 0 {
		return dto, true
	}
	if err = json.Unmarshal(body, &dto); err != nil {
		log.WithContext(ctx).Errorf("[%s] Plugin returned invalid data", h.name)
		return dto, false
	}
	return dto, true
}

func (h *HookSender) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail) {
	_, _ = h.callHook(ctx, "ReceiveSaveAfter", framework.HookDTO{Ctx: ctx, Email: email, UserEmail: ue})
}

func (h *HookSender) SendBefore(ctx *context.Context, email *parsemail.Email) {
	dto, ok := h.callHook(ctx, "SendBefore", framework.HookDTO{Ctx: ctx, Email: email})
	if ok && dto.Email != nil && email != nil {
		*email = *dto.Email
	}
}

func (h *HookSender) SendAfter(ctx *context.Context, email *parsemail.Email, errMap map[string]error) {
	_, _ = h.callHook(ctx, "SendAfter", framework.HookDTO{Ctx: ctx, Email: email, ErrMap: errMap})
}

func (h *HookSender) ReceiveParseBefore(ctx *context.Context, email *[]byte) {
	dto, ok := h.callHook(ctx, "ReceiveParseBefore", framework.HookDTO{Ctx: ctx, EmailByte: email})
	if ok && dto.EmailByte != nil && email != nil {
		*email = append((*email)[:0], (*dto.EmailByte)...)
	}
}

func (h *HookSender) ReceiveParseAfter(ctx *context.Context, email *parsemail.Email) {
	dto, ok := h.callHook(ctx, "ReceiveParseAfter", framework.HookDTO{Ctx: ctx, Email: email})
	if ok && dto.Email != nil && email != nil {
		*email = *dto.Email
	}
}

func (h *HookSender) GetName(ctx *context.Context) string {
	body, err := h.call("GetName", framework.HookDTO{Ctx: ctx})
	if err != nil {
		log.WithContext(ctx).Errorf("[%s] Plugin call failed: %v", h.name, err)
		return ""
	}
	return string(body)
}

func (h *HookSender) SettingsHtml(ctx *context.Context, url string, requestData string) string {
	responseLimit := int64(maxPluginSettingsPageResponseBytes)
	if strings.HasSuffix(strings.TrimSuffix(url, "/"), "/save") {
		responseLimit = int64(maxPluginSettingsSaveResponseBytes)
	}
	body, err := h.callWithResponseLimit("SettingsHtml", framework.SettingsHtmlRequest{Ctx: ctx, URL: url, RequestData: requestData}, responseLimit)
	if err != nil {
		log.WithContext(ctx).Errorf("[%s] Plugin call failed: %v", h.name, err)
		return ""
	}
	return string(body)
}

func NewHookSender(socketPath string, name string, serverVersion string) *HookSender {
	httpc := http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			DialContext: func(ctx oContext.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
	return &HookSender{
		httpc:  httpc,
		socket: socketPath,
		name:   name,
	}
}

var (
	processList   []*os.Process
	processListMu sync.Mutex
)

// Init 注册hook对象
func Init(serverVersion string) {
	Stop()
	ResetHooks()
	cfg := config.CurrentConfig()
	if cfg == nil {
		log.Warn("Plugin configuration is unavailable")
		return
	}

	allowedPlugins := pluginAllowList(cfg.PluginAllowList)
	if len(allowedPlugins) == 0 {
		log.Info("Plugin loading is disabled because the allow list is empty")
		return
	}

	root, err := filepath.Abs("./plugins")
	if err != nil {
		log.Error("Plugin root resolve failed")
		return
	}
	if !securePluginDirectory(root) || !securePluginDirectoryOwner(root) {
		log.Error("Plugin directory permissions are unsafe")
		return
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		log.Error("Plugin directory cannot be read")
		return
	}

	pluginNo := 1
	for _, entry := range entries {
		name := entry.Name()
		if !allowedPlugins[name] || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || !isPluginExecutable(name, info.Mode()) || !securePluginFile(info.Mode()) || !securePluginFileOwner(info) {
			log.Warnf("[%s] Plugin file was rejected", name)
			continue
		}

		pluginPath := filepath.Join(root, name)
		socketName, err := newPluginSocketName(pluginNo)
		if err != nil {
			log.Warnf("[%s] Plugin socket name generation failed", name)
			continue
		}
		env := pluginEnvironment(root, name)
		launchSpec, err := newPluginLaunchSpec(pluginPath, root, name, socketName, env)
		if err != nil {
			if !config.DevelopmentMode {
				log.Warnf("[%s] Plugin rejected because secure process isolation is unavailable: %v", name, err)
				continue
			}
			log.Warnf("[%s] Secure process isolation is unavailable in development mode: %v", name, err)
			launchSpec = compatibilityPluginLaunchSpec(pluginPath, root, name, socketName, env)
		}
		socketPath := launchSpec.socketPath
		if !isAbstractPluginSocket(socketPath) {
			if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
				log.Warnf("[%s] Plugin socket cleanup failed", name)
				launchSpec.cleanup()
				continue
			}
		}

		log.Infof("[%s] Plugin load", name)
		process, err := os.StartProcess(launchSpec.path, launchSpec.args, launchSpec.procAttr)
		if err != nil {
			log.Warnf("[%s] Plugin start failed", name)
			launchSpec.cleanup()
			continue
		}
		launchSpec.closeParentFiles()
		if err := applyPluginResourceLimits(process); err != nil {
			log.Warnf("[%s] Plugin resource limits could not be applied", name)
			_ = process.Kill()
			_, _ = process.Wait()
			launchSpec.cleanup()
			continue
		}
		processListMu.Lock()
		processList = append(processList, process)
		processListMu.Unlock()
		pluginNo++

		go waitPlugin(process, name, socketPath, launchSpec)

		if !waitPluginSocket(socketPath) {
			log.Warnf("[%s] Plugin socket was not created", name)
			_ = process.Kill()
			continue
		}
		if runtime.GOOS != "windows" && !isAbstractPluginSocket(socketPath) {
			if err := os.Chmod(socketPath, 0600); err != nil {
				log.Warnf("[%s] Plugin socket permissions could not be tightened", name)
				_ = process.Kill()
				continue
			}
		}

		hook := NewHookSender(socketPath, name, serverVersion)
		hookName := strings.TrimSpace(hook.GetName(&context.Context{}))
		if hookName == "" || !RegisterHook(hookName, hook) {
			log.Warnf("[%s] Plugin reported an invalid hook name", name)
			_ = process.Kill()
			continue
		}
		log.Infof("[%s] Plugin load success", hookName)
	}
}

func pluginAllowList(values []string) map[string]bool {
	allowed := make(map[string]bool, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\*?[]`) {
			continue
		}
		allowed[name] = true
	}
	return allowed
}

func securePluginDirectory(root string) bool {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return false
	}
	return info.Mode()&os.ModeSymlink == 0 && info.Mode()&os.ModeSetuid == 0 && info.Mode()&os.ModeSetgid == 0 && (runtime.GOOS == "windows" || info.Mode().Perm()&0022 == 0)
}

func securePluginFile(mode os.FileMode) bool {
	return runtime.GOOS == "windows" || mode&os.ModeSetuid == 0 && mode&os.ModeSetgid == 0 && mode.Perm()&0022 == 0
}

func isPluginExecutable(name string, mode os.FileMode) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Ext(name), ".exe")
	}
	return filepath.Ext(name) == "" && mode.Perm()&0100 != 0
}

func waitPluginSocket(socketPath string) bool {
	if isAbstractPluginSocket(socketPath) {
		for i := 0; i < 50; i++ {
			conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return true
			}
			time.Sleep(100 * time.Millisecond)
		}
		return false
	}
	for i := 0; i < 5; i++ {
		time.Sleep(time.Second)
		info, err := os.Lstat(socketPath)
		if err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode()&os.ModeSymlink == 0 {
			return true
		}
	}
	return false
}

func waitPlugin(process *os.Process, name string, socketPath string, launchSpec *pluginLaunchSpec) {
	_, err := process.Wait()
	removeProcess(process)
	if err != nil {
		log.Warnf("[%s] Plugin stopped", name)
	}
	RemoveHooksByPlugin(name)
	if !isAbstractPluginSocket(socketPath) {
		_ = os.Remove(socketPath)
	}
	launchSpec.cleanup()
}

func newPluginSocketName(pluginNo int) (string, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return fmt.Sprintf("smail-%d-%s", pluginNo, hex.EncodeToString(token)), nil
}

func minimalPluginEnv(root string) []string {
	allowed := map[string]bool{
		"PATH": true, "SYSTEMROOT": true, "WINDIR": true, "TEMP": true, "TMP": true,
		"LANG": true, "LC_ALL": true,
	}
	ret := []string{"SMAIL_PLUGIN=1", "SMAIL_ROOT=" + root}
	for _, item := range os.Environ() {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 && allowed[strings.ToUpper(parts[0])] {
			ret = append(ret, item)
		}
	}
	return ret
}

func pluginEnvironment(root, name string) []string {
	ret := minimalPluginEnv(root)
	cfg := config.CurrentConfig()
	if cfg == nil {
		return ret
	}
	pluginName := strings.TrimSuffix(strings.ToLower(name), filepath.Ext(name))
	if pluginName == "wechat_push" {
		ret = appendPluginEnv(ret, "SMAIL_WECHAT_APP_ID", cfg.WeChatPushAppId)
		ret = appendPluginEnv(ret, "SMAIL_WECHAT_SECRET", cfg.WeChatPushSecret)
		ret = appendPluginEnv(ret, "SMAIL_WECHAT_TEMPLATE_ID", cfg.WeChatPushTemplateId)
		ret = appendPluginEnv(ret, "SMAIL_WECHAT_USER_ID", cfg.WeChatPushUserId)
		ret = appendPluginEnv(ret, "SMAIL_WEB_DOMAIN", cfg.WebDomain)
		ret = appendPluginEnv(ret, "SMAIL_HTTPS_ENABLED", fmt.Sprintf("%d", cfg.HttpsEnabled))
	}
	return ret
}

func appendPluginEnv(env []string, name, value string) []string {
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return env
	}
	return append(env, name+"="+value)
}

func PersistPluginConfig(pluginName, payload string) error {
	if len(payload) == 0 || int64(len(payload)) > maxPluginConfigBytes || !json.Valid([]byte(payload)) {
		return errors.New("plugin configuration is invalid")
	}
	hook, ok := GetHook(pluginName)
	if !ok {
		return errors.New("plugin is unavailable")
	}
	sender, ok := hook.(*HookSender)
	if !ok {
		return errors.New("plugin configuration persistence is unavailable")
	}
	root, err := filepath.Abs("./plugins")
	if err != nil || !securePluginDirectory(root) || !securePluginDirectoryOwner(root) {
		return errors.New("plugin directory is unsafe")
	}
	configPath := filepath.Join(root, pluginConfigFileName(sender.name))
	info, err := os.Lstat(configPath)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("plugin configuration path is unsafe")
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return atomicfile.Write(configPath, []byte(payload), 0600)
}

func removeProcess(target *os.Process) {
	processListMu.Lock()
	defer processListMu.Unlock()
	filtered := processList[:0]
	for _, process := range processList {
		if process != target {
			filtered = append(filtered, process)
		}
	}
	processList = filtered
}

func Stop() {
	log.Info("Plugin Stop")
	processListMu.Lock()
	processes := append([]*os.Process(nil), processList...)
	processList = nil
	processListMu.Unlock()
	for _, process := range processes {
		_ = process.Kill()
	}
}
