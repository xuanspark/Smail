package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/internal/buildinfo"
	"git.pcux.cn/xuans/Smail/services/adminreset"
)

func main() {
	code := run(os.Args[1:])
	if code != 0 {
		os.Exit(code)
	}
}

func run(args []string) int {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Println(buildinfo.Current().Version)
		return 0
	}
	if len(args) == 1 && (args[0] == "doctor" || args[0] == "--doctor") {
		return runDoctor()
	}
	if len(args) != 0 || hasPasswordArgument(args) {
		fmt.Fprintln(os.Stderr, "smail-admin 只允许由 systemctl start smail-admin 无参数启动")
		return 2
	}
	if err := adminreset.AuthorizeCurrentProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "超级管理员重置已拒绝：当前进程不是受信任的部署管理员")
		return 1
	}
	password, err := adminreset.ReadProtectedPassword()
	if err != nil {
		fmt.Fprintln(os.Stderr, "超级管理员重置已拒绝：", err)
		return 1
	}
	defer clearString(&password)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := adminreset.DialControl(ctx, adminreset.DefaultControlPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "超级管理员重置通道不可用")
		return 1
	}
	defer conn.Close()
	if err := adminreset.SendReset(ctx, conn, password); err != nil {
		fmt.Fprintln(os.Stderr, "超级管理员重置失败：", safeError(err))
		return 1
	}
	fmt.Println("超级管理员密码重置成功")
	return 0
}

func runDoctor() int {
	report := adminreset.CheckEnvironment()
	status := "正常"
	if !report.Supported {
		status = "不支持"
	} else if !report.Privileged || !report.ServiceManaged || !report.PasswordAgentAvailable {
		status = "不满足重置条件"
	}
	fmt.Printf("平台：%s\n重置支持：%t\n部署管理员身份：%t\n系统服务上下文：%t\n密码代理：%t\n控制通道：%s\n状态：%s\n",
		report.Platform,
		report.Supported,
		report.Privileged,
		report.ServiceManaged,
		report.PasswordAgentAvailable,
		report.ControlPath,
		status,
	)
	return 0
}

func hasPasswordArgument(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if strings.Contains(lower, "password") || strings.Contains(lower, "passwd") || strings.Contains(lower, "secret") {
			return true
		}
	}
	return false
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, adminreset.ErrInvalidRequest) || errors.Is(err, adminreset.ErrUnsupported) {
		return "请求无效或当前平台不支持"
	}
	return "本机受保护通道拒绝了请求"
}

func clearString(value *string) {
	if value == nil {
		return
	}
	buffer := []byte(*value)
	for i := range buffer {
		buffer[i] = 0
	}
	*value = ""
}
