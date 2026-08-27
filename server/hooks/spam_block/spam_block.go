package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/hooks/framework"
	"git.pcux.cn/xuans/Smail/hooks/spam_block/tools"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/credential"
	log "github.com/sirupsen/logrus"
	"html/template"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	maxSpamBlockSourceBytes    = 1 << 20
	maxSpamBlockModelTextBytes = 32 << 10
)

type SpamBlock struct {
	mu  sync.RWMutex
	cfg SpamBlockConfig
	hc  *http.Client
}

func (s *SpamBlock) SendBefore(ctx *context.Context, email *parsemail.Email) {

}

func (s *SpamBlock) SendAfter(ctx *context.Context, email *parsemail.Email, err map[string]error) {

}

func (s *SpamBlock) ReceiveParseBefore(ctx *context.Context, email *[]byte) {

}

// GetName 获取插件名称
func (s *SpamBlock) GetName(ctx *context.Context) string {
	return "SpamBlock"
}

//go:embed static/index.html
var index string

//go:embed static/jquery.js
var jquery string

// SettingsHtml 插件页面
func (s *SpamBlock) SettingsHtml(ctx *context.Context, url string, requestData string) string {

	if strings.Contains(url, "jquery.js") {
		return jquery
	}

	if strings.Contains(url, "index.html") {
		if !ctx.IsAdmin {
			return fmt.Sprintf(`
<div>
	Please contact the administrator for configuration.
</div>
`)
		}
		s.mu.RLock()
		cfg := s.cfg
		s.mu.RUnlock()
		return fmt.Sprintf(index, template.HTMLEscapeString(cfg.ApiURL), cfg.ApiTimeout, cfg.Threshold)
	}
	if !ctx.IsAdmin {
		return "No Access Privileges"
	}

	cfg, err := decodeSpamBlockConfig([]byte(requestData))
	if err != nil {
		return err.Error()
	}

	s.mu.Lock()
	s.cfg = cfg
	s.hc = newHTTPClient(cfg.ApiTimeout)
	s.mu.Unlock()

	return "success"

}

type ModelResponse struct {
	Predictions [][]float64 `json:"predictions"`
}

type ApiRequest struct {
	Instances []InstanceItem `json:"instances"`
}

type InstanceItem struct {
	Token []string `json:"token"`
}

