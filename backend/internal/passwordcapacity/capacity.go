// Package passwordcapacity 管理所有密码计算入口共享的并发名额。
package passwordcapacity

import "errors"

var ErrBusy = errors.New("password computation is busy")

var computationSlot = make(chan struct{}, 1)

// Acquire 满载时立即拒绝，不积压等待请求；调用方必须延迟释放名额。
func Acquire() (func(), error) {
	select {
	case computationSlot <- struct{}{}:
		return func() { <-computationSlot }, nil
	default:
		return nil, ErrBusy
	}
}
