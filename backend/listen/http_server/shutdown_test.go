package http_server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebShutdownTimeoutClosesActiveConnection(t *testing.T) {
	entered, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); server.Close() })
	client := server.Client()
	client.Timeout = 3 * time.Second
	requestDone := make(chan error, 1)
	go func() {
		result, err := client.Post(server.URL, "application/json", strings.NewReader("{}"))
		if result != nil {
			_ = result.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter handler")
	}
	if err := shutdownWebServer(server.Config, 25*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected shutdown deadline error, got %v", err)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timeout did not close active connection")
	}
	if err := <-requestDone; err == nil {
		t.Fatal("timed-out request unexpectedly succeeded")
	}
}

func TestWebShutdownNilServer(t *testing.T) {
	if err := shutdownWebServer(nil, time.Second); err != nil {
		t.Fatal(err)
	}
}

// 真实连接仍在处理响应时停止服务，不能通过提前关闭客户端伪装成重启成功。
func TestWebStopPreservesActiveResponse(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(fmt.Sprintf("tls=%v", secure), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			finish := func() { releaseOnce.Do(func() { close(release) }) }
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"accepted"}`)
			}))
			var stop func()
			if secure {
				server.StartTLS()
				httpsServer, stop = server.Config, HttpsStop
			} else {
				server.Start()
				httpServer, stop = server.Config, HttpStop
			}
			t.Cleanup(func() { finish(); stop(); server.Close() })
			client := server.Client()
			client.Timeout = 3 * time.Second
			response := make(chan error, 1)
			go func() {
				result, err := client.Post(server.URL, "application/json", strings.NewReader("{}"))
				if err != nil {
					response <- err
					return
				}
				defer result.Body.Close()
				body, err := io.ReadAll(result.Body)
				if err == nil && (result.StatusCode != http.StatusOK || string(body) != `{"status":"accepted"}`) {
					err = fmt.Errorf("unexpected response: status=%d body=%q", result.StatusCode, body)
				}
				response <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not enter handler")
			}
			stopped := make(chan struct{})
			go func() { stop(); close(stopped) }()
			select {
			case <-stopped:
				t.Error("server stop returned before active response was completed")
			case <-time.After(75 * time.Millisecond):
			}
			finish()
			if err := <-response; err != nil {
				t.Errorf("active response was lost: %v", err)
			}
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				t.Fatal("server did not stop after response completed")
			}
		})
	}
}
