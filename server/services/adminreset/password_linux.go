//go:build linux

package adminreset

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"strings"
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
	command := exec.Command("systemd-ask-password", "--no-tty", prompt)
	command.Stderr = io.Discard
	output, err := command.Output()
	if err != nil {
		return "", errors.New("受保护密码代理不可用")
	}
	output = bytes.TrimSpace(output)
	if len(output) == 0 || len(output) > 256 {
		return "", errors.New("密码长度无效")
	}
	return strings.TrimSpace(string(output)), nil
}
