//go:build linux

package adminreset

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestTerminalDeviceSignedEncodingBoundaries(t *testing.T) {
	tests := []struct {
		raw   int
		major uint32
		minor uint32
	}{
		{0, 0, 0},
		{34817, 136, 1},
		{math.MaxInt32, 4095, 524287},
		{math.MinInt32, 0, 524288},
		{-1, 4095, 1048575},
	}
	for _, test := range tests {
		t.Run(strconv.Itoa(test.raw), func(t *testing.T) {
			device, err := terminalDevice(test.raw)
			if err != nil || device != unix.Mkdev(test.major, test.minor) {
				t.Fatalf("终端设备编码错误：%x，%v", device, err)
			}
		})
	}
	if strconv.IntSize == 64 {
		for _, raw := range []int64{int64(math.MinInt32) - 1, int64(math.MaxInt32) + 1, math.MaxInt64, math.MinInt64} {
			if _, err := terminalDevice(int(raw)); !errors.Is(err, ErrUnauthorizedPeer) {
				t.Fatalf("越界终端设备号未拒绝：%d", raw)
			}
		}
	}
}

func TestTerminalPollDescriptorRejectsTruncation(t *testing.T) {
	for _, fd := range []uintptr{0, 1, math.MaxInt32} {
		actual, err := terminalPollDescriptor(fd)
		if err != nil || uint64(actual) != uint64(fd) {
			t.Fatalf("合法描述符转换失败：%d，%v", fd, err)
		}
	}
	for _, fd := range []uintptr{math.MaxInt32 + 1, math.MaxUint32, ^uintptr(0)} {
		if _, err := terminalPollDescriptor(fd); !errors.Is(err, ErrUnauthorizedPeer) {
			t.Fatalf("越界描述符未拒绝：%d", fd)
		}
	}
}

func TestTerminalStreamRejectsClosedDescriptor(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	stream := &terminalStream{ctx: context.Background(), terminal: &invocationTerminal{file: file}}
	if _, err := stream.Read(make([]byte, 1)); !errors.Is(err, ErrUnauthorizedPeer) {
		t.Fatalf("已关闭描述符未拒绝：%v", err)
	}
}

func TestProtectedCommandsIgnoreSearchPath(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"systemctl", "systemd-ask-password"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\nexit 91\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", directory)
	for _, name := range []string{"systemctl", "systemd-ask-password"} {
		resolved, err := exec.LookPath(name)
		if err != nil || resolved != filepath.Join(directory, name) {
			t.Fatal("PATH 劫持反例未正确构造")
		}
	}
	command := systemdInputCommand(context.Background(), "--no-tty", "--echo=no", "$(id);提示")
	if command.Path != systemdInputAgentPath || command.Args[3] != "$(id);提示" {
		t.Fatal("密码代理路径或原样参数不符合约束")
	}
	if command := systemdJobCommand(); command.Path != systemctlPath || len(command.Args) != 5 {
		t.Fatal("systemctl 来源被搜索路径替换")
	}
}
