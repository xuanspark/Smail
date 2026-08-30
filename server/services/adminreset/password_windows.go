//go:build windows

package adminreset

import (
	"errors"
	"fmt"
	"os"

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
	fmt.Fprint(os.Stderr, prompt+": ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", errors.New("受保护的密码输入失败")
	}
	normalized := string(value)
	for i := range value {
		value[i] = 0
	}
	if password.ValidateUserPassword(normalized) != nil {
		return "", errors.New("\u5bc6\u7801\u957f\u5ea6\u5fc5\u987b\u4e3a 8 \u5230 12 \u4e2a\u5b57\u7b26")
	}
	return normalized, nil
}
