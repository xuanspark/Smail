package password_test

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"git.pcux.cn/xuans/Smail/internal/passwordtest"
	"git.pcux.cn/xuans/Smail/utils/password"
)

func TestCapacityRejectsBurstAndAllowsRetry(t *testing.T) {
	encoded, err := password.Hash("StrongPass12")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatal("密码算法强度发生变化")
	}
	release := passwordtest.Hold(t)
	var wg sync.WaitGroup
	for index := 0; index < 64; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := password.Hash("StrongPass12"); !errors.Is(err, password.ErrBusy) {
				t.Errorf("并发哈希未返回繁忙：%v", err)
			}
			if matched, err := password.Check(encoded, "StrongPass12"); matched || !errors.Is(err, password.ErrBusy) {
				t.Errorf("并发校验未返回繁忙：%v %v", matched, err)
			}
			if err := password.CheckDummy("StrongPass12"); !errors.Is(err, password.ErrBusy) {
				t.Errorf("模拟校验绕过计算名额：%v", err)
			}
		}()
	}
	wg.Wait()
	release()
	if matched, err := password.Check(encoded, "StrongPass12"); !matched || err != nil {
		t.Fatalf("名额释放后不能继续认证：%v %v", matched, err)
	}
	if matched, err := password.Check(encoded, "WrongPass12"); matched || err != nil {
		t.Fatalf("密码错误与服务繁忙未区分：%v %v", matched, err)
	}
}

func TestCapacityValidationDoesNotLeakSlot(t *testing.T) {
	if _, err := password.Hash(""); !errors.Is(err, password.ErrInvalidLength) {
		t.Fatal(err)
	}
	if matched, err := password.Check(strings.Repeat("x", 256), "StrongPass12"); matched || err != nil {
		t.Fatal("非法哈希应被直接拒绝")
	}
	if err := password.CheckDummy("StrongPass12"); err != nil {
		t.Fatal(err)
	}
	if _, err := password.Hash("StrongPass12"); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityRealConcurrentComputations(t *testing.T) {
	encoded, err := password.Hash("StrongPass12")
	if err != nil {
		t.Fatal(err)
	}
	var succeeded, rejected atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := 0; index < 64; index++ {
		wg.Add(1)
		go func(mode int) {
			defer wg.Done()
			<-start
			var err error
			switch mode % 3 {
			case 0:
				_, err = password.Hash("StrongPass12")
			case 1:
				var matched bool
				matched, err = password.Check(encoded, "StrongPass12")
				if err == nil && !matched {
					t.Error("真实并发校验密码不匹配")
				}
			default:
				err = password.CheckDummy("StrongPass12")
			}
			if errors.Is(err, password.ErrBusy) {
				rejected.Add(1)
			} else if err == nil {
				succeeded.Add(1)
			} else {
				t.Errorf("真实并发计算异常：%v", err)
			}
		}(index)
	}
	close(start)
	wg.Wait()
	if succeeded.Load() == 0 || rejected.Load() == 0 || succeeded.Load()+rejected.Load() != 64 {
		t.Fatalf("并发保护未生效：成功=%d，繁忙=%d", succeeded.Load(), rejected.Load())
	}
	t.Logf("64 路真实计算：成功=%d，繁忙=%d", succeeded.Load(), rejected.Load())
}
