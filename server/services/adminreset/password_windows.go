//go:build windows

package adminreset

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func ReadProtectedPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("\u53d7\u4fdd\u62a4\u7684\u5bc6\u7801\u8f93\u5165\u9700\u8981\u4ea4\u4e92\u5f0f\u63a7\u5236\u53f0")
	}
	first, err := readWindowsPassword("\u8bf7\u8f93\u5165\u65b0\u7684\u8d85\u7ea7\u7ba1\u7406\u5458\u5bc6\u7801")
	if err != nil {
		return "", err
	}
	second, err := readWindowsPassword("\u8bf7\u518d\u6b21\u8f93\u5165\u65b0\u7684\u8d85\u7ea7\u7ba1\u7406\u5458\u5bc6\u7801")
	if err != nil {
		clearPassword(&first)
		return "", err
	}
	if first == "" || first != second {
		clearPassword(&first)
		clearPassword(&second)
		return "", errors.New("\u5bc6\u7801\u4e3a\u7a7a\u6216\u4e24\u6b21\u8f93\u5165\u4e0d\u4e00\u81f4")
	}
	clearPassword(&second)
	return first, nil
}

func readWindowsPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt+": ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", errors.New("\u53d7\u4fdd\u62a4\u7684\u5bc6\u7801\u8f93\u5165\u5931\u8d25")
	}
	normalized := strings.TrimSpace(string(value))
	for i := range value {
		value[i] = 0
	}
	if len([]byte(normalized)) < 8 || len([]byte(normalized)) > 256 {
		return "", errors.New("\u5bc6\u7801\u957f\u5ea6\u5fc5\u987b\u4e3a 8 \u5230 256 \u4e2a\u5b57\u7b26")
	}
	return normalized, nil
}
