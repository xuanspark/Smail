//go:build linux

package adminreset

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestUnixResetRoundTrip(t *testing.T) {
	if DefaultControlPath == "" {
		t.Skip("unsupported platform")
	}
	path := t.TempDir() + "/control.sock"
	server, err := ListenUnix(path, []uint32{uint32(os.Getuid())})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	guard := NewSessionGuard(time.Minute)
	called := make(chan string, 1)
	go func() { _ = server.Serve(ctx, func(password string) error { called <- password; return nil }, guard) }()
	var conn net.Conn
	for i := 0; i < 20; i++ {
		conn, err = DialControl(context.Background(), path)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := SendReset(context.Background(), conn, "NewPass123"); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	select {
	case password := <-called:
		if password != "NewPass123" {
			t.Fatalf("password = %q", password)
		}
	case <-time.After(time.Second):
		t.Fatal("reset handler was not called")
	}
}

func TestUnixListenDoesNotReplaceActiveSocket(t *testing.T) {
	path := t.TempDir() + "/control.sock"
	server, err := ListenUnix(path, []uint32{uint32(os.Getuid())})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := ListenUnix(path, []uint32{uint32(os.Getuid())}); err == nil {
		t.Fatal("active control socket was replaced")
	}
	if _, err := DialControl(context.Background(), path); err != nil {
		t.Fatalf("original control socket became unavailable: %v", err)
	}
}

func TestUnixEmptyAllowedUIDsRejectConnection(t *testing.T) {
	path := t.TempDir() + "/control.sock"
	server, err := ListenUnix(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Serve(ctx, func(string) error { t.Error("unauthorized handler was called"); return nil }, NewSessionGuard(time.Minute))
	}()

	conn, err := DialControl(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	response, err := DecodeResponse(conn)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Code != "UNAUTHORIZED" {
		t.Fatalf("response = %+v", response)
	}
}

func TestUnixListenRejectsWritableControlDirectory(t *testing.T) {
	parent := t.TempDir() + "/control"
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenUnix(parent+"/control.sock", []uint32{uint32(os.Getuid())}); err == nil {
		t.Fatal("writable control directory was accepted")
	}
}

func TestUnixControlSocketUsesRestrictedPermissions(t *testing.T) {
	parent := t.TempDir() + "/control"
	path := parent + "/control.sock"
	server, err := ListenUnix(path, []uint32{uint32(os.Getuid())})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	parentInfo, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if parentInfo.Mode().Perm() != 0750 {
		t.Fatalf("control directory permissions = %04o, want 0750", parentInfo.Mode().Perm())
	}
	socketInfo, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if socketInfo.Mode().Perm() != 0600 {
		t.Fatalf("control socket permissions = %04o, want 0600", socketInfo.Mode().Perm())
	}
}
