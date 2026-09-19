package password

import "git.pcux.cn/xuans/Smail/internal/passwordcapacity"

var ErrBusy = passwordcapacity.ErrBusy

func acquireComputation() (func(), error) {
	return passwordcapacity.Acquire()
}
