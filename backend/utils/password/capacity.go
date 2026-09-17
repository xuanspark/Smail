package password

import "errors"

var ErrBusy = errors.New("password computation is busy")

// 所有协议和恢复流程共享一个计算名额；满载立即拒绝，不积压等待请求。
var computationSlot = make(chan struct{}, 1)

func acquireComputation() (func(), error) {
	select {
	case computationSlot <- struct{}{}:
		return func() { <-computationSlot }, nil
	default:
		return nil, ErrBusy
	}
}
