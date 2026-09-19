// Package passwordtest 为串行安全测试占用真实密码计算名额，不执行额外的昂贵计算。
package passwordtest

import (
	"sync"
	"testing"

	"git.pcux.cn/xuans/Smail/internal/passwordcapacity"
)

// Hold 仅供非并行测试使用，直接占用计算名额，不修改 TLS 等服务使用的随机源。
func Hold(t *testing.T) func() {
	t.Helper()
	unlock, err := passwordcapacity.Acquire()
	if err != nil {
		t.Fatalf("密码计算名额未被测试请求占用：%v", err)
	}
	var once sync.Once
	release := func() {
		once.Do(unlock)
	}
	t.Cleanup(release)
	return release
}
