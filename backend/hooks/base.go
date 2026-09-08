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
	pluginservice "git.pcux.cn/xuans/Smail/services/plugin"
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
	httpc      http.Client
	name       string
	executable string
	socket     string
	authToken  string
}

var pluginConfigService *pluginservice.ConfigService

func SetPluginConfigService(service *pluginservice.ConfigService) {
	pluginConfigService = service
}

func PluginConfigService() *pluginservice.ConfigService {
	return pluginConfigService
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
	if h.authToken != "" {
		req.Header.Set("X-Smail-Plugin-Token", h.authToken)
	}
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

func (h *HookSender) Capabilities() (framework.Capabilities, error) {
	body, err := h.call("Capabilities", struct{}{})
	if err != nil {
		return framework.Capabilities{}, err
	}
	var capabilities framework.Capabilities
	if err := json.Unmarshal(body, &capabilities); err != nil {
		return framework.Capabilities{}, err
	}
	return capabilities, nil
}

func (h *HookSender) Health() error {
	body, err := h.call("Health", struct{}{})
	if err != nil {
		return err
	}
	var response struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Status != "ok" {
		return errors.New("plugin health check returned an invalid status")
	}
	return nil
}

func (h *HookSender) ValidateConfig(payload framework.PluginRuntimeConfig) error {
	_, err := h.call("ValidateConfig", payload)
	return err
}

func (h *HookSender) Configure(payload framework.PluginRuntimeConfig) error {
	_, err := h.call("Configure", payload)
	return err
}

func (h *HookSender) ConfigureUserSettings(userID int, settings json.RawMessage) error {
	_, err := h.call("UserSettings", framework.UserSettingsRequest{
		UserID:   userID,
		Settings: append(json.RawMessage(nil), settings...),
	})
	return err
}

func (h *HookSender) GetBotInfo() (framework.BotInfo, error) {
	body, err := h.call("BotInfo", struct{}{})
	if err != nil {
		return framework.BotInfo{}, err
	}
	var info framework.BotInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return framework.BotInfo{}, err
	}
	return info, nil
}

