package password

import (
	"errors"
	"testing"
)

func TestHashRandomFailureReleasesCapacity(t *testing.T) {
	expected := errors.New("random source unavailable")
	_, err := hashWithRandom("StrongPass12", func([]byte) (int, error) {
		return 0, expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("随机源异常未返回：%v", err)
	}
	if _, err := Hash("StrongPass12"); err != nil {
		t.Fatalf("随机源异常后计算名额未释放：%v", err)
	}
}

func TestHashRandomPanicReleasesCapacity(t *testing.T) {
	expected := errors.New("random source panic")
	func() {
		defer func() {
			if recovered := recover(); recovered != expected {
				t.Errorf("未保留预期的随机源异常：%v", recovered)
			}
		}()
		_, _ = hashWithRandom("StrongPass12", func([]byte) (int, error) {
			panic(expected)
		})
	}()
	if _, err := Hash("StrongPass12"); err != nil {
		t.Fatalf("随机源异常退出后计算名额未释放：%v", err)
	}
}
