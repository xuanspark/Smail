package runtimelog

import (
	"regexp"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/logpolicy"
)

var contentLine = regexp.MustCompile(`^\[[^\]\r\n]+\]\s+\[(info|debug|trace)\]\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\](?:\[[^\]\r\n]*\])?\[([^\]\r\n]+):\d+\]([^\r\n]*)$`)

// presentLine 先过滤明确无内容的历史记录；警告、错误和未知格式均保留原文。
func presentLine(line string) (string, bool) {
	parts := contentLine.FindStringSubmatch(line)
	if parts == nil {
		if end := strings.Index(line, "] "); end >= 0 && logpolicy.EmptyMessage(line[end+2:]) {
			return "", false
		}
		return line, true
	}
	if logpolicy.EmptyMessage(parts[3]) || (parts[1] == "info" && legacyStartupNoise(parts[2], parts[3])) {
		return "", false
	}
	return line, true
}

func legacyStartupNoise(source, message string) bool {
	source = strings.ReplaceAll(source, `\`, "/")
	if source != "internal/app/app.go" && !strings.HasSuffix(source, "/internal/app/app.go") {
		return false
	}
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "***\t") && !strings.HasPrefix(message, "*** ") {
		return false
	}
	message = strings.TrimSpace(message[3:])
	for _, prefix := range []string{"Server Version:", "Git Commit Hash:", "Build Date:", "Build GoLang Version:"} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}
