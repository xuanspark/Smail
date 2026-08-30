package http_server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	log "github.com/sirupsen/logrus"
)

const setupCookieName = "smail_setup_token"

const (
	setupFailureWindow = 5 * time.Minute
	setupBlockDuration = 15 * time.Minute
	setupMaxAttempts   = 10000
)

type setupAttempt struct {
	failures     int
	started      time.Time
	blockedUntil time.Time
}

type setupSecurity struct {
	mu        sync.Mutex
	requestMu sync.Mutex
	token     string
	tokenHash [32]byte
	consumed  bool
	allowed   []*net.IPNet
	attempts  map[string]setupAttempt
}

func newSetupSecurity() (*setupSecurity, error) {
	token := strings.TrimSpace(os.Getenv("SMAIL_SETUP_TOKEN"))
	if token == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return nil, err
		}
		token = base64.RawURLEncoding.EncodeToString(raw)
	}
	if len(token) < 24 || len(token) > 256 {
		return nil, errors.New("invalid setup token")
	}
	allowed, err := setupAllowedNetworks(os.Getenv("SMAIL_SETUP_ALLOWED_CIDRS"))
	if err != nil {
		return nil, err
	}
	return &setupSecurity{
		token:     token,
		tokenHash: sha256.Sum256([]byte(token)),
		allowed:   allowed,
		attempts:  make(map[string]setupAttempt),
	}, nil
}

func setupAllowedNetworks(value string) ([]*net.IPNet, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	result := make([]*net.IPNet, 0, len(parts))
	for _, part := range parts {
		_, network, err := net.ParseCIDR(strings.TrimSpace(part))
		if err != nil {
			return nil, errors.New("invalid setup network")
		}
		result = append(result, network)
	}
	return result, nil
}

func setupRemoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(strings.TrimSpace(host))
}

func (s *setupSecurity) sourceAllowed(r *http.Request) bool {
	ip := setupRemoteIP(r)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, network := range s.allowed {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *setupSecurity) bootstrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.sourceAllowed(r) || config.IsSetupReady() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: setupCookieName, Value: s.token, Path: "/api/",
			HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
		})
		next.ServeHTTP(w, r)
	})
}

func (s *setupSecurity) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.sourceAllowed(r) {
			log.Warn("Setup request rejected from unauthorized source")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ip := setupRemoteIP(r).String()
		if s.isBlocked(ip) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		token := strings.TrimSpace(r.Header.Get("X-Setup-Token"))
		if token == "" {
			if cookie, err := r.Cookie(setupCookieName); err == nil {
				token = cookie.Value
			}
		}
		if !s.validToken(token) {
			s.recordFailure(ip)
			log.Warn("Setup request rejected")
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// 一次性令牌必须串行消费请求，避免两个并发请求在状态切换前同时进入初始化处理器。
		s.requestMu.Lock()
		defer s.requestMu.Unlock()
		if !s.validToken(token) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		s.clearFailures(ip)
		next.ServeHTTP(w, r)
		if config.IsSetupReady() {
			s.consume()
		}
	}
}

func (s *setupSecurity) validToken(token string) bool {
	hash := sha256.Sum256([]byte(token))
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.consumed && !config.IsSetupReady() && token != "" && subtle.ConstantTimeCompare(hash[:], s.tokenHash[:]) == 1
}

func (s *setupSecurity) consume() {
	s.mu.Lock()
	s.consumed = true
	s.token = ""
	s.tokenHash = [32]byte{}
	s.mu.Unlock()
}

func (s *setupSecurity) isBlocked(ip string) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupAttemptsLocked(now)
	attempt := s.attempts[ip]
	return attempt.blockedUntil.After(now)
}

func (s *setupSecurity) recordFailure(ip string) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupAttemptsLocked(now)
	if _, exists := s.attempts[ip]; !exists && len(s.attempts) >= setupMaxAttempts {
		s.evictOldestAttemptLocked()
	}
	attempt := s.attempts[ip]
	if attempt.started.IsZero() || now.Sub(attempt.started) > setupFailureWindow {
		attempt = setupAttempt{started: now}
	}
	attempt.failures++
	if attempt.failures >= 5 {
		attempt.blockedUntil = now.Add(setupBlockDuration)
	}
	s.attempts[ip] = attempt
}

func (s *setupSecurity) cleanupAttemptsLocked(now time.Time) {
	for ip, attempt := range s.attempts {
		if attempt.blockedUntil.After(now) {
			continue
		}
		if !attempt.blockedUntil.IsZero() || attempt.started.IsZero() || now.Sub(attempt.started) > setupFailureWindow {
			delete(s.attempts, ip)
		}
	}
}

func (s *setupSecurity) evictOldestAttemptLocked() {
	var oldestIP string
	var oldestTime time.Time
	for ip, attempt := range s.attempts {
		candidate := attempt.started
		if candidate.IsZero() {
			oldestIP = ip
			break
		}
		if oldestIP == "" || candidate.Before(oldestTime) {
			oldestIP = ip
			oldestTime = candidate
		}
	}
	if oldestIP != "" {
		delete(s.attempts, oldestIP)
	}
}

func (s *setupSecurity) clearFailures(ip string) {
	s.mu.Lock()
	delete(s.attempts, ip)
	s.mu.Unlock()
}
