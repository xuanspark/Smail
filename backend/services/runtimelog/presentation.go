package runtimelog

import (
	"regexp"
	"strings"
)

var legacyStartupLine = regexp.MustCompile(`^(\[[^\]\r\n]+\]\s+\[info\]\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\](?:\[[^\]\r\n]*\])?\[([^\]\r\n]+):\d+\])([^\r\n]*)$`)

// presentLine 只精简旧版主程序的启动横幅，不修改磁盘原文或隐藏其他来源和级别的记录。
func presentLine(line string) (string, bool) {
	parts := legacyStartupLine.FindStringSubmatch(line)
	if parts == nil {
		return line, true
	}
	source := strings.ReplaceAll(parts[2], `\`, "/")
	if source != "internal/app/app.go" && !strings.HasSuffix(source, "/internal/app/app.go") {
		return line, true
	}
	message := strings.TrimSpace(parts[3])
	if len(message) >= 3 && strings.Trim(message, "*") == "" {
		return "", false
	}
	if !strings.HasPrefix(message, "***\t") && !strings.HasPrefix(message, "*** ") {
		return line, true
	}
	message = strings.TrimSpace(message[3:])
	for _, prefix := range []string{"Server Version:", "Git Commit Hash:", "Build Date:", "Build GoLang Version:"} {
		if strings.HasPrefix(message, prefix) {
			return "", false
		}
	}
	return line, true
}
