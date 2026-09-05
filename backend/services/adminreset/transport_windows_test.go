//go:build windows

package adminreset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/term"
)

func testPipePath(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`\\.\pipe\smail-admin-control-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
}

func startWindowsTestServer(t *testing.T, handler ResetHandler) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	if err := authorizeCurrentWindowsProcess(); err != nil {
		t.Skip("Windows IPC tests require an elevated local administrator or SYSTEM token")
	}
	path := testPipePath(t)
	server, err := listenWindows(path, "D:P(A;;GA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, handler, NewSessionGuard(time.Minute)) }()
	t.Cleanup(func() {
		cancel()
		_ = server.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server stop error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("server did not stop")
		}
	})
	return path, cancel, done
}

func TestWindowsNamedPipeRoundTrip(t *testing.T) {
	var calls atomic.Int32
	path, _, _ := startWindowsTestServer(t, func(password string) error {
		if password != "NewPass123" {
			return errors.New("unexpected password")
		}
		calls.Add(1)
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := DialControl(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := SendReset(ctx, conn, "NewPass123"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
}

func TestWindowsNamedPipeRejectsInvalidAndOversizedRequests(t *testing.T) {
	path, _, _ := startWindowsTestServer(t, func(string) error { return nil })
	cases := [][]byte{
		[]byte("{invalid}\n"),
		append([]byte(`{"version":1,"nonce":"wrong","new_password":"Password123"}`), '\n'),
		append([]byte(strings.Repeat("x", maxControlMessageSize+1)), '\n'),
	}
	for _, payload := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := DialControl(ctx, path)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		challenge, err := DecodeResponse(conn)
		if err != nil || challenge.Code != "CHALLENGE" {
			_ = conn.Close()
			cancel()
			t.Fatalf("challenge = %+v, err = %v", challenge, err)
		}
		if _, err := conn.Write(payload); err != nil {
			_ = conn.Close()
			cancel()
			t.Fatal(err)
		}
		response, err := DecodeResponse(conn)
		_ = conn.Close()
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if response.OK || response.Code != "INVALID_REQUEST" {
			t.Fatalf("response = %+v", response)
		}
	}
}

func TestWindowsNamedPipeRejectsNonceFromAnotherConnection(t *testing.T) {
	path, _, _ := startWindowsTestServer(t, func(string) error { return nil })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first, err := DialControl(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	firstChallenge, err := DecodeResponse(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := DialControl(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := DecodeResponse(second); err != nil {
		t.Fatal(err)
	}
	if err := json.NewEncoder(second).Encode(Request{Version: ProtocolVersion, Nonce: firstChallenge.Nonce, NewPassword: "NewPass123"}); err != nil {
		t.Fatal(err)
	}
	response, err := DecodeResponse(second)
	if err != nil {
		t.Fatal(err)
	}
	if response.OK || response.Code != "INVALID_REQUEST" {
		t.Fatalf("response = %+v", response)
	}
	_ = first.Close()
}

func TestWindowsNamedPipeReturnsSafeHandlerFailure(t *testing.T) {
	path, _, _ := startWindowsTestServer(t, func(string) error { return errors.New("database details") })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := DialControl(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := SendReset(ctx, conn, "NewPass123"); err == nil || strings.Contains(err.Error(), "database details") {
		t.Fatalf("unsafe error = %v", err)
	}
}

func TestWindowsServerStopsWhenContextIsCancelled(t *testing.T) {
	path := testPipePath(t)
	server, err := listenWindows(path, "D:P(A;;GA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, func(string) error { return nil }, NewSessionGuard(time.Minute)) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop after cancellation")
	}
}

func TestWindowsPasswordInputRejectsNonInteractiveStdin(t *testing.T) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		t.Skip("test process has interactive stdin")
	}
	if _, err := ReadProtectedPassword(); err == nil {
		t.Fatal("non-interactive password input was accepted")
	}
}

func TestWindowsProductionACLRejectsNonElevatedCaller(t *testing.T) {
	path := testPipePath(t)
	server, err := ListenWindows(path)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := DialControl(ctx, path)
	if err != nil {
		return
	}
	_ = conn.Close()
}

func TestWindowsPipeACLOnlyNamesSystemAndAdministrators(t *testing.T) {
	if windowsControlSDDL != "D:P(A;;GA;;;SY)(A;;GA;;;BA)" {
		t.Fatalf("unexpected pipe ACL: %s", windowsControlSDDL)
	}
}
