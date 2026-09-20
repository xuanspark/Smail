package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"git.pcux.cn/xuans/Smail/utils/password"
)

const (
	protocolPairFailureLimit    = 10
	protocolAccountFailureLimit = 20
	protocolRemoteFailureLimit  = 100
	protocolLoginWindow         = 5 * time.Minute
	protocolLoginBlockTime      = 15 * time.Minute
	protocolLoginMaxEntries     = 10000
)

type protocolLoginAttempt struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
	lastSeen     time.Time
}

type protocolLoginLimiter struct {
	mu           sync.Mutex
	attempts     map[string]protocolLoginAttempt
	now          func() time.Time
	failureLimit int
}

var (
	protocolPairAttempts    = newProtocolLoginLimiter(protocolPairFailureLimit)
	protocolAccountAttempts = newProtocolLoginLimiter(protocolAccountFailureLimit)
	protocolRemoteAttempts  = newProtocolLoginLimiter(protocolRemoteFailureLimit)
)

func AuthenticateProtocol(protocol string, remoteAddress string, account string, plain string) (*models.User, error) {
	var grant *credentials.ProtocolGrant
	return authenticateProtocol(protocol, remoteAddress, account, plain, &grant)
}

func authenticateProtocol(protocol string, remoteAddress string, account string, plain string, grant **credentials.ProtocolGrant) (*models.User, error) {
	normalizedProtocol, protocolErr := credentials.NormalizeProtocol(protocol)
	normalizedAccount := strings.ToLower(strings.TrimSpace(account))
	if normalizedIdentifier, err := NormalizeProtocolIdentifier(account); err == nil {
		normalizedAccount = normalizedIdentifier
	}
	normalizedRemote := protocolRemoteHost(remoteAddress)
	pairKey := hashedProtocolLoginKey("pair", normalizedProtocol, normalizedRemote, normalizedAccount)
	accountKey := hashedProtocolLoginKey("account", normalizedAccount)
	remoteKey := hashedProtocolLoginKey("remote", normalizedRemote)

	if protocolPairAttempts.blocked(pairKey) || protocolAccountAttempts.blocked(accountKey) || protocolRemoteAttempts.blocked(remoteKey) {
		return nil, errors.New("invalid credentials")
	}
	fail := func() (*models.User, error) {
		protocolPairAttempts.recordFailure(pairKey)
		protocolAccountAttempts.recordFailure(accountKey)
		protocolRemoteAttempts.recordFailure(remoteKey)
		return nil, errors.New("invalid credentials")
	}
	if protocolErr != nil || db.Instance == nil {
		return fail()
	}

	protocolIdentifier, err := NormalizeProtocolIdentifier(account)
	if err != nil {
		return fail()
	}
	identity, err := ResolveIdentifier(protocolIdentifier)
	if err != nil {
		return fail()
	}
	userID := identity.UserID
	if userID <= 0 {
		return fail()
	}

	service, err := credentials.NewService(db.Instance)
	if err != nil {
		return fail()
	}
	matched, err := service.CheckClientOrProtocolGrant(userID, normalizedProtocol, plain)
	if errors.Is(err, password.ErrBusy) {
		return nil, err
	}
	if err != nil || matched == nil {
		return fail()
	}
	var user models.User
	has, err := db.Instance.ID(userID).Where("disabled = 0").Get(&user)
	if err != nil || !has || user.ID <= 0 {
		return fail()
	}
	protocolPairAttempts.clear(pairKey)
	protocolAccountAttempts.clear(accountKey)
	*grant = matched
	return &user, nil
}

func newProtocolLoginLimiter(failureLimit int) *protocolLoginLimiter {
	return &protocolLoginLimiter{
		attempts:     make(map[string]protocolLoginAttempt),
		now:          time.Now,
		failureLimit: failureLimit,
	}
}

func protocolRemoteHost(remoteAddress string) string {
	remoteAddress = strings.TrimSpace(remoteAddress)
	host, _, err := net.SplitHostPort(remoteAddress)
	if err == nil && host != "" {
		return strings.ToLower(host)
	}
	if remoteAddress == "" {
		return "unknown"
	}
	return strings.ToLower(remoteAddress)
}

func hashedProtocolLoginKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (limiter *protocolLoginLimiter) blocked(key string) bool {
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.cleanupLocked(now)
	attempt, exists := limiter.attempts[key]
	return exists && attempt.blockedUntil.After(now)
}

func (limiter *protocolLoginLimiter) recordFailure(key string) {
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	limiter.cleanupLocked(now)
	attempt := limiter.attempts[key]
	if attempt.windowStart.IsZero() || now.Sub(attempt.windowStart) > protocolLoginWindow {
		attempt = protocolLoginAttempt{windowStart: now}
	}
	attempt.failures++
	attempt.lastSeen = now
	if attempt.failures >= limiter.failureLimit {
		attempt.blockedUntil = now.Add(protocolLoginBlockTime)
	}
	if _, exists := limiter.attempts[key]; !exists && len(limiter.attempts) >= protocolLoginMaxEntries {
		limiter.evictOldestLocked()
	}
	limiter.attempts[key] = attempt
}

func (limiter *protocolLoginLimiter) clear(key string) {
	limiter.mu.Lock()
	delete(limiter.attempts, key)
	limiter.mu.Unlock()
}

func (limiter *protocolLoginLimiter) cleanupLocked(now time.Time) {
	for key, attempt := range limiter.attempts {
		windowExpired := !attempt.windowStart.IsZero() && now.Sub(attempt.windowStart) > protocolLoginWindow
		blockExpired := attempt.blockedUntil.IsZero() || !attempt.blockedUntil.After(now)
		if windowExpired && blockExpired {
			delete(limiter.attempts, key)
		}
	}
}

func (limiter *protocolLoginLimiter) evictOldestLocked() {
	oldestKey := ""
	var oldestTime time.Time
	for key, attempt := range limiter.attempts {
		if oldestKey == "" || attempt.lastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = attempt.lastSeen
		}
	}
	if oldestKey != "" {
		delete(limiter.attempts, oldestKey)
	}
}
