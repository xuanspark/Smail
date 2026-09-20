//go:build linux

package adminreset

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"git.pcux.cn/xuans/Smail/utils/password"
)

const systemdInputAgentPath = "/usr/bin/systemd-ask-password"

func ReadProtectedPassword() (string, error) {
	return ReadProtectedPasswordContext(context.Background())
}

func ReadProtectedPasswordContext(ctx context.Context) (string, error) {
	first, err := askPasswordContext(ctx, "请输入新的超级管理员密码")
	if err != nil {
		return "", err
	}
	second, err := askPasswordContext(ctx, "请再次输入新的超级管理员密码")
	if err != nil {
		clearPassword(&first)
		return "", err
	}
	if first == "" || first != second {
		clearPassword(&first)
		clearPassword(&second)
		return "", errors.New("密码为空或两次输入不一致")
	}
	clearPassword(&second)
	return first, nil
}

func askPassword(prompt string) (string, error) {
	return askPasswordContext(context.Background(), prompt)
}

func askPasswordContext(ctx context.Context, prompt string) (string, error) {
	value, err := readSystemdInputContext(ctx, prompt, "no")
	if err != nil {
		return "", err
	}
	if password.ValidateUserPassword(value) != nil {
		return "", errors.New("密码长度无效")
	}
	return value, nil
}

func ReadVisibleInput(prompt string) (string, error) {
	return ReadVisibleInputContext(context.Background(), prompt)
}

func ReadVisibleInputContext(ctx context.Context, prompt string) (string, error) {
	return readSystemdInputContext(ctx, prompt, "yes")
}

// ShowResult 保留隔离密码代理的测试入口，正式流程使用绑定终端的上下文。
func ShowResult(message string) {
	ShowResultContext(context.Background(), message)
}

func ShowResultContext(ctx context.Context, message string) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, _ = readSystemdInputContext(ctx, message+"\n按回车结束", "yes", "--timeout=30")
}

func readSystemdInput(prompt, echo string) (string, error) {
	return readSystemdInputContext(context.Background(), prompt, echo)
}

func readSystemdInputContext(ctx context.Context, prompt, echo string, options ...string) (string, error) {
	if terminal, ok := ctx.Value(invocationTerminalKey{}).(*invocationTerminal); ok {
		return terminal.read(ctx, prompt, echo == "no")
	}
	prompt = strings.TrimRight(prompt, ":： ") + "："
	// systemd 代理把提示保存在单行配置中，用终端换行控制显示列表，避免配置被拆行。
	prompt = strings.ReplaceAll(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n", "\x1bE")
	args := append([]string{"--no-tty", "--echo=" + echo}, options...)
	command := systemdInputCommand(ctx, append(args, prompt)...)
	// 先允许代理清理请求文件，避免取消后继续弹出已经失效的输入提示。
	command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
	command.WaitDelay = time.Second
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		return "", errors.New("受保护密码代理不可用")
	}
	output = stripPasswordLineEnding(output)
	value := string(output)
	defer clearBytes(output)
	return value, nil
}

func systemdInputCommand(ctx context.Context, args ...string) *exec.Cmd {
	// 管理员流程不能从调用环境的 PATH 解析高权限密码代理。
	return exec.CommandContext(ctx, systemdInputAgentPath, args...)
}

func stripPasswordLineEnding(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
	}
	if len(value) > 0 && value[len(value)-1] == '\r' {
		value = value[:len(value)-1]
	}
	return value
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
