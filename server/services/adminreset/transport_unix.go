//go:build linux

package adminreset

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const DefaultControlPath = "/run/smail/admin-control.sock"

type UnixServer struct {
	listener net.Listener
	path     string
	allowed  map[uint32]bool
}

func ListenUnix(path string, allowedUIDs []uint32) (*UnixServer, error) {
	if path == "" {
		path = DefaultControlPath
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0750); err != nil {
		return nil, fmt.Errorf("create control directory: %w", err)
	}
	if info, err := os.Lstat(parent); err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("control directory is not secure")
	}
	if err := os.Chmod(parent, 0750); err != nil {
		return nil, fmt.Errorf("set control directory permissions: %w", err)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("control path is not a socket")
		}
		if conn, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond); dialErr == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("control socket is already active")
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove stale control socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen control socket: %w", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("set control socket permissions: %w", err)
	}
	allowed := make(map[uint32]bool, len(allowedUIDs))
	for _, uid := range allowedUIDs {
		allowed[uid] = true
	}
	return &UnixServer{listener: listener, path: path, allowed: allowed}, nil
}

func (s *UnixServer) Serve(ctx context.Context, handler ResetHandler, guard *SessionGuard) error {
	if s == nil || s.listener == nil || handler == nil || guard == nil {
		return ErrInvalidRequest
	}
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
		go s.handle(conn, handler, guard)
	}
}

func (s *UnixServer) handle(conn net.Conn, handler ResetHandler, guard *SessionGuard) {
	defer conn.Close()
	if err := s.authorize(conn); err != nil {
		_ = EncodeResponse(conn, Response{Code: "UNAUTHORIZED", Message: "本机调用者身份不受信任"})
		return
	}
	nonce, err := guard.Issue()
	if err != nil {
		_ = EncodeResponse(conn, Response{Code: "CHANNEL_UNAVAILABLE", Message: "\u91cd\u7f6e\u901a\u9053\u4e0d\u53ef\u7528"})
		return
	}
	defer guard.Discard(nonce)
	if err := EncodeResponse(conn, Response{OK: true, Code: "CHALLENGE", Nonce: nonce}); err != nil {
		return
	}
	request, err := DecodeRequest(conn)
	if err != nil {
		_ = EncodeResponse(conn, Response{Code: "INVALID_REQUEST", Message: "重置请求无效"})
		return
	}
	if request.Nonce != nonce {
		_ = EncodeResponse(conn, Response{Code: "INVALID_REQUEST", Message: "\u91cd\u7f6e\u8bf7\u6c42\u65e0\u6548"})
		return
	}
	if err := guard.Consume(request.Nonce); err != nil {
		_ = EncodeResponse(conn, Response{Code: "REPLAY_OR_EXPIRED", Message: "重置会话已失效或已使用"})
		return
	}
	password := request.NewPassword
	defer clearPassword(&password)
	if err := handler(password); err != nil {
		_ = EncodeResponse(conn, Response{Code: "RESET_FAILED", Message: "超级管理员密码重置失败"})
		return
	}
	_ = EncodeResponse(conn, Response{OK: true, Message: "超级管理员密码已重置"})
}

func (s *UnixServer) authorize(conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return ErrUnauthorizedPeer
	}
	var uid uint32
	var controlErr error
	rawConn, err := unixConn.SyscallConn()
	if err != nil {
		return err
	}
	if err := rawConn.Control(func(fd uintptr) {
		ucred, err := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if err != nil {
			controlErr = err
			return
		}
		uid = ucred.Uid
	}); err != nil {
		return err
	}
	if controlErr != nil {
		return controlErr
	}
	if len(s.allowed) == 0 {
		return ErrUnauthorizedPeer
	}
	if !s.allowed[uid] {
		return syscall.EPERM
	}
	return nil
}

func (s *UnixServer) Close() error {
	if s == nil {
		return nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
	}
	if s.path != "" {
		if info, err := os.Lstat(s.path); err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode()&os.ModeSocket != 0 {
			return os.Remove(s.path)
		}
	}
	return nil
}

func DialControl(ctx context.Context, path string) (net.Conn, error) {
	if path == "" {
		path = DefaultControlPath
	}
	dialer := net.Dialer{}
	return dialer.DialContext(ctx, "unix", path)
}
