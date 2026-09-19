//go:build linux

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/services/adminreset"
)

// 由隔离的 systemd 临时服务人工检查终端显示，不连接生产重置通道。
func TestSystemdResultPrompt(t *testing.T) {
	if os.Getenv("SMAIL_TEST_RESULT_PROMPT") != "1" || os.Getenv("INVOCATION_ID") == "" {
		t.Skip("需要隔离的 systemd 交互测试服务")
	}
	if code := finishReset("显示测试：重置结果提示正常", 0); code != 0 {
		t.Fatalf("退出状态：%d", code)
	}
}

func TestSystemdCancelledInputResult(t *testing.T) {
	if os.Getenv("SMAIL_TEST_RESULT_PROMPT") != "1" || os.Getenv("INVOCATION_ID") == "" {
		t.Skip("需要隔离的 systemd 交互测试服务")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := adminreset.ReadVisibleInputContext(ctx, "取消测试：请等待自动结束"); err == nil {
		t.Fatal("取消输入后未返回错误")
	}
	if code := finishReset("显示测试：发送失败后输入已取消", 1); code != 1 {
		t.Fatalf("退出状态：%d", code)
	}
}
