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
		return "", errors.New("protected password input requires an interactive console")
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
		return "", errors.New("password is empty or does not match")
	}
	clearPassword(&second)
	return first, nil
}

func readWindowsPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt+": ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", errors.New("protected password input failed")
	}
	normalized := strings.TrimSpace(string(value))
	for i := range value {
		value[i] = 0
	}
	if len([]byte(normalized)) < 8 || len([]byte(normalized)) > 256 {
		return "", errors.New("invalid password length")
	}
	return normalized, nil
}
