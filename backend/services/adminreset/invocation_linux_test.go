//go:build linux

package adminreset

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestAdminCommandRequiresExplicitInteractiveStart(t *testing.T) {
	tests := []struct {
		command string
		targets bool
		allowed bool
	}{
		{"systemctl start smail-admin", true, true},
		{"/usr/bin/systemctl --system start smail-admin.service --no-pager", true, true},
		{"systemctl --wait --quiet start smail-admin", true, true},
		{"systemctl restart smail-admin", true, false},
		{"systemctl reload smail-admin", true, false},
		{"systemctl stop smail-admin", true, false},
		{"systemctl try-restart smail-admin", true, false},
		{"systemctl reload-or-restart smail-admin", true, false},
		{"systemctl try-reload-or-restart smail-admin", true, false},
		{"systemctl force-reload smail-admin", true, false},
		{"systemctl --no-block start smail-admin", true, false},
		{"systemctl --no-ask-password start smail-admin", true, false},
		{"systemctl --user start smail-admin", true, false},
		{"systemctl --host=other start smail-admin", true, false},
		{"systemctl start smail-admin smail", true, false},
		{"systemctl start smail-*", true, false},
		{"systemctl restart smail-*", true, false},
		{"systemctl status smail-admin", false, false},
		{"systemctl show smail-admin", false, false},
		{"systemctl list-jobs", false, false},
		{"systemctl restart smail", false, false},
	}
	for _, test := range tests {
		t.Run(test.command, func(t *testing.T) {
			targets, allowed := classifyAdminCommand(strings.Fields(test.command))
			if targets != test.targets || allowed != test.allowed {
				t.Fatalf("target=%v allowed=%v", targets, allowed)
			}
		})
	}
}

func TestCallerIdentityIncludesStartTimeAndTerminal(t *testing.T) {
	fields := strings.Fields("S 1 123 123 34817 123 0 0 0 0 0 0 0 0 0 0 0 1 0 9000")
	data := "123 (name with ) spaces) " + strings.Join(fields, " ")
	caller, err := parseCallerIdentity(123, data)
	if err != nil || caller.pid != 123 || caller.started != 9000 || caller.group != 123 || caller.terminal != 34817 || caller.foreground != 123 {
		t.Fatalf("identity=%+v err=%v", caller, err)
	}
	fields[19] = "9001"
	reused, err := parseCallerIdentity(123, "123 (systemctl) "+strings.Join(fields, " "))
	if err != nil || reused == caller {
		t.Fatal("PID 重用未被识别")
	}
	fields[0] = "Z"
	if _, err := parseCallerIdentity(123, "123 (systemctl) "+strings.Join(fields, " ")); err == nil {
		t.Fatal("僵尸进程被接受")
	}
	self, err := readCallerIdentity(os.Getpid())
	if err != nil || self.pid != os.Getpid() || self.started == 0 {
		t.Fatalf("真实进程标识读取失败：%+v %v", self, err)
	}
}

func TestCallerRequiresRootIdentity(t *testing.T) {
	if !rootProcessStatus("Name:\tsystemctl\nUid:\t0\t0\t0\t0\n") {
		t.Fatal("root 身份未通过")
	}
	for index := 0; index < 4; index++ {
		ids := []string{"0", "0", "0", "0"}
		ids[index] = strconv.Itoa(1000)
		if rootProcessStatus("Uid:\t" + strings.Join(ids, "\t")) {
			t.Fatal("非 root 身份被接受")
		}
	}
}
