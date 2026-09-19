//go:build windows

package adminreset

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"git.pcux.cn/xuans/Smail/utils/password"

	"golang.org/x/term"
)

func ReadProtectedPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("受保护的密码输入需要交互式控制台")
	}
	first, err := readWindowsPassword("请输入新的超级管理员密码")
	if err != nil {
		return "", err
	}
	second, err := readWindowsPassword("请再次输入新的超级管理员密码")
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

func readWindowsPassword(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("受保护的密码输入需要交互式控制台")
	}
	fmt.Fprint(os.Stderr, prompt+": ")
	valueBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", errors.New("受保护的密码输入失败")
	}
	value := string(valueBytes)
	for index := range valueBytes {
		valueBytes[index] = 0
	}
	if password.ValidateUserPassword(value) != nil {
		return "", errors.New("密码长度必须为 8 到 12 个字符")
	}
	return value, nil
}

func ReadVisibleInput(prompt string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("安全验证需要交互式控制台")
	}
	fmt.Fprint(os.Stderr, prompt+": ")
	value, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", errors.New("读取安全验证输入失败")
	}
	return strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r"), nil
}
