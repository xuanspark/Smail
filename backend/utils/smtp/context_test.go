package smtp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestDialResolvedAddressesFallsBackAfterTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	addresses := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("2001:db8::1")}
	calls := 0
	conn, err := dialResolvedAddresses(context.Background(), addresses, "25", 10*time.Millisecond, func(ctx context.Context, network, address string) (net.Conn, error) {
		calls++
		if network != "tcp" || address != net.JoinHostPort(addresses[calls-1].String(), "25") {
			t.Fatalf("unexpected dial target: %s %s", network, address)
		}
		if calls == 1 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if ctx.Err() != nil {
			t.Fatal("fallback inherited exhausted attempt context")
		}
		return client, nil
	})
	if err != nil || conn != client || calls != 2 {
		t.Fatalf("fallback failed: conn=%v calls=%d err=%v", conn, calls, err)
	}
}

func TestDialResolvedAddressesHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addresses := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")}
	calls := 0
	_, err := dialResolvedAddresses(ctx, addresses, "25", time.Second, func(context.Context, string, string) (net.Conn, error) {
		calls++
		cancel()
		return nil, context.Canceled
	})
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled parent was ignored: calls=%d err=%v", calls, err)
	}
}

func TestDialResolvedAddressesRetainsFailures(t *testing.T) {
	first := errors.New("first address failed")
	second := errors.New("second address failed")
	addresses := []netip.Addr{netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")}
	calls := 0
	_, err := dialResolvedAddresses(context.Background(), addresses, "25", time.Second, func(context.Context, string, string) (net.Conn, error) {
		calls++
		if calls == 1 {
			return nil, first
		}
		return nil, second
	})
	if calls != 2 || !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("connection failures were lost: calls=%d err=%v", calls, err)
	}
}

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
