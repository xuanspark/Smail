package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var defaultClient = &http.Client{
	Timeout: 15 * time.Second,
}

var apiBaseURL = "https://translate.googleapis.com/translate_a/single"

// Result 包含翻译后内容及源语言
type Result struct {
	OriginalText   string `json:"original_text"`
	TranslatedText string `json:"translated_text"`
	SourceLang     string `json:"source_lang"`
	TargetLang     string `json:"target_lang"`
}

// SetApiBaseURL 设置翻译引擎基础地址（主要用于单元测试与代理）
func SetApiBaseURL(u string) {
	if u != "" {
		apiBaseURL = u
	}
}

// SetDefaultClient 设置 HTTP 客户端（主要用于单元测试与超时配置）
func SetDefaultClient(c *http.Client) {
	if c != nil {
		defaultClient = c
	}
}

// TranslateText 调用免配置翻译引擎将文本翻译为目标语言（默认 zh-CN）
func TranslateText(ctx context.Context, text string, targetLang string) (*Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return &Result{
			OriginalText:   "",
			TranslatedText: "",
			SourceLang:     "auto",
			TargetLang:     targetLang,
		}, nil
	}

	if targetLang == "" {
		targetLang = "zh-CN"
	}

	apiURL := fmt.Sprintf("%s?client=gtx&sl=auto&tl=%s&dt=t", apiBaseURL, url.QueryEscape(targetLang))
	formData := url.Values{}
	formData.Set("q", text)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建翻译请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求翻译服务失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("翻译服务返回异常状态码 %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取翻译响应失败: %w", err)
	}

	var rawResponse []any
	if err := json.Unmarshal(bodyBytes, &rawResponse); err != nil {
		return nil, fmt.Errorf("解析翻译响应结构失败: %w", err)
	}

	if len(rawResponse) == 0 {
		return nil, errors.New("翻译响应格式为空")
	}

	sentences, ok := rawResponse[0].([]any)
	if !ok {
		return nil, errors.New("未找到译文段落列表")
	}

	var translatedSb strings.Builder
	for _, item := range sentences {
		if sentenceArr, ok := item.([]any); ok && len(sentenceArr) > 0 {
			if seg, ok := sentenceArr[0].(string); ok {
				translatedSb.WriteString(seg)
			}
		}
	}

	sourceLang := "auto"
	if len(rawResponse) > 2 {
		if sl, ok := rawResponse[2].(string); ok && sl != "" {
			sourceLang = sl
		}
	}

	return &Result{
		OriginalText:   text,
		TranslatedText: translatedSb.String(),
		SourceLang:     sourceLang,
		TargetLang:     targetLang,
	}, nil
}
