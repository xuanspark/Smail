//go:build linux

package adminreset

import (
	"errors"
	"io"
	"os/exec"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/password"
)

func ReadProtectedPassword() (string, error) {
	first, err := askPassword("请输入新的超级管理员密码")
	if err != nil {
		return "", err
	}
	second, err := askPassword("请再次输入新的超级管理员密码")
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
	value, err := readSystemdInput(prompt, "no")
	if err != nil {
		return "", err
	}
	if password.ValidateUserPassword(value) != nil {
		return "", errors.New("密码长度无效")
	}
	return value, nil
}

func ReadVisibleInput(prompt string) (string, error) {
	return readSystemdInput(prompt, "yes")
}

func readSystemdInput(prompt, echo string) (string, error) {
	prompt = strings.TrimRight(prompt, ":： ") + "："
	// systemd 代理把提示保存在单行配置中，用终端换行控制显示列表，避免配置被拆行。
	prompt = strings.ReplaceAll(strings.ReplaceAll(prompt, "\r\n", "\n"), "\n", "\x1bE")
	command := exec.Command("systemd-ask-password", "--no-tty", "--echo="+echo, prompt)
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
