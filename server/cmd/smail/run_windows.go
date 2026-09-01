//go:build windows

package main

import (
	"time"

	"git.pcux.cn/xuans/Smail/internal/app"
	"git.pcux.cn/xuans/Smail/signal"
	"golang.org/x/sys/windows/svc"
)

func runMain() error {
	if !svc.IsWindowsService() {
		return app.RecoverPanic(app.Run)
	}
	return svc.Run("Smail", &windowsService{})
}

type windowsService struct{}

func (windowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	done := make(chan error, 1)
	go func() {
		done <- app.RecoverPanic(app.Run)
	}()
	status <- svc.Status{State: svc.Running, Accepts: accepts}

	for {
		select {
		case err := <-done:
			if err != nil {
				return false, 1
			}
			status <- svc.Status{State: svc.StopPending}
			return false, 0
		case request, ok := <-requests:
			if !ok {
				return false, 0
			}
			switch request.Cmd {
			case svc.Interrogate:
				status <- svc.Status{State: svc.Running, Accepts: accepts}
			case svc.Stop, svc.Shutdown:
				return windowsServiceStop(done, status)
			}
		}
	}
}

func windowsServiceStop(done <-chan error, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StopPending, WaitHint: 30_000}
	select {
	case signal.StopChan <- true:
	case <-time.After(2 * time.Second):
		return false, 1
	}

	select {
	case err := <-done:
		if err != nil {
			return false, 1
		}
		return false, 0
	case <-time.After(30 * time.Second):
		return false, 1
	}
}