func (h *HookSender) TestMessage(settings json.RawMessage) error {
	_, err := h.call("TestMessage", append(json.RawMessage(nil), settings...))
	return err
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

func NewHookSender(socketPath string, name string, serverVersion string, authToken ...string) *HookSender {
	httpc := http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			DialContext: func(ctx oContext.Context, network, addr string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}
	token := ""
	if len(authToken) > 0 {
		token = authToken[0]
	}
	return &HookSender{
		httpc:     httpc,
		socket:    socketPath,
		name:      name,
		authToken: token,
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

	definitions, err := pluginDefinitionsForStartup(cfg)
	if err != nil {
		log.Warn("Plugin loading is disabled because the configured plugin target is invalid")
		return
	}
	if len(definitions) == 0 {
		log.Info("Plugin loading is disabled because the allow list is empty")
		return
	}

	root, err := pluginRoot()
	if err != nil {
		log.Error("Plugin root resolve failed")
		return
	}
	if !securePluginDirectory(root) || !securePluginDirectoryOwner(root) {
		log.Error("Plugin directory permissions are unsafe")
		return
	}

	pluginNo := 1
	for _, definition := range definitions {
		logicalID := definition.LogicalID
		executableName, executableErr := pluginservice.ExecutableName(logicalID, runtime.GOOS)
		if executableErr != nil {
			log.Warnf("[%s] Plugin executable mapping is unavailable", logicalID)
			continue
		}
		pluginPath := filepath.Join(root, executableName)
		info, statErr := os.Lstat(pluginPath)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !isPluginExecutable(executableName, info.Mode()) || !securePluginFile(info.Mode()) || !securePluginFileOwner(info) {
			log.Warnf("[%s] Plugin file was rejected", logicalID)
			continue
		}
		socketName, socketErr := newPluginSocketName(pluginNo)
		if socketErr != nil {
			log.Warnf("[%s] Plugin socket name generation failed", logicalID)
			continue
		}
		authToken, tokenErr := newPluginAuthToken()
		if tokenErr != nil {
			log.Warnf("[%s] Plugin authentication material generation failed", logicalID)
			continue
		}
		env := pluginEnvironment(root, logicalID)
		env = appendPluginEnv(env, "SMAIL_PLUGIN_RPC_TOKEN", authToken)
		launchSpec, launchErr := newPluginLaunchSpec(pluginPath, root, executableName, socketName, env)
		if launchErr != nil {
			if !config.DevelopmentMode {
				log.Warnf("[%s] Plugin rejected because secure process isolation is unavailable: %v", logicalID, launchErr)
				continue
			}
			log.Warnf("[%s] Secure process isolation is unavailable in development mode: %v", logicalID, launchErr)
			launchSpec = compatibilityPluginLaunchSpec(pluginPath, root, executableName, socketName, env)
		}
		socketPath := launchSpec.socketPath
		if !isAbstractPluginSocket(socketPath) {
			if removeErr := os.Remove(socketPath); removeErr != nil && !os.IsNotExist(removeErr) {
				log.Warnf("[%s] Plugin socket cleanup failed", logicalID)
				launchSpec.cleanup()
				continue
			}
		}

		log.Infof("[%s] Plugin load", logicalID)
		process, startErr := os.StartProcess(launchSpec.path, launchSpec.args, launchSpec.procAttr)
		if startErr != nil {
			log.Warnf("[%s] Plugin start failed", logicalID)
			launchSpec.cleanup()
			continue
		}
		launchSpec.closeParentFiles()
		if limitErr := applyPluginResourceLimits(process); limitErr != nil {
			log.Warnf("[%s] Plugin resource limits could not be applied", logicalID)
			_ = process.Kill()
			_, _ = process.Wait()
			launchSpec.cleanup()
			continue
		}
		processListMu.Lock()
		processList = append(processList, process)
		processListMu.Unlock()
		pluginNo++

		go waitPlugin(process, logicalID, socketPath, launchSpec)

		if !waitPluginSocket(socketPath) {
			log.Warnf("[%s] Plugin socket was not created", logicalID)
			_ = process.Kill()
			continue
		}
		if runtime.GOOS != "windows" && !isAbstractPluginSocket(socketPath) {
			if chmodErr := os.Chmod(socketPath, 0600); chmodErr != nil {
				log.Warnf("[%s] Plugin socket permissions could not be tightened", logicalID)
				_ = process.Kill()
				continue
			}
		}

		hook := NewHookSender(socketPath, logicalID, serverVersion, authToken)
		hook.executable = executableName
		capabilities, capabilityErr := hook.Capabilities()
		if capabilityErr != nil || !pluginCapabilitiesMatch(definition, capabilities) {
			log.Warnf("[%s] Plugin capability handshake failed", logicalID)
			_ = process.Kill()
			continue
		}
		if instance, generation, ok := pluginInstanceForStartup(logicalID); ok {
			payload, payloadErr := pluginRuntimeConfig(logicalID, generation, instance)
			if payloadErr != nil || hook.ValidateConfig(payload) != nil || hook.Configure(payload) != nil {
				log.Warnf("[%s] Plugin runtime configuration failed", logicalID)
				_ = process.Kill()
				continue
			}
		}
		syncTelegramUserSettings(logicalID, hook)
		if healthErr := hook.Health(); healthErr != nil {
			log.Warnf("[%s] Plugin health check failed", logicalID)
			_ = process.Kill()
			continue
		}

		hookName := strings.TrimSpace(hook.GetName(&context.Context{}))
		if hookName == "" || !RegisterHook(hookName, hook) {
			log.Warnf("[%s] Plugin reported an invalid hook name", logicalID)
			_ = process.Kill()
			continue
		}
		log.Infof("[%s] Plugin load success", hookName)
	}
}

func SyncTelegramUserSettings(userID int, settings pluginservice.TelegramUserSettings) error {
	if userID <= 0 {
		return errors.New("invalid user ID")
	}
	hook, ok := GetHook("TelegramPush")
	if !ok {
		hook, ok = GetHook("telegram_push")
	}
	if !ok {
		return errors.New("telegram plugin is unavailable")
	}
	configurer, ok := hook.(framework.UserSettingsConfigurer)
	if !ok {
		return errors.New("telegram plugin does not support user settings")
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return configurer.ConfigureUserSettings(userID, payload)
}
func syncTelegramUserSettings(logicalID string, hook *HookSender) {
	if logicalID != "telegram_push" || hook == nil || pluginConfigService == nil {
		return
	}
	entries, err := pluginConfigService.ListTelegramUserSettings()
	if err != nil {
		log.WithError(err).Warnf("[%s] Telegram user settings sync load failed", logicalID)
		return
	}
	for _, entry := range entries {
		if syncErr := syncTelegramUserSetting(hook, entry.UserID, entry.Settings); syncErr != nil {
			log.WithError(syncErr).Warnf("[%s] Telegram user settings sync failed for user %d", logicalID, entry.UserID)
		}
	}
}

func syncTelegramUserSetting(hook *HookSender, userID int, settings pluginservice.TelegramUserSettings) error {
	if hook == nil || userID <= 0 {
		return errors.New("invalid plugin user settings target")
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	return hook.ConfigureUserSettings(userID, payload)
}
func pluginInstanceForStartup(logicalID string) (config.PluginInstance, uint64, bool) {
	manager := config.CurrentManager()
	if manager == nil {
		return config.PluginInstance{}, 0, false
	}
	snapshot := manager.Snapshot()
	instance, ok := snapshot.Plugins.Plugins[logicalID]
	return instance, snapshot.Generation, ok
}

func pluginRuntimeConfig(logicalID string, generation uint64, instance config.PluginInstance) (framework.PluginRuntimeConfig, error) {
	if err := validatePluginRuntimeBinding(logicalID, instance); err != nil {
		return framework.PluginRuntimeConfig{}, err
	}
	if generation == 0 {
		return framework.PluginRuntimeConfig{}, errors.New("plugin configuration generation is invalid")
	}
	result := framework.PluginRuntimeConfig{
		PluginID:     logicalID,
		Generation:   generation,
		Settings:     append(json.RawMessage(nil), instance.Settings...),
		Capabilities: append([]string(nil), instance.Permissions.Capabilities...),
		Network:      make([]framework.PluginNetworkTarget, 0, len(instance.Permissions.Network)),
		Secrets:      make(map[string]string),
	}
	for _, target := range instance.Permissions.Network {
		result.Network = append(result.Network, framework.PluginNetworkTarget{Scheme: target.Scheme, Host: target.Host, Port: target.Port})
	}
	if pluginConfigService == nil {
		if instance.Secret.Configured() {
			return framework.PluginRuntimeConfig{}, errors.New("plugin secret service is unavailable")
		}
		return result, nil
	}
	fieldName := ""
	switch logicalID {
	case "wechat_push":
		fieldName = "appSecret"
	case "telegram_push":
		fieldName = "botToken"
	}
	if fieldName != "" {
		secret, err := pluginConfigService.OpenSecretForPlugin(logicalID, fieldName)
		if err != nil {
			return framework.PluginRuntimeConfig{}, err
		}
		result.Secrets[fieldName] = secret
	}
	return result, nil
}

// validatePluginRuntimeBinding 在启动插件前再次校验配置与插件目录声明一致。
// 该校验不能替代 ConfigManager 的完整校验，只用于阻止错误或过期快照被下发。
func validatePluginRuntimeBinding(logicalID string, instance config.PluginInstance) error {
	if !instance.Enabled {
		return errors.New("plugin is disabled")
	}
	definition, ok := pluginservice.Lookup(logicalID)
	if !ok {
		return pluginservice.ErrUnknownPluginID
	}
	if !sameStringSet(instance.Permissions.Capabilities, definition.Capabilities) {
		return errors.New("plugin capabilities do not match catalog")
	}
	if instance.Secret.Ciphertext != "" || instance.Secret.Version != 0 || !instance.Secret.UpdatedAt.IsZero() {
		return errors.New("legacy plugin secret migration is required")
	}
	return nil
}

func pluginCapabilitiesMatch(definition pluginservice.Definition, capabilities framework.Capabilities) bool {
	return capabilities.PluginID == definition.LogicalID &&
		capabilities.Protocol == 1 &&
		sameStringSet(capabilities.Capabilities, definition.HandshakeCapabilities) &&
		sameStringSet(capabilities.Settings, definition.SettingsFields)
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	values := make(map[string]struct{}, len(left))
	for _, value := range left {
		if _, exists := values[value]; exists {
			return false
		}
		values[value] = struct{}{}
	}
	for _, value := range right {
		if _, exists := values[value]; !exists {
			return false
		}
	}
	return true
}

func pluginDefinitionsForStartup(legacy *config.Config) ([]pluginservice.Definition, error) {
	if manager := config.CurrentManager(); manager != nil {
		return pluginDefinitionsFromRuntime(manager.Snapshot())
	}
	if legacy == nil {
		return nil, errors.New("plugin configuration is unavailable")
	}
	return pluginservice.EnabledDefinitions(legacy.PluginAllowList, runtime.GOOS)
}

func pluginDefinitionsFromRuntime(runtimeConfig config.RuntimeConfig) ([]pluginservice.Definition, error) {
	values := make([]string, 0, len(runtimeConfig.Plugins.Plugins))
	for logicalID, instance := range runtimeConfig.Plugins.Plugins {
		if instance.Enabled {
			values = append(values, logicalID)
		}
	}
	return pluginservice.EnabledDefinitions(values, runtime.GOOS)
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

func newPluginAuthToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	return hex.EncodeToString(token), nil
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
	if config.CurrentManager() != nil {
		return ret
	}
	cfg := config.CurrentConfig()
	if cfg == nil {
		return ret
	}
	pluginName := strings.ToLower(strings.TrimSpace(name))
	if pluginName == "wechat_push" {
		ret = appendPluginEnv(ret, "SMAIL_WECHAT_APP_ID", cfg.WeChatPushAppId)
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
	if bundledPlugins != nil {
		return errors.New("内置插件配置必须通过 plugins.json 配置服务保存")
	}
	root, err := pluginRoot()
	if err != nil || !securePluginDirectory(root) || !securePluginDirectoryOwner(root) {
		return errors.New("plugin directory is unsafe")
	}
	executableName := sender.executable
	if executableName == "" {
		executableName = sender.name
	}
	configPath := filepath.Join(root, pluginConfigFileName(executableName))
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
