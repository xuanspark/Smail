//go:build linux

package adminreset

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// AuthorizeCurrentProcess 只允许部署管理员启动正式重置流程。
func AuthorizeCurrentProcess() error {
	if !authorizeLinuxProcess(os.Geteuid(), os.Getpid(), os.Getppid(), os.Getenv("INVOCATION_ID"), os.Getenv("SYSTEMD_EXEC_PID")) || !authorizedSystemdStartJob() {
		return ErrUnauthorizedPeer
	}
	if _, err := findStartCaller(); err != nil {
		return err
	}
	return nil
}

func authorizedSystemdStartJob() bool {
	output, err := exec.Command("systemctl", "list-jobs", "--no-legend", "--plain", "--no-pager").Output()
	if err != nil {
		return false
	}
	return hasAuthorizedStartJob(string(output))
}

func hasAuthorizedStartJob(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[1] == "smail-admin.service" {
			return fields[2] == "start" && (fields[3] == "waiting" || fields[3] == "running")
		}
	}
	return false
}

func authorizeLinuxProcess(euid, pid, parentPID int, invocationID, systemdExecPID string) bool {
	if euid != 0 || parentPID != 1 || !validInvocationID(invocationID) {
		return false
	}
	if systemdExecPID == "" {
		return true
	}
	execPID, err := strconv.Atoi(systemdExecPID)
	return err == nil && execPID == pid
}

func validInvocationID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range strings.ToLower(value) {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// CheckEnvironment 读取 Linux 管理程序的非敏感运行状态。
func CheckEnvironment() EnvironmentReport {
	_, err := exec.LookPath("systemd-ask-password")
	serviceManaged := os.Getppid() == 1 && validInvocationID(os.Getenv("INVOCATION_ID")) && authorizedSystemdStartJob()
	_, callerErr := findStartCaller()
	return EnvironmentReport{
		Platform:               "linux",
		Supported:              true,
		Privileged:             os.Geteuid() == 0,
		ServiceManaged:         serviceManaged && callerErr == nil,
		PasswordAgentAvailable: callerErr == nil || err == nil,
		ControlPath:            DefaultControlPath,
	}
}