func (s *SpamBlock) ReceiveParseAfter(ctx *context.Context, email *parsemail.Email) {
	if email == nil {
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	client := s.hc
	s.mu.RUnlock()
	if cfg.ApiURL == "" {
		return
	}
	if client == nil {
		client = newHTTPClient(cfg.ApiTimeout)
	}

	reqData := ApiRequest{
		Instances: []InstanceItem{
			{
				Token: []string{buildSpamBlockModelText(email)},
			},
		},
	}

	str, err := json.Marshal(reqData)
	if err != nil {
		log.WithContext(ctx).Error("spam block request build failed")
		return
	}
	resp, err := client.Post(cfg.ApiURL, "application/json", strings.NewReader(string(str)))
	if err != nil || resp == nil {
		log.WithContext(ctx).Error("spam block request failed")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		log.WithContext(ctx).Error("spam block request was rejected")
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil || len(body) > 1<<20 {
		log.WithContext(ctx).Error("spam block response is invalid")
		return
	}

	modelResponse := ModelResponse{}
	err = json.Unmarshal(body, &modelResponse)
	if err != nil {
		log.WithContext(ctx).Error("spam block response is invalid")
		return
	}

	if len(modelResponse.Predictions) == 0 {
		log.WithContext(ctx).Error("spam block response has no prediction")
		return
	}

	classes := modelResponse.Predictions[0]
	if len(classes) != 3 {
		return
	}
	var maxScore float64
	var maxClass int
	for i, score := range classes {
		if score > maxScore {
			maxScore = score
			maxClass = i
		}
	}

	switch maxClass {
	case 0:
		log.WithContext(ctx).Infof("spam block result: normal score=%f", maxScore)
	case 1:
		log.WithContext(ctx).Infof("spam block result: spam score=%f", maxScore)
	case 2:
		log.WithContext(ctx).Infof("spam block result: blackmail score=%f", maxScore)
	}

	if maxClass != 0 && maxScore > cfg.Threshold/100 {
		if maxClass == 2 {
			email.Status = 3
		} else {
			email.Status = 5
		}
	}
}

func (s *SpamBlock) ReceiveSaveAfter(ctx *context.Context, email *parsemail.Email, ue []*models.UserEmail) {

}

func buildSpamBlockModelText(email *parsemail.Email) string {
	if email == nil {
		return ""
	}
	html := truncateUTF8(string(limitSpamBlockBytes(email.HTML)), maxSpamBlockSourceBytes)
	content := tools.Trim(tools.TrimHtml(html))
	if content == "" {
		content = tools.Trim(string(limitSpamBlockBytes(email.Text)))
	}
	return joinSpamBlockText(email.Subject, content)
}

func joinSpamBlockText(subject string, content string) string {
	subject = truncateUTF8(strings.TrimSpace(subject), maxSpamBlockModelTextBytes)
	content = strings.TrimSpace(content)
	if subject == "" {
		return truncateUTF8(content, maxSpamBlockModelTextBytes)
	}
	remaining := maxSpamBlockModelTextBytes - len(subject)
	if content == "" || remaining <= 1 {
		return subject
	}
	return subject + " " + truncateUTF8(content, remaining-1)
}

func limitSpamBlockBytes(value []byte) []byte {
	if len(value) <= maxSpamBlockSourceBytes {
		return value
	}
	return value[:maxSpamBlockSourceBytes]
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

type SpamBlockConfig struct {
	ApiURL     string  `json:"url"`
	ApiTimeout int     `json:"timeout"` // 单位毫秒
	Threshold  float64 `json:"threshold"`
}

func NewSpamBlockHook() *SpamBlock {
	pluginConfig := defaultSpamBlockConfig()
	if cfgData, err := os.ReadFile("./plugins/spam_block_config.json"); err == nil {
		parsed, parseErr := decodeSpamBlockConfig(cfgData)
		if parseErr != nil {
			log.Error("spam block configuration is invalid")
		} else {
			pluginConfig = parsed
		}
	}

	return &SpamBlock{
		cfg: pluginConfig,
		hc:  newHTTPClient(pluginConfig.ApiTimeout),
	}
}

func defaultSpamBlockConfig() SpamBlockConfig {
	return SpamBlockConfig{ApiTimeout: credential.NormalizeTimeoutMS(0), Threshold: 20}
}

func decodeSpamBlockConfig(data []byte) (SpamBlockConfig, error) {
	type payload struct {
		ApiURL     string          `json:"url"`
		ApiTimeout json.RawMessage `json:"timeout"`
		Threshold  json.RawMessage `json:"threshold"`
	}
	var raw payload
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return SpamBlockConfig{}, fmt.Errorf("invalid configuration")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return SpamBlockConfig{}, fmt.Errorf("invalid configuration")
	}
	defaults := defaultSpamBlockConfig()
	timeout, err := parseConfigInt(raw.ApiTimeout, defaults.ApiTimeout)
	if err != nil {
		return SpamBlockConfig{}, fmt.Errorf("invalid timeout")
	}
	threshold, err := parseConfigFloat(raw.Threshold, defaults.Threshold)
	if err != nil || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return SpamBlockConfig{}, fmt.Errorf("invalid threshold")
	}
	return normalizeConfig(SpamBlockConfig{ApiURL: raw.ApiURL, ApiTimeout: timeout, Threshold: threshold})
}

func parseConfigInt(raw json.RawMessage, fallback int) (int, error) {
	value, err := configNumber(raw)
	if err != nil || value == "" {
		return fallback, err
	}
	return strconv.Atoi(value)
}

func parseConfigFloat(raw json.RawMessage, fallback float64) (float64, error) {
	value, err := configNumber(raw)
	if err != nil || value == "" {
		return fallback, err
	}
	return strconv.ParseFloat(value, 64)
}

func configNumber(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", err
		}
		return strings.TrimSpace(value), nil
	}
	return string(raw), nil
}

func normalizeConfig(cfg SpamBlockConfig) (SpamBlockConfig, error) {
	cfg.ApiTimeout = credential.NormalizeTimeoutMS(cfg.ApiTimeout)
	if cfg.Threshold < 0 || cfg.Threshold > 100 {
		return SpamBlockConfig{}, fmt.Errorf("invalid threshold")
	}
	if cfg.ApiURL == "" {
		return cfg, nil
	}
	endpoint, err := credential.NormalizeHTTPSURL(cfg.ApiURL)
	if err != nil {
		return SpamBlockConfig{}, err
	}
	cfg.ApiURL = endpoint
	return cfg, nil
}

func newHTTPClient(timeout int) *http.Client {
	return credential.NewRestrictedHTTPClient(timeout)
}

func main() {
	log.Infof("SpamBlockPlug Star Success")
	instance := NewSpamBlockHook()
	if instance == nil {
		return
	}
	if err := framework.CreatePlugin("spam_block", instance).Run(); err != nil {
		log.WithError(err).Error("spam block plugin stopped")
		os.Exit(1)
	}
}
