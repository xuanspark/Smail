package smtp

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestSMTPContextCancelsGreeting(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := newClientWithContext(ctx, client, "localhost", "example.com"); err == nil {
		t.Fatal("blocked greeting succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("context did not cancel greeting")
	}
}

func TestSMTPContextCancelsCommand(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		_, _ = server.Write([]byte("220 local ready\r\n"))
		var command [512]byte
		_, _ = server.Read(command[:])
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	smtpClient, err := newClientWithContext(ctx, client, "localhost", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer smtpClient.Close()
	started := time.Now()
	if err := smtpClient.Hello("example.com"); err == nil {
		t.Fatal("blocked command succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("context did not cancel command")
	}
}
