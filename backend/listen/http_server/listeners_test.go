package http_server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/config"
)

func TestWebListenersStopBeforeServe(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	tlsConfig := certificateServer.TLS.Clone()
	certificateServer.Close()
	for _, secure := range []bool{false, true} {
		server := &http.Server{Addr: "127.0.0.1:0", TLSConfig: tlsConfig.Clone()}
		var stop func()
		var serve func(*http.Server) error
		if secure {
			httpsServer = server
			stop, serve = HttpsStop, serveHTTPSServer
		} else {
			httpServer = server
			stop, serve = HttpStop, serveHTTPServer
		}
		stop()
		if err := serve(server); err != nil {
			t.Fatalf("secure=%v: stopped listener returned %v", secure, err)
		}
		stop()
	}
}

func TestWebListenersConcurrentStop(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificates := append([]tls.Certificate(nil), certificateServer.TLS.Certificates...)
	certificateServer.Close()
	for _, secure := range []bool{false, true} {
		server := &http.Server{Addr: "127.0.0.1:0", TLSConfig: &tls.Config{Certificates: certificates}}
		var stop func()
		var serve func(*http.Server) error
		if secure {
			httpsServer = server
			stop, serve = HttpsStop, serveHTTPSServer
		} else {
			httpServer = server
			stop, serve = HttpStop, serveHTTPServer
		}
		done := make(chan error, 1)
		go func() { done <- serve(server) }()
		var workers sync.WaitGroup
		for range 10 {
			workers.Add(1)
			go func() { defer workers.Done(); stop() }()
		}
		workers.Wait()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("listener did not stop")
		}
	}
}

func TestPrepareWebServersClosesHTTPOnHTTPSFailure(t *testing.T) {
	oldConfig, oldManager, oldDevelopment := config.Instance, config.CurrentManager(), config.DevelopmentMode
	config.SetRuntimeManager(nil)
	config.Instance = &config.Config{WebDomain: "example.com", HttpsEnabled: 1}
	config.DevelopmentMode = false
	t.Cleanup(func() {
		HttpStop()
		HttpsStop()
		config.Instance = oldConfig
		config.SetRuntimeManager(oldManager)
		config.DevelopmentMode = oldDevelopment
	})
	if _, _, err := PrepareWebServers(); err == nil {
		t.Fatal("missing HTTPS dependencies were accepted")
	}
	if httpServer != nil {
		t.Fatal("HTTP listener was retained after HTTPS preparation failed")
	}
}
