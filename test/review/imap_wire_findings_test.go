package imap_server

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// 直接读取隔离 TLS 服务的线上响应，避免客户端库过滤动态 UID 集合。
func auditIMAPWire(t *testing.T) (*tls.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := tls.Dial("tcp", imapTestAddr, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "* OK") {
		t.Fatalf("TLS 欢迎响应无效：%q %v", line, err)
	}
	if _, err := fmt.Fprintf(conn, "a1 LOGIN testCase@test.domain %s\r\n", imapTestProtocolCode); err != nil {
		t.Fatal("临时授权码登录写入失败")
	}
	auditIMAPTagged(t, reader, "a1")
	if _, err := fmt.Fprint(conn, "a2 SELECT INBOX\r\n"); err != nil {
		t.Fatal(err)
	}
	auditIMAPTagged(t, reader, "a2")
	return conn, reader
}

func auditIMAPTagged(t *testing.T, reader *bufio.Reader, tag string) []string {
	t.Helper()
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
		if strings.HasPrefix(line, tag+" ") {
			if !strings.HasPrefix(line, tag+" OK") {
				t.Fatalf("命令未成功：%q", line)
			}
			return lines
		}
	}
}

func TestAuditUIDFetchWildcardWire(t *testing.T) {
	last := auditIMAPMessage(t)
	conn, reader := auditIMAPWire(t)
	if _, err := fmt.Fprint(conn, "a3 UID FETCH * (UID FLAGS)\r\n"); err != nil {
		t.Fatal(err)
	}
	lines := auditIMAPTagged(t, reader, "a3")
	count := 0
	for _, line := range lines {
		if strings.Contains(line, " FETCH (") {
			count++
			if !strings.Contains(line, fmt.Sprintf("UID %d", last.ID)) {
				t.Fatalf("通配必须返回最大 UID：%q", line)
			}
		}
	}
	if count != 1 {
		t.Fatalf("UID FETCH * 必须在线上返回一封邮件：%q", lines)
	}
}

func auditBeginIdle(t *testing.T, conn *tls.Conn, reader *bufio.Reader, userID int, tag string) {
	t.Helper()
	if _, err := fmt.Fprintf(conn, "%s IDLE\r\n", tag); err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || !strings.HasPrefix(line, "+") {
		t.Fatalf("未进入 IDLE：%q %v", line, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := userConnects.Load(userID); ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("IDLE 未登记连接")
}

func TestAuditIDLEChangesDoneAndCleanup(t *testing.T) {
	initial := auditIMAPMessage(t)
	conn, reader := auditIMAPWire(t)
	ctx := &context.Context{UserID: initial.UserID}
	auditBeginIdle(t, conn, reader, initial.UserID, "a3")
	// 先同步初始快照，再观察新增邮件，避免把进入 IDLE 前的变化重复计入。
	if err := IdleNotice(ctx, initial.UserID, &models.Email{Id: initial.EmailID}); err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, " EXISTS") {
		t.Fatalf("初始通知无效：%q %v", line, err)
	}
	added := auditIMAPMessage(t)
	if err := IdleNotice(ctx, initial.UserID, &models.Email{Id: added.EmailID}); err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, " EXISTS") {
		t.Fatalf("新增邮件未通知：%q %v", line, err)
	}
	if _, err := fmt.Fprint(conn, "DONE\r\n"); err != nil {
		t.Fatal(err)
	}
	for _, line := range auditIMAPTagged(t, reader, "a3") {
		if strings.Contains(line, " EXISTS") {
			t.Fatalf("一次新增产生了重复 EXISTS：%q", line)
		}
	}
	waitIdleConnectionsDeleted(t, initial.UserID)

	auditBeginIdle(t, conn, reader, initial.UserID, "a4")
	// 外部移动等价于从已选邮箱移除；不借助投递通知，由周期轮询发现变化。
	if _, err := db.Instance.ID(added.ID).Cols("status").Update(&models.UserEmail{Status: 5}); err != nil {
		t.Fatal(err)
	}
	if line, err := reader.ReadString('\n'); err != nil || !strings.Contains(line, " EXPUNGE") {
		t.Fatalf("外部移除未产生 EXPUNGE：%q %v", line, err)
	}
	noticed := make(chan error, 1)
	go func() { noticed <- IdleNotice(ctx, initial.UserID, &models.Email{Id: initial.EmailID}) }()
	if _, err := fmt.Fprint(conn, "DONE\r\n"); err != nil {
		t.Fatal(err)
	}
	auditIMAPTagged(t, reader, "a4")
	if err := <-noticed; err != nil {
		t.Fatal(err)
	}
	waitIdleConnectionsDeleted(t, initial.UserID)
	if _, err := fmt.Fprint(conn, "a5 LOGOUT\r\n"); err != nil {
		t.Fatal(err)
	}
	auditIMAPTagged(t, reader, "a5")
	if _, exists := userConnects.Load(initial.UserID); exists {
		t.Fatal("退出后仍残留 IDLE 连接")
	}
}
