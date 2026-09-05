package gopop

import (
	"bufio"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// 仅替换数据提供层，命令解析和响应编码执行真实实现；不启动公网监听。
type auditPOP3Action struct {
	securityTestAction
	committed []int64
	topLines  []int
}

func (a *auditPOP3Action) Delete(session *Session, id int64) error {
	session.DeleteIds = append(session.DeleteIds, id)
	return nil
}

func (a *auditPOP3Action) Rest(session *Session) error {
	session.DeleteIds = nil
	return nil
}

func (a *auditPOP3Action) Quit(session *Session) error {
	a.committed = append([]int64(nil), session.DeleteIds...)
	return nil
}

func (a *auditPOP3Action) Custom(session *Session, cmd string, args []string) ([]string, error) {
	return nil, errors.New("unsupported command")
}

func (a *auditPOP3Action) List(session *Session, msg string) ([]MailInfo, error) {
	return []MailInfo{{Id: 1, Size: 100}}, nil
}

func (a *auditPOP3Action) Uidl(session *Session, msg string) ([]UidlItem, error) {
	return []UidlItem{{Id: 1, UnionId: "audit-message"}}, nil
}

func (a *auditPOP3Action) Top(session *Session, id int64, n int) (string, error) {
	a.topLines = append(a.topLines, n)
	return "fixture", nil
}

func auditPOP3Pipe(t *testing.T, action Action) (net.Conn, *bufio.Reader, <-chan struct{}) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	server := &Server{Domain: "audit.invalid", Action: action}
	done := make(chan struct{})
	go func() {
		server.handleClient(serverConn)
		close(done)
	}()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("隔离 POP3 解析器未及时退出")
		}
	})
	if err := clientConn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(clientConn)
	readResponseLine(t, reader, "+OK")
	writeCommand(t, clientConn, "USER audit")
	readResponseLine(t, reader, "+OK")
	writeCommand(t, clientConn, "PASS fixture-only")
	readResponseLine(t, reader, "+OK")
	return clientConn, reader, done
}

func TestAuditPOP3RSETMustCancelPendingDeletion(t *testing.T) {
	action := &auditPOP3Action{}
	conn, reader, done := auditPOP3Pipe(t, action)
	writeCommand(t, conn, "DELE 1")
	readResponseLine(t, reader, "+OK")
	writeCommand(t, conn, "RSET")
	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	writeCommand(t, conn, "QUIT")
	readResponseLine(t, reader, "+OK")
	<-done
	t.Logf("rset_response=%q committed_ids=%v", strings.TrimSpace(response), action.committed)
	if !strings.HasPrefix(response, "+OK") || len(action.committed) != 0 {
		t.Fatal("标准 RSET 没有撤销删除标记，QUIT 仍提交原删除集合")
	}
}

func TestAuditPOP3SingleMessageRepliesMustBeSingleLine(t *testing.T) {
	for _, test := range []struct {
		command string
		want    string
	}{
		{command: "LIST 1", want: "+OK 1 100"},
		{command: "UIDL 1", want: "+OK 1 audit-message"},
	} {
		t.Run(test.command, func(t *testing.T) {
			conn, reader, _ := auditPOP3Pipe(t, &auditPOP3Action{})
			writeCommand(t, conn, test.command)
			response, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("command=%q first_response_line=%q buffered_extra_bytes=%d", test.command, strings.TrimSpace(response), reader.Buffered())
			if strings.TrimSpace(response) != test.want || reader.Buffered() != 0 {
				t.Fatal("指定消息编号的 LIST/UIDL 被错误编码成多行响应")
			}
		})
	}
}

func TestAuditPOP3ParserMustRejectNegativeTOPLines(t *testing.T) {
	action := &auditPOP3Action{}
	conn, reader, done := auditPOP3Pipe(t, action)
	writeCommand(t, conn, "TOP 1 -100000")
	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(response, "+OK") {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == ".\r\n" {
				break
			}
		}
	}
	writeCommand(t, conn, "QUIT")
	readResponseLine(t, reader, "+OK")
	<-done
	t.Logf("negative_top_response=%q dispatched_line_counts=%v", strings.TrimSpace(response), action.topLines)
	if !strings.HasPrefix(response, "-ERR") || len(action.topLines) != 0 {
		t.Fatal("TOP 的负数行参数未在协议入口拒绝")
	}
}
