package main

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"golang.org/x/net/html"
)

const maxWeChatVerificationScanBytes = 64 << 10

const weChatVerificationKeyword = `(?:验证码|校验码|动态码|认证码|一次性(?:密码|口令)|verification[\s-]*code|security[\s-]*code|authentication[\s-]*code|one[\s-]*time[\s-]*(?:password|code)|passcode|\botp\b)`

var weChatVerificationPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)` + weChatVerificationKeyword + `(?:[[:space:][:punct:]：，。！【】（）]|是|为|is|equals|如下|your|please|use){0,20}\b([A-Z0-9]{4,8})\b`),
	regexp.MustCompile(`(?i)\b([A-Z0-9]{4,8})\b(?:[[:space:][:punct:]：，。！【】（）]|是|为|is|your|您的){0,20}` + weChatVerificationKeyword),
}

func extractWeChatVerificationCode(email *parsemail.Email) (string, bool) {
	if email == nil {
		return "", false
	}
	// 分别检查标题和各正文，避免跨字段拼接出不存在的验证码。
	if code, ok := findWeChatVerificationCode(boundedWeChatVerificationText(email.Subject)); ok {
		return code, true
	}
	text := email.Text[:min(len(email.Text), maxWeChatVerificationScanBytes)]
	plain := string(text)
	if len(email.Text) > len(text) {
		plain += "_"
	}
	if code, ok := findWeChatVerificationCode(plain); ok {
		return code, true
	}
	return findWeChatVerificationCode(weChatHTMLNotificationText(email.HTML))
}

func findWeChatVerificationCode(content string) (string, bool) {
	content = strings.Map(func(char rune) rune {
		if unicode.IsSpace(char) {
			return ' '
		}
		return char
	}, content)
	for _, pattern := range weChatVerificationPatterns {
		for _, match := range pattern.FindAllStringSubmatch(content, -1) {
			code := match[1]
			// 排除普通英文单词，只识别至少含一个数字的四至八位字母数字组合。
			if strings.ContainsAny(code, "0123456789") {
				return code, true
			}
		}
	}
	return "", false
}

func boundedWeChatVerificationText(content string) string {
	if len(content) > maxWeChatVerificationScanBytes {
		// 用标识符边界阻止截断的长串被误识别为完整验证码，不进入通知正文。
		return truncateWeChatText(content, maxWeChatVerificationScanBytes) + "_"
	}
	return content
}

func weChatHTMLNotificationText(content []byte) string {
	// 限制原始输入，避免大邮件在通知提取阶段产生无界扫描和额外分配。
	truncated := len(content) > maxWeChatVerificationScanBytes
	content = content[:min(len(content), maxWeChatVerificationScanBytes)]
	tokenizer := html.NewTokenizer(bytes.NewReader(content))
	var text strings.Builder
	hiddenDepth := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if truncated {
				text.WriteByte('_')
			}
			return text.String()
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			if weChatHiddenHTMLTag(string(name)) {
				hiddenDepth++
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if hiddenDepth > 0 && weChatHiddenHTMLTag(string(name)) {
				hiddenDepth--
			}
		case html.TextToken:
			if hiddenDepth == 0 {
				// 分词器负责解码实体；不读取属性、注释、脚本及样式中的内容。
				text.WriteByte(' ')
				text.Write(tokenizer.Text())
			}
		}
	}
}

func weChatHiddenHTMLTag(name string) bool {
	switch name {
	case "head", "script", "style", "template":
		return true
	default:
		return false
	}
}
