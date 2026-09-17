// Package passwordtest 为串行安全测试占用真实密码计算名额，不执行额外的昂贵计算。
package passwordtest

import (
	"crypto/rand"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/utils/password"
)

type heldReader struct {
	original    io.Reader
	entered     chan struct{}
	resume      chan struct{}
	enteredOnce atomic.Bool
}

func (r *heldReader) Read(p []byte) (int, error) {
	if r.enteredOnce.CompareAndSwap(false, true) {
		close(r.entered)
		<-r.resume
		panic(r)
	}
	return r.original.Read(p)
}

// Hold 仅供非并行测试使用，释放时同时验证异常路径没有泄漏计算名额。
func Hold(t *testing.T) func() {
	t.Helper()
	r := &heldReader{original: rand.Reader, entered: make(chan struct{}), resume: make(chan struct{})}
	rand.Reader = r
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		_, _ = password.Hash("HeldPass123")
	}()
	var once sync.Once
	release := func() {
		once.Do(func() {
			close(r.resume)
			defer func() { rand.Reader = r.original }()
			select {
			case result := <-done:
				if result != r {
					t.Errorf("密码测试占位未经过预期的异常释放路径")
				}
			case <-time.After(5 * time.Second):
				t.Error("密码测试占位释放超时")
			}
		})
	}
	t.Cleanup(release)
	select {
	case <-r.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("密码计算名额未被测试请求占用")
	}
	return release
}
