package gopop

import (
	"bufio"
	"errors"
	"net"
	"testing"
	"time"
)

type revokedAction struct{ securityTestAction }

func (a *revokedAction) Pass(session *Session, password string) error {
	session.Status = TRANSACTION
	session.Authorize = func() error { return errors.New("revoked") }
	return nil
}

func TestRevokedSessionRejectsCommandsWithoutQuitCommit(t *testing.T) {
	for _, command := range []string{"STAT", "LIST", "UIDL", "TOP 1 1", "RETR 1", "DELE 1", "RSET", "QUIT", "NOOP"} {
		t.Run(command, func(t *testing.T) {
			serverConn, clientConn := net.Pipe()
			t.Cleanup(func() { _ = clientConn.Close() })
			if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			action := &revokedAction{}
			server := &Server{Domain: "test", Action: action}
			done := make(chan struct{})
			go func() { server.handleClient(serverConn); close(done) }()
			reader := bufio.NewReader(clientConn)
			readResponseLine(t, reader, "+OK")
			writeCommand(t, clientConn, "USER user")
			readResponseLine(t, reader, "+OK")
			writeCommand(t, clientConn, "PASS test-password")
			readResponseLine(t, reader, "+OK")
			writeCommand(t, clientConn, command)
			readResponseLine(t, reader, "-ERR authorization revoked")
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("已撤销的连接未关闭")
			}
			if action.quitCalls != 0 {
				t.Fatal("撤销后仍提交延迟删除")
			}
		})
	}
}
