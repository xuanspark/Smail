//go:build windows

package adminreset

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const DefaultControlPath = `\\.\pipe\smail-admin-control`

const windowsControlSDDL = "D:P(A;;GA;;;SY)(A;;GA;;;BA)"

type WindowsServer struct {
	listener  net.Listener
	closeOnce sync.Once
	closeErr  error
}

func ListenWindows(path string) (*WindowsServer, error) {
	return listenWindows(path, windowsControlSDDL)
}

func listenWindows(path, securityDescriptor string) (*WindowsServer, error) {
	if path == "" {
		path = DefaultControlPath
	}
	listener, err := winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: securityDescriptor,
		MessageMode:        true,
		InputBufferSize:    maxControlMessageSize,
		OutputBufferSize:   maxControlMessageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("listen named pipe: %w", err)
	}
	return &WindowsServer{listener: listener}, nil
}

func (s *WindowsServer) Serve(ctx context.Context, handler ResetHandler, guard *SessionGuard) error {
	if s == nil || s.listener == nil || handler == nil || guard == nil {
		return ErrInvalidRequest
	}
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
			return err
		}
		go handleWindowsConnection(conn, handler, guard)
	}
}

func handleWindowsConnection(conn net.Conn, handler ResetHandler, guard *SessionGuard) {
	defer conn.Close()
	if err := authorizeWindowsConnection(conn); err != nil {
		_ = EncodeResponse(conn, Response{Code: "UNAUTHORIZED", Message: "本机调用者身份不受信任"})
		return
	}
	handleResetConnection(conn, conn.SetDeadline, handler, guard)
}

func authorizeWindowsConnection(conn net.Conn) error {
	provider, ok := conn.(interface{ Fd() uintptr })
	if !ok || provider.Fd() == 0 {
		return ErrUnauthorizedPeer
	}

	var processID uint32
	if err := windows.GetNamedPipeClientProcessId(windows.Handle(provider.Fd()), &processID); err != nil || processID == 0 {
		return ErrUnauthorizedPeer
	}
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, processID)
	if err != nil {
		return ErrUnauthorizedPeer
	}
	defer windows.CloseHandle(process)

	var token windows.Token
	if err := windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token); err != nil {
		return ErrUnauthorizedPeer
	}
	defer token.Close()
	return authorizeWindowsToken(token)
}

func authorizeCurrentWindowsProcess() error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return ErrUnauthorizedPeer
	}
	defer token.Close()
	return authorizeWindowsToken(token)
}

func authorizeWindowsToken(token windows.Token) error {
	user, err := token.GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil {
		return ErrUnauthorizedPeer
	}
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return ErrUnauthorizedPeer
	}
	if user.User.Sid.Equals(systemSID) {
		return nil
	}

	adminSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return ErrUnauthorizedPeer
	}
	isAdmin, err := token.IsMember(adminSID)
	if err != nil || !isAdmin || !token.IsElevated() {
		return ErrUnauthorizedPeer
	}
	return nil
}

func (s *WindowsServer) Close() error {
	if s == nil || s.listener == nil {
		return nil
	}
	s.closeOnce.Do(func() { s.closeErr = s.listener.Close() })
	return s.closeErr
}

func DialControl(ctx context.Context, path string) (net.Conn, error) {
	if path == "" {
		path = DefaultControlPath
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout <= 0 {
			return nil, context.DeadlineExceeded
		}
		return winio.DialPipe(path, &timeout)
	}
	return winio.DialPipeContext(ctx, path)
}
