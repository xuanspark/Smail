package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
)

type weChatRoundTripFunc func(*http.Request) (*http.Response, error)

func (f weChatRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestBuildWeChatMessageLimitsContent(t *testing.T) {
	message := buildWeChatMessage(&parsemail.Email{
		Subject: strings.Repeat("\u754c", maxWeChatMessageBytes),
		Text:    []byte(strings.Repeat("body", maxWeChatMessageBytes)),
	})
	if len(message) > maxWeChatMessageBytes {
		t.Fatalf("message length = %d, want at most %d", len(message), maxWeChatMessageBytes)
	}
	if !utf8.ValidString(message) {
		t.Fatal("message is not valid UTF-8")
	}
	if message := buildWeChatMessage(nil); message != "" {
		t.Fatalf("nil email message = %q", message)
	}
}

func TestSendUserMsgLimitsOutboundContent(t *testing.T) {
	originalClient := weChatHTTPClient
	t.Cleanup(func() { weChatHTTPClient = originalClient })

	var captured sendMsgRequest
	weChatHTTPClient = &http.Client{Transport: weChatRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{}`)),
		}, nil
	})}

	hook := &WeChatPushHook{
		token:        "test-token",
		tokenExpires: time.Now().Add(time.Minute).Unix(),
		templateId:   "template",
		webDomain:    "mail.example.com",
	}
	hook.sendUserMsg(nil, "user", strings.Repeat("x", maxWeChatMessageBytes+100))
	if len(captured.Data.Content.Value) != maxWeChatMessageBytes {
		t.Fatalf("outbound content length = %d, want %d", len(captured.Data.Content.Value), maxWeChatMessageBytes)
	}
}

func TestGetWxAccessTokenConcurrentRefresh(t *testing.T) {
	originalClient := weChatHTTPClient
	t.Cleanup(func() { weChatHTTPClient = originalClient })

	var requestCount atomic.Int32
	weChatHTTPClient = &http.Client{Transport: weChatRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestCount.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"access_token":"shared-token","expires_in":7200}`)),
		}, nil
	})}

	hook := &WeChatPushHook{appId: "test-app-id", secret: "test-secret"}
	const workerCount = 32
	start := make(chan struct{})
	results := make(chan string, workerCount)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			<-start
			results <- hook.getWxAccessToken()
		}()
	}

	close(start)
	workers.Wait()
	close(results)

	for token := range results {
		if token != "shared-token" {
			t.Fatalf("token = %q, want shared-token", token)
		}
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("token request count = %d, want 1", got)
	}
}
